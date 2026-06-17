package httpserver

import (
	"net/http"
	"time"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
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

func LoggerMiddleware(l logging.Logger, opts ...Option) func(h http.Handler) http.Handler {
	cfg := newMiddlewareConfig(opts...)
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = r.WithContext(logging.ContextWithLogger(r.Context(), l))

			// Skip request logging for ignored paths (e.g. health probes), but
			// keep the logger available to downstream handlers via the context.
			if cfg.isIgnored(r.URL.Path) {
				h.ServeHTTP(w, r)
				return
			}

			start := time.Now()
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
