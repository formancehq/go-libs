package httpserver

import (
	"net/http"
	"time"

	"github.com/felixge/httpsnoop"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func NewLoggingResponseWriter(w http.ResponseWriter) *loggingResponseWriter {
	return &loggingResponseWriter{w, http.StatusOK}
}

func (lrw *loggingResponseWriter) Unwrap() http.ResponseWriter {
	return lrw.ResponseWriter
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

			// copy
			method := r.Method
			path := r.URL.Path

			metrics := httpsnoop.CaptureMetrics(h, w, r)
			statusCode := metrics.Code
			latency := time.Since(start)

			logger = logger.WithFields(map[string]interface{}{
				"method":     method,
				"path":       path,
				"latency":    latency,
				"user_agent": r.UserAgent(),
				"status":     statusCode,
			})
			if statusCode >= 400 {
				logger.Error("Request")
			} else {
				logger.Info("Request")
			}

		})
	}
}
