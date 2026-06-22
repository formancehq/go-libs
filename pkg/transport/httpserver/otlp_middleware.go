package httpserver

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/felixge/httpsnoop"
	"github.com/riandyrn/otelchi"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	debugBodyAttributeLimit   = 64 * 1024
	debugHeaderAttributeLimit = 8 * 1024
	debugRedactedValue        = "[REDACTED]"
)

type responseWriter struct {
	data          []byte
	statusCode    int
	captureBody   bool
	bodyLimit     int
	bodyTruncated bool
	wroteHeader   bool
}

func (w *responseWriter) wrap(writer http.ResponseWriter) http.ResponseWriter {
	return httpsnoop.Wrap(writer, httpsnoop.Hooks{
		WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
			return func(code int) {
				if w.writeHeader(code) {
					next(code)
				}
			}
		},
		Write: func(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
			return func(data []byte) (int, error) {
				w.write()

				n, err := next(data)
				if w.captureBody && n > 0 {
					w.capture(data[:n])
				}
				return n, err
			}
		},
		Flush: func(next httpsnoop.FlushFunc) httpsnoop.FlushFunc {
			return func() {
				w.write()
				next()
			}
		},
		ReadFrom: func(next httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
			return func(src io.Reader) (int64, error) {
				w.write()
				if w.captureBody {
					src = &captureReader{
						reader:  src,
						capture: w.capture,
					}
				}
				return next(src)
			}
		},
	})
}

func (w *responseWriter) writeHeader(code int) bool {
	if code == http.StatusSwitchingProtocols {
		if w.wroteHeader {
			return false
		}
		w.wroteHeader = true
		w.statusCode = code
		return true
	}
	if code >= 100 && code <= 199 {
		return true
	}
	if w.wroteHeader {
		return false
	}

	w.wroteHeader = true
	w.statusCode = code
	return true
}

func (w *responseWriter) write() {
	if !w.wroteHeader {
		w.wroteHeader = true
		w.statusCode = http.StatusOK
	}
}

func (w *responseWriter) capture(data []byte) {
	if len(data) == 0 {
		return
	}

	remaining := w.bodyLimit - len(w.data)
	if remaining <= 0 {
		w.bodyTruncated = true
		return
	}

	if len(data) > remaining {
		w.data = append(w.data, data[:remaining]...)
		w.bodyTruncated = true
		return
	}

	w.data = append(w.data, data...)
}

type captureReader struct {
	reader  io.Reader
	capture func([]byte)
}

func (r *captureReader) Read(data []byte) (int, error) {
	n, err := r.reader.Read(data)
	if n > 0 {
		r.capture(data[:n])
	}
	return n, err
}

func isJSONContent(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "application/json")
}

func formatDebugHeaders(headers http.Header, limit int) (string, bool) {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)

	var builder strings.Builder
	for _, name := range names {
		value := strings.Join(headers[name], ", ")
		if isSensitiveHeader(name) {
			value = debugRedactedValue
		}

		part := fmt.Sprintf("%s: %s", name, value)
		if builder.Len() > 0 {
			part = "\n" + part
		}
		if appendLimitedString(&builder, part, limit) {
			return builder.String(), true
		}
	}

	return builder.String(), false
}

func isSensitiveHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "cookie", "proxy-authorization", "set-cookie":
		return true
	default:
		return false
	}
}

func appendLimitedString(builder *strings.Builder, value string, limit int) bool {
	remaining := limit - builder.Len()
	if remaining <= 0 {
		return len(value) > 0
	}
	if len(value) > remaining {
		builder.WriteString(value[:remaining])
		return true
	}
	builder.WriteString(value)
	return false
}

type bodyReadCloser struct {
	io.Reader
	io.Closer
}

func captureDebugBody(body io.ReadCloser, limit int) ([]byte, bool, io.ReadCloser, error) {
	data, err := io.ReadAll(io.LimitReader(body, int64(limit)+1))
	replacement := &bodyReadCloser{
		Reader: io.MultiReader(bytes.NewReader(data), body),
		Closer: body,
	}
	if err != nil {
		return nil, false, replacement, err
	}

	if len(data) > limit {
		return data[:limit], true, replacement, nil
	}

	return data, false, replacement, nil
}

func OTLPMiddleware(serverName string, debug bool, opts ...Option) func(h http.Handler) http.Handler {
	cfg := newMiddlewareConfig(opts...)
	// Hand the ignore set to otelchi as a filter so no span is started for
	// ignored paths (e.g. health probes). A filter returning false excludes the
	// request from tracing.
	m := otelchi.Middleware(serverName, otelchi.WithFilter(func(r *http.Request) bool {
		return !cfg.isIgnored(r.URL.Path)
	}))
	return func(h http.Handler) http.Handler {
		return m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// otelchi created no span for ignored paths; skip attribute
			// collection and body capture entirely.
			if cfg.isIgnored(r.URL.Path) {
				h.ServeHTTP(w, r)
				return
			}

			span := trace.SpanFromContext(r.Context())

			// Always log basic request metadata
			span.SetAttributes(
				attribute.String("http.request.method", r.Method),
				attribute.String("http.request.url", r.URL.String()),
				attribute.String("http.request.host", r.Host),
				attribute.String("http.request.proto", r.Proto),
			)

			captureBody := debug && isJSONContent(r.Header.Get("Content-Type"))

			if debug {
				// Debug: request headers
				headers, truncated := formatDebugHeaders(r.Header, debugHeaderAttributeLimit)
				span.SetAttributes(attribute.String("http.request.headers", headers))
				if truncated {
					span.SetAttributes(attribute.Bool("http.request.headers.truncated", true))
				}

				// Debug: request body (JSON only)
				if captureBody && r.Body != nil {
					body, truncated, replacement, err := captureDebugBody(r.Body, debugBodyAttributeLimit)
					r.Body = replacement
					if err == nil {
						span.SetAttributes(attribute.String("http.request.body", string(body)))
						if truncated {
							span.SetAttributes(attribute.Bool("http.request.body.truncated", true))
						}
					}
				}
			}

			if !debug {
				// httpsnoop defaults Code to StatusOK when the handler returns
				// without writing, matching net/http's implicit 200 response.
				metrics := httpsnoop.CaptureMetricsFn(w, func(w http.ResponseWriter) {
					h.ServeHTTP(w, r)
				})
				span.SetAttributes(attribute.Int("http.response.status_code", metrics.Code))
				return
			}

			rw := &responseWriter{
				data:        make([]byte, 0, 1024),
				captureBody: captureBody,
				bodyLimit:   debugBodyAttributeLimit,
			}
			ww := rw.wrap(w)
			defer func() {
				// Always log status code
				statusCode := rw.statusCode
				if statusCode == 0 {
					statusCode = http.StatusOK
				}
				span.SetAttributes(attribute.Int("http.response.status_code", statusCode))

				if debug {
					// Debug: response headers
					headers, truncated := formatDebugHeaders(ww.Header(), debugHeaderAttributeLimit)
					span.SetAttributes(attribute.String("http.response.headers", headers))
					if truncated {
						span.SetAttributes(attribute.Bool("http.response.headers.truncated", true))
					}

					// Debug: response body (only if we captured it, i.e. JSON content)
					if captureBody && len(rw.data) > 0 {
						span.SetAttributes(attribute.String("http.response.body", string(rw.data)))
						if rw.bodyTruncated {
							span.SetAttributes(attribute.Bool("http.response.body.truncated", true))
						}
					}
				}
			}()

			h.ServeHTTP(ww, r)
		}))
	}
}
