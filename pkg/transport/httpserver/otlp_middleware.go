package httpserver

import (
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
	data []byte
}

func (w *responseWriter) Write(data []byte) (int, error) {
	if w.Header().Get("Content-Type") == "application/octet-stream" {
		return w.ResponseWriter.Write(data)
	}
	w.data = append(w.data, data...)
	return len(data), nil
}

func (w *responseWriter) finalize() {
	if w.Header().Get("Content-Type") == "application/octet-stream" {
		return
	}
	if len(w.data) == 0 {
		return
	}
	_, err := w.ResponseWriter.Write(w.data)
	if err != nil {
		panic(err)
	}
}

func OTLPMiddleware(serverName string, debug bool) func(h http.Handler) http.Handler {
	m := otelchi.Middleware(serverName)
	return func(h http.Handler) http.Handler {
		return m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if debug {
				span := trace.SpanFromContext(r.Context())

				// Request sub-attributes
				attrs := []attribute.KeyValue{
					attribute.String("http.request.method", r.Method),
					attribute.String("http.request.url", r.URL.String()),
					attribute.String("http.request.host", r.Host),
					attribute.String("http.request.proto", r.Proto),
				}

				// Headers
				headerParts := make([]string, 0, len(r.Header))
				for name, values := range r.Header {
					headerParts = append(headerParts, fmt.Sprintf("%s: %s", name, strings.Join(values, ", ")))
				}
				attrs = append(attrs, attribute.String("http.request.headers", strings.Join(headerParts, "\n")))

				// Body
				if r.Body != nil {
					body, err := io.ReadAll(r.Body)
					if err == nil {
						attrs = append(attrs, attribute.String("http.request.body", string(body)))
						r.Body = io.NopCloser(strings.NewReader(string(body)))
					}
				}

				span.SetAttributes(attrs...)

				rw := &responseWriter{w, make([]byte, 0, 1024)}
				defer func() {
					rw.finalize()

					respAttrs := []attribute.KeyValue{
						attribute.String("http.response.body", string(rw.data)),
					}

					// Response headers
					respHeaderParts := make([]string, 0, len(rw.Header()))
					for name, values := range rw.Header() {
						respHeaderParts = append(respHeaderParts, fmt.Sprintf("%s: %s", name, strings.Join(values, ", ")))
					}
					respAttrs = append(respAttrs, attribute.String("http.response.headers", strings.Join(respHeaderParts, "\n")))

					trace.SpanFromContext(r.Context()).SetAttributes(respAttrs...)
				}()

				h.ServeHTTP(rw, r)
				return
			}

			h.ServeHTTP(w, r)
		}))
	}
}
