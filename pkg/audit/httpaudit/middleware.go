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
	"net/url"
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
	maxBodyBytes        int
	maxQueryParamsBytes int
}

const (
	// DefaultMaxCapturedBodyBytes bounds how many bytes of each request and
	// response body are stored in an audit event when WithMaxBodyBytes is not
	// set. It keeps the serialized audit message well within broker payload
	// limits (e.g. NATS max_payload).
	DefaultMaxCapturedBodyBytes = 64 * 1024

	// DefaultMaxCapturedQueryParamsBytes bounds how many raw query string bytes
	// are parsed into audit query parameters when WithMaxQueryParamsBytes is not
	// set.
	DefaultMaxCapturedQueryParamsBytes = DefaultMaxCapturedBodyBytes
)

// WithSensitivePaths sets path prefixes for which request and response bodies should not be captured.
func WithSensitivePaths(paths ...string) HTTPOption {
	return func(o *httpOptions) {
		for _, p := range paths {
			o.sensitivePaths[p] = struct{}{}
		}
	}
}

// WithMaxBodyBytes caps how many bytes of each captured request and response body
// are stored in the audit event. Bodies larger than the cap are truncated to the
// cap and flagged via HTTPRequest.BodyTruncated / HTTPResponse.BodyTruncated, which
// bounds the serialized audit message so it stays within broker payload limits
// (e.g. NATS max_payload). The full body is always passed through to the client and
// downstream handlers; only the audited copy is bounded. A value <= 0 keeps the
// default cap (DefaultMaxCapturedBodyBytes).
func WithMaxBodyBytes(maxBytes int) HTTPOption {
	return func(o *httpOptions) {
		if maxBytes > 0 {
			o.maxBodyBytes = maxBytes
		}
	}
}

// WithMaxQueryParamsBytes caps how many raw query string bytes are parsed into
// audit query parameters. Query strings larger than the cap are truncated before
// parsing and flagged via HTTPRequest.QueryParamsTruncated. A value <= 0 keeps
// the default cap (DefaultMaxCapturedQueryParamsBytes).
func WithMaxQueryParamsBytes(maxBytes int) HTTPOption {
	return func(o *httpOptions) {
		if maxBytes > 0 {
			o.maxQueryParamsBytes = maxBytes
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
		sensitivePaths:      make(map[string]struct{}),
		maxBodyBytes:        DefaultMaxCapturedBodyBytes,
		maxQueryParamsBytes: DefaultMaxCapturedQueryParamsBytes,
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
				body                 []byte
				requestBodyTruncated bool
				err                  error
			)

			sensitivePath := ho.isSensitivePath(r.URL.Path)
			queryParams, queryParamsTruncated := captureQueryParams(r.URL.RawQuery, ho.maxQueryParamsBytes)
			if sensitivePath {
				queryParams = nil
				queryParamsTruncated = false
			}

			if !isStreamRequest(r) && !sensitivePath {
				body, requestBodyTruncated, err = captureRequestBody(r, ho.maxBodyBytes)
				if err != nil && !errors.Is(err, io.EOF) {
					http.Error(w, "failed to read request body", http.StatusInternalServerError)
					return
				}
			}

			requestHeaders := cloneHeaderWithout(r.Header, "Authorization", "Cookie", audit.HandledHeader)

			r.Header.Set(audit.HandledHeader, handledHeaderValue)

			buf := bufPool.Get().(*bytes.Buffer)
			buf.Reset()
			defer putCaptureBuffer(buf)

			rww := &responseWriterWrapper{
				ResponseWriter: w,
				body:           buf,
				statusCode:     http.StatusOK,
				maxBodyBytes:   ho.maxBodyBytes,
				captureBody:    !sensitivePath,
			}

			next.ServeHTTP(rww, r)

			responseBody := ""
			responseBodyTruncated := false
			if !sensitivePath {
				responseBody = rww.body.String()
				responseBodyTruncated = rww.bodyTruncated
			}
			responseHeaders := cloneHeaderWithout(rww.Header(), "Set-Cookie")

			actor := audit.ExtractClaims(r, auditOpts)

			payload := audit.Payload{
				ID:      audit.NewPayloadID(),
				TraceID: audit.ExtractTraceID(r.Context()),
				Actor:   actor,
				HTTP: audit.HTTP{
					Request: audit.HTTPRequest{
						Method:               r.Method,
						Path:                 r.URL.Path,
						QueryParams:          queryParams,
						QueryParamsTruncated: queryParamsTruncated,
						Host:                 r.Host,
						Header:               requestHeaders,
						Body:                 string(body),
						BodyTruncated:        requestBodyTruncated,
					},
					Response: audit.HTTPResponse{
						StatusCode:    rww.statusCode,
						Headers:       responseHeaders,
						Body:          responseBody,
						BodyTruncated: responseBodyTruncated,
					},
				},
			}

			eventPublisher.Publish(r.Context(), payload)
		})
	}
}

func cloneHeaderWithout(header http.Header, names ...string) http.Header {
	clone := header.Clone()
	for _, name := range names {
		clone.Del(name)
	}
	return clone
}

func (o *httpOptions) isSensitivePath(requestPath string) bool {
	for sensitivePath := range o.sensitivePaths {
		if pathMatchesPrefix(requestPath, sensitivePath) {
			return true
		}
	}
	return false
}

func pathMatchesPrefix(requestPath string, sensitivePath string) bool {
	if sensitivePath == "" {
		return false
	}

	sensitivePath = strings.TrimRight(sensitivePath, "/")
	if sensitivePath == "" {
		return strings.HasPrefix(requestPath, "/")
	}

	return requestPath == sensitivePath || strings.HasPrefix(requestPath, sensitivePath+"/")
}

func isStreamRequest(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	return strings.HasPrefix(ct, "application/vnd.formance") && strings.HasSuffix(ct, "-stream")
}

func captureQueryParams(rawQuery string, maxBytes int) (url.Values, bool) {
	if rawQuery == "" {
		return nil, false
	}

	if maxBytes <= 0 {
		maxBytes = DefaultMaxCapturedQueryParamsBytes
	}

	truncated := false
	captured := rawQuery
	if len(rawQuery) > maxBytes {
		captured = trimIncompleteTrailingEscape(rawQuery[:maxBytes])
		truncated = true
	}

	values, _ := url.ParseQuery(captured)
	return values, truncated
}

func trimIncompleteTrailingEscape(s string) string {
	percent := strings.LastIndexByte(s, '%')
	if percent == -1 || len(s)-percent >= 3 {
		return s
	}
	return s[:percent]
}

// captureRequestBody reads up to maxBytes of the request body for audit capture
// while leaving the full body readable by downstream handlers. truncated reports
// whether the captured copy was cut to the cap.
func captureRequestBody(r *http.Request, maxBytes int) (captured []byte, truncated bool, err error) {
	if r.Body == nil {
		return nil, false, nil
	}

	// Read one byte past the cap to detect whether the body exceeds it.
	raw, err := io.ReadAll(io.LimitReader(r.Body, int64(maxBytes)+1))
	if len(raw) > 0 {
		r.Body = readCloser{
			Reader: io.MultiReader(bytes.NewReader(raw), r.Body),
			Closer: r.Body,
		}
	}

	captured = raw
	if len(raw) > maxBytes {
		truncated = true
		captured = raw[:maxBytes]
	}
	return captured, truncated, err
}

func putCaptureBuffer(buf *bytes.Buffer) {
	if shouldPoolCaptureBuffer(buf) {
		buf.Reset()
		bufPool.Put(buf)
	}
}

// shouldPoolCaptureBuffer keeps oversized buffers (from large WithMaxBodyBytes
// caps) out of the shared pool so they are not pinned in memory.
func shouldPoolCaptureBuffer(buf *bytes.Buffer) bool {
	return buf.Cap() <= DefaultMaxCapturedBodyBytes
}

type readCloser struct {
	io.Reader
	io.Closer
}

type responseWriterWrapper struct {
	http.ResponseWriter
	body          *bytes.Buffer
	statusCode    int
	maxBodyBytes  int
	captureBody   bool
	bodyTruncated bool
}

func (rww *responseWriterWrapper) Write(buf []byte) (int, error) {
	mediaType, _, _ := mime.ParseMediaType(rww.Header().Get("Content-Type"))
	if rww.captureBody && mediaType != "application/octet-stream" {
		rww.captureBodyBytes(buf)
	}
	return rww.ResponseWriter.Write(buf)
}

// captureBody appends bytes to the audited copy up to maxBodyBytes, flagging
// truncation once the cap is reached. The full buffer is still written to the
// client by the caller.
func (rww *responseWriterWrapper) captureBodyBytes(buf []byte) {
	remaining := rww.maxBodyBytes - rww.body.Len()
	if remaining <= 0 {
		if len(buf) > 0 {
			rww.bodyTruncated = true
		}
		return
	}
	if len(buf) > remaining {
		rww.body.Write(buf[:remaining])
		rww.bodyTruncated = true
		return
	}
	rww.body.Write(buf)
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
