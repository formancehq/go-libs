package httpserver

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/riandyrn/otelchi"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type responseWriter struct {
	http.ResponseWriter
	data        []byte
	statusCode  int
	captureBody bool
}

func (w *responseWriter) WriteHeader(code int) {
	w.statusCode = code
	if !w.captureBody {
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *responseWriter) Write(data []byte) (int, error) {
	if !w.captureBody {
		return w.ResponseWriter.Write(data)
	}
	w.data = append(w.data, data...)
	return len(data), nil
}

func (w *responseWriter) finalize() {
	if !w.captureBody {
		return
	}
	if w.statusCode != 0 {
		w.ResponseWriter.WriteHeader(w.statusCode)
	}
	if len(w.data) == 0 {
		return
	}
	_, err := w.ResponseWriter.Write(w.data)
	if err != nil {
		panic(err)
	}
}

func isJSONContent(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "application/json")
}

func OTLPMiddleware(serverName string, debug bool) func(h http.Handler) http.Handler {
	m := otelchi.Middleware(serverName)
	return func(h http.Handler) http.Handler {
		return m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				headerParts := make([]string, 0, len(r.Header))
				for name, values := range r.Header {
					headerParts = append(headerParts, fmt.Sprintf("%s: %s", name, strings.Join(values, ", ")))
				}
				span.SetAttributes(attribute.String("http.request.headers", strings.Join(headerParts, "\n")))

				// Debug: request body (JSON only)
				if captureBody && r.Body != nil {
					body, err := io.ReadAll(r.Body)
					if err == nil {
						span.SetAttributes(attribute.String("http.request.body", string(body)))
						r.Body = io.NopCloser(bytes.NewReader(body))
					}
				}
			}

			rw := &responseWriter{
				ResponseWriter: w,
				data:           make([]byte, 0, 1024),
				captureBody:    captureBody,
			}
			defer func() {
				rw.finalize()

				// Always log status code
				statusCode := rw.statusCode
				if statusCode == 0 {
					statusCode = http.StatusOK
				}
				span.SetAttributes(attribute.Int("http.response.status_code", statusCode))

				if debug {
					// Debug: response headers
					respHeaderParts := make([]string, 0, len(rw.Header()))
					for name, values := range rw.Header() {
						respHeaderParts = append(respHeaderParts, fmt.Sprintf("%s: %s", name, strings.Join(values, ", ")))
					}
					span.SetAttributes(attribute.String("http.response.headers", strings.Join(respHeaderParts, "\n")))

					// Debug: response body (only if we captured it, i.e. JSON content)
					if captureBody && len(rw.data) > 0 {
						span.SetAttributes(attribute.String("http.response.body", string(rw.data)))
					}
				}
			}()

			h.ServeHTTP(rw, r)
		}))
	}
}
