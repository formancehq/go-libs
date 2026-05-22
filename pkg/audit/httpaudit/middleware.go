package httpaudit

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/formancehq/go-libs/v5/pkg/audit"
)

// HTTPOption configures HTTP-specific audit behavior.
type HTTPOption func(*httpOptions)

type httpOptions struct {
	sensitivePaths map[string]struct{}
}

// WithSensitivePaths sets paths for which the response body should not be captured.
func WithSensitivePaths(paths ...string) HTTPOption {
	return func(o *httpOptions) {
		for _, p := range paths {
			o.sensitivePaths[p] = struct{}{}
		}
	}
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

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get(audit.HandledHeader) != "" {
				next.ServeHTTP(w, r)
				return
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

			r.Header.Set(audit.HandledHeader, "true")

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

			audit.PublishEvent(r.Context(), publisher, topicName, appName, payload)
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
