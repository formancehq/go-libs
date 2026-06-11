package api_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/require"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/transport/api"
)

func TestChiLogFormatter(t *testing.T) {
	t.Parallel()
	t.Run("NewLogFormatter", func(t *testing.T) {
		t.Parallel()
		formatter := api.NewLogFormatter()
		require.NotNil(t, formatter)
	})

	t.Run("NewLogEntry", func(t *testing.T) {
		t.Parallel()
		formatter := api.NewLogFormatter()
		req, err := http.NewRequest("GET", "/test", nil)
		require.NoError(t, err)

		entry := formatter.NewLogEntry(req)
		require.NotNil(t, entry)
		require.Implements(t, (*middleware.LogEntry)(nil), entry)
	})

	t.Run("Write", func(t *testing.T) {
		t.Parallel()
		// Create a buffer to capture log output
		var buf bytes.Buffer
		logger := logging.NewDefaultLogger(&buf, true, false, false)

		// Create a request with the logger in context
		req, err := http.NewRequest("GET", "/test", nil)
		require.NoError(t, err)
		ctx := logging.ContextWithLogger(context.Background(), logger)
		req = req.WithContext(ctx)

		// Create a log formatter and entry
		formatter := api.NewLogFormatter()
		entry := formatter.NewLogEntry(req)

		// Call Write method
		status := 200
		bytesWritten := 100
		elapsed := time.Millisecond * 50
		entry.Write(status, bytesWritten, nil, elapsed, nil)

		// Verify log output contains expected fields
		logOutput := buf.String()
		require.Contains(t, logOutput, "GET /test")
		require.Contains(t, logOutput, "status")
		require.Contains(t, logOutput, "bytes")
		require.Contains(t, logOutput, "elapsed")

		// Test with extra data
		buf.Reset()
		extra := map[string]string{"key": "value"}
		entry.Write(status, bytesWritten, nil, elapsed, extra)
		logOutput = buf.String()
		require.Contains(t, logOutput, "extra")
	})

	t.Run("Panic", func(t *testing.T) {
		t.Parallel()
		// Create a buffer to capture log output
		var buf bytes.Buffer
		logger := logging.NewDefaultLogger(&buf, true, false, false)

		// Create a request with the logger in context
		req, err := http.NewRequest("GET", "/test", nil)
		require.NoError(t, err)
		ctx := logging.ContextWithLogger(context.Background(), logger)
		req = req.WithContext(ctx)

		formatter := api.NewLogFormatter()
		entry := formatter.NewLogEntry(req)

		// Panic must log the panic value and stack instead of re-panicking,
		// otherwise chi's Recoverer cannot write the 500 response.
		require.NotPanics(t, func() {
			entry.Panic("test panic", []byte("stack trace"))
		})

		logOutput := buf.String()
		require.Contains(t, logOutput, "GET /test")
		require.Contains(t, logOutput, "test panic")
		require.Contains(t, logOutput, "stack trace")
	})

	t.Run("Panicking handler returns 500 with Recoverer", func(t *testing.T) {
		t.Parallel()
		// Regression test for EN-1164: re-panicking from chiLogEntry.Panic
		// aborted chi's Recoverer before it could write the 500 response.
		var buf bytes.Buffer
		logger := logging.NewDefaultLogger(&buf, true, false, false)

		router := chi.NewRouter()
		router.Use(middleware.RequestLogger(api.NewLogFormatter()))
		router.Use(middleware.Recoverer)
		router.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
			panic("boom")
		})

		req := httptest.NewRequest("GET", "/panic", nil)
		req = req.WithContext(logging.ContextWithLogger(context.Background(), logger))
		rr := httptest.NewRecorder()

		// Recoverer must be able to complete its recovery path and write
		// the 500 response instead of the panic being propagated
		require.NotPanics(t, func() {
			router.ServeHTTP(rr, req)
		})
		require.Equal(t, http.StatusInternalServerError, rr.Code)

		// The panic value must have been logged
		require.Contains(t, buf.String(), "boom")
	})

	t.Run("Integration with Chi middleware", func(t *testing.T) {
		t.Parallel()
		// Create a buffer to capture log output
		var buf bytes.Buffer
		logger := logging.NewDefaultLogger(&buf, true, false, false)

		// Create a test handler
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		})

		// Set up the middleware chain
		formatter := api.NewLogFormatter()
		mw := middleware.RequestLogger(formatter)
		wrappedHandler := mw(handler)

		// Create a test request
		req := httptest.NewRequest("GET", "/test", nil)
		ctx := logging.ContextWithLogger(context.Background(), logger)
		req = req.WithContext(ctx)

		// Create a response recorder
		rr := httptest.NewRecorder()

		// Call the handler
		wrappedHandler.ServeHTTP(rr, req)

		// Verify response
		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, "OK", rr.Body.String())

		// Verify log output
		logOutput := buf.String()
		require.Contains(t, logOutput, "GET /test")
		require.Contains(t, logOutput, "status")
	})
}
