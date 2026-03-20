package httpserver

import (
	"net/http"

	"go.opentelemetry.io/otel/propagation"
)

// OtelHeaders adds standard OpenTelemetry trace headers (traceparent/tracestate)
// to outgoing HTTP responses.
func OtelHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := make(http.Header)
		tc := propagation.TraceContext{}
		tc.Inject(r.Context(), propagation.HeaderCarrier(headers))

		for k, values := range headers {
			for _, v := range values {
				w.Header().Add(k, v)
			}
		}

		next.ServeHTTP(w, r)
	})
}
