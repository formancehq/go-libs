package httpserver

import (
	"net/http"
	"time"

	"github.com/formancehq/go-libs/v3/logging"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func NewLoggingResponseWriter(w http.ResponseWriter) *loggingResponseWriter {
	return &loggingResponseWriter{w, http.StatusOK}
}

func (lrw *loggingResponseWriter) WriteHeader(statusCode int) {
	lrw.statusCode = statusCode
	lrw.ResponseWriter.WriteHeader(statusCode)
}

func LoggerMiddleware(l logging.Logger) func(h http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			r = r.WithContext(logging.ContextWithLogger(r.Context(), l))
			logger := logging.FromContext(r.Context())

			rec := NewLoggingResponseWriter(w)
			// copy
			method := r.Method
			path := r.URL.Path

			h.ServeHTTP(rec, r)
			latency := time.Since(start)

			logger = logger.WithFields(map[string]interface{}{
				"method":     method,
				"path":       path,
				"latency":    latency,
				"user_agent": r.UserAgent(),
				"status":     rec.statusCode,
			})
			if rec.statusCode >= 400 {
				logger.Error("Request")
			} else {
				logger.Info("Request")
			}

		})
	}
}
