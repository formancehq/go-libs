package httpaudit

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/formancehq/go-libs/v5/pkg/audit"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

// HTTPOption configures HTTP-specific audit behavior.
type HTTPOption func(*httpOptions)

type httpOptions struct {
	enabled             bool
	sensitivePaths      map[string]struct{}
	eventPublisher      auditEventPublisher
	handledHeaderSecret string
}

// WithSensitivePaths sets paths for which the response body should not be captured.
func WithSensitivePaths(paths ...string) HTTPOption {
	return func(o *httpOptions) {
		for _, p := range paths {
			o.sensitivePaths[p] = struct{}{}
		}
	}
}

// WithEnabled enables or disables HTTP audit event capture and publication.
func WithEnabled(enabled bool) HTTPOption {
	return func(o *httpOptions) {
		o.enabled = enabled
	}
}

// WithHandledHeaderSecret sets the shared secret required to honor the
// audit.HandledHeader dedup header.
//
// When a secret is configured, the middleware only skips audit when the
// incoming header value matches the secret, and marks handled requests by
// setting the header to the secret so trusted downstream services (sharing
// the same secret) deduplicate correctly. When no secret is configured, any
// non-empty header value skips audit, which lets external clients bypass the
// audit trail; configure a secret on every service in the request path that
// is reachable, directly or indirectly, by untrusted clients.
func WithHandledHeaderSecret(secret string) HTTPOption {
	return func(o *httpOptions) {
		o.handledHeaderSecret = secret
	}
}

// WithConfig configures HTTP audit event capture from audit.Config.
func WithConfig(config audit.Config) HTTPOption {
	return func(o *httpOptions) {
		WithEnabled(config.Enabled)(o)
		WithHandledHeaderSecret(config.HandledHeaderSecret)(o)
	}
}

// WithAsyncPublishing enables async publishing using a caller-managed AsyncPublisher.
// Callers should close the publisher during shutdown to drain queued audit events.
func WithAsyncPublishing(publisher *AsyncPublisher) HTTPOption {
	return func(o *httpOptions) {
		if publisher != nil {
			o.eventPublisher = publisher
		}
	}
}

// WithAsyncPublisher enables async publishing using a caller-managed AsyncPublisher.
// Use this option when the caller needs explicit lifecycle control via AsyncPublisher.Close.
func WithAsyncPublisher(publisher *AsyncPublisher) HTTPOption {
	return WithAsyncPublishing(publisher)
}

var bufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

// Middleware returns an HTTP middleware that captures request/response data
// and publishes audit events via the given Watermill publisher.
func Middleware(publisher message.Publisher, topicName string, appName string, opts []audit.Option, httpOpts ...HTTPOption) func(http.Handler) http.Handler {
	auditOpts := audit.NewOptions(opts...)

	ho := &httpOptions{
		sensitivePaths: make(map[string]struct{}),
	}
	for _, opt := range httpOpts {
		opt(ho)
	}

	handledHeaderValue := "true"
	if ho.handledHeaderSecret != "" {
		handledHeaderValue = ho.handledHeaderSecret
	}

	var eventPublisher auditEventPublisher
	if ho.enabled {
		if ho.handledHeaderSecret == "" {
			logging.Infof(
				"WARNING: audit middleware configured without a handled-header secret: any client sending the %s header can bypass the audit trail; configure WithHandledHeaderSecret (flag --%s)",
				audit.HandledHeader, audit.AuditHandledHeaderSecretFlag,
			)
		}
		eventPublisher = auditEventPublisher(syncAuditEventPublisher{
			publisher: publisher,
			topicName: topicName,
			appName:   appName,
		})
		if ho.eventPublisher != nil {
			eventPublisher = ho.eventPublisher
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !ho.enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Skip audit when an upstream trusted hop already handled it.
			// With a secret configured, only a header carrying the exact
			// secret is honored; anything else (e.g. a client-forged value)
			// is still audited. Without a secret, keep the legacy behavior
			// of trusting any non-empty value.
			if headerValue := r.Header.Get(audit.HandledHeader); headerValue != "" {
				if ho.handledHeaderSecret == "" ||
					subtle.ConstantTimeCompare([]byte(headerValue), []byte(ho.handledHeaderSecret)) == 1 {
					next.ServeHTTP(w, r)
					return
				}
			}

			var (
				body []byte
				err  error
			)

			if !isStreamRequest(r) {
				body, err = io.ReadAll(r.Body)
				if err != nil && !errors.Is(err, io.EOF) {
					http.Error(w, "failed to read request body", http.StatusInternalServerError)
					return
				}
				if len(body) > 0 {
					_ = r.Body.Close()
					r.Body = io.NopCloser(bytes.NewBuffer(body))
				}
			}

			requestHeaders := r.Header.Clone()
			requestHeaders.Del("Authorization")
			requestHeaders.Del(audit.HandledHeader)

			r.Header.Set(audit.HandledHeader, handledHeaderValue)

			buf := bufPool.Get().(*bytes.Buffer)
			buf.Reset()
			defer bufPool.Put(buf)

			rww := &responseWriterWrapper{
				ResponseWriter: w,
				body:           buf,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(rww, r)

			responseBody := rww.body.String()
			if _, sensitive := ho.sensitivePaths[r.URL.Path]; sensitive {
				responseBody = ""
			}

			actor := audit.ExtractClaims(r, auditOpts)

			payload := audit.Payload{
				ID:      audit.NewPayloadID(),
				TraceID: audit.ExtractTraceID(r.Context()),
				Actor:   actor,
				HTTP: audit.HTTP{
					Request: audit.HTTPRequest{
						Method: r.Method,
						Path:   r.URL.Path,
						Host:   r.Host,
						Header: requestHeaders,
						Body: func() string {
							if len(body) > 0 {
								return string(body)
							}
							return ""
						}(),
					},
					Response: audit.HTTPResponse{
						StatusCode: rww.statusCode,
						Headers:    rww.Header(),
						Body:       responseBody,
					},
				},
			}

			eventPublisher.Publish(r.Context(), payload)
		})
	}
}

func isStreamRequest(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	return strings.HasPrefix(ct, "application/vnd.formance") && strings.HasSuffix(ct, "-stream")
}

type responseWriterWrapper struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (rww *responseWriterWrapper) Write(buf []byte) (int, error) {
	mediaType, _, _ := mime.ParseMediaType(rww.Header().Get("Content-Type"))
	if mediaType != "application/octet-stream" {
		rww.body.Write(buf)
	}
	return rww.ResponseWriter.Write(buf)
}

func (rww *responseWriterWrapper) WriteHeader(statusCode int) {
	rww.statusCode = statusCode
	rww.ResponseWriter.WriteHeader(statusCode)
}

func (rww *responseWriterWrapper) Flush() {
	if f, ok := rww.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (rww *responseWriterWrapper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := rww.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("hijack not supported")
}

func (rww *responseWriterWrapper) Unwrap() http.ResponseWriter {
	return rww.ResponseWriter
}
