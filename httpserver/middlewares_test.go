package httpserver_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/formancehq/go-libs/v2/httpserver"
	"github.com/formancehq/go-libs/v2/logging"
	"github.com/stretchr/testify/require"
)

func TestLoggerMiddleware(t *testing.T) {
	t.Parallel()

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify logger is in context
		logger := logging.FromContext(r.Context())
		require.NotNil(t, logger)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Create logger middleware with a properly initialized logger
	// Use io.Discard to avoid test output noise
	var buf bytes.Buffer
	logger := logging.NewDefaultLogger(&buf, false, false, false)
	middleware := httpserver.LoggerMiddleware(logger)

	// Wrap the test handler
	wrappedHandler := middleware(testHandler)

	// Create a test request
	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	// Call the handler
	wrappedHandler.ServeHTTP(rr, req)

	// Verify response
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "OK", rr.Body.String())
}
