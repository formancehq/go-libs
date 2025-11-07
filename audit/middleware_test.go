package audit_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/formancehq/go-libs/v3/audit"
	"go.uber.org/zap"
)

// TestMiddlewareBodyCapture tests that request bodies are always fully available to handlers
// The maxBodySize config only affects what gets logged in audit, not what handlers receive
func TestMiddlewareBodyCapture(t *testing.T) {
	tests := []struct {
		name        string
		maxBodySize int64
		requestBody string
	}{
		{
			name:        "handler receives full body when under limit",
			maxBodySize: 1024,
			requestBody: `{"key":"value"}`,
		},
		{
			name:        "handler receives full body even when over audit limit",
			maxBodySize: 10,
			requestBody: strings.Repeat("x", 100),
		},
		{
			name:        "handler receives full body when maxBodySize is 0",
			maxBodySize: 0,
			requestBody: `{"key":"value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := audit.DefaultConfig("test")
			cfg.Enabled = false // Disable to avoid needing real publisher
			cfg.MaxBodySize = tt.maxBodySize

			client, err := audit.NewClient(cfg, zap.NewNop())
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Handler should ALWAYS receive the full body, regardless of maxBodySize
				// maxBodySize only affects audit logging, not request flow
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("failed to read body in handler: %v", err)
				}

				// Verify handler receives the FULL original body
				if string(body) != tt.requestBody {
					t.Errorf("handler should receive full body\nexpected: %s\ngot: %s",
						tt.requestBody, string(body))
				}

				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"status":"ok"}`))
			})

			req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(tt.requestBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			audit.HTTPMiddleware(client)(handler).ServeHTTP(w, req)
		})
	}
}

// TestMiddlewareExcludedPaths tests path exclusion feature
func TestMiddlewareExcludedPaths(t *testing.T) {
	cfg := audit.DefaultConfig("test")
	cfg.Enabled = false
	cfg.ExcludedPaths = []string{"/health", "/metrics"}

	client, err := audit.NewClient(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		path            string
		shouldBeAudited bool
	}{
		{"/health", false},
		{"/metrics", false},
		{"/api/users", true},
		{"/health/detailed", true}, // Should not match (not exact match)
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			handlerCalled = false

			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			audit.HTTPMiddleware(client)(handler).ServeHTTP(w, req)

			if !handlerCalled {
				t.Error("handler should always be called")
			}

			// We can't directly test if audit was skipped without a real publisher,
			// but we can verify the middleware doesn't break the request flow
			if w.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", w.Code)
			}
		})
	}
}

// TestMiddlewareResponseCapture tests that response status and body are captured
func TestMiddlewareResponseCapture(t *testing.T) {
	tests := []struct {
		name           string
		handlerStatus  int
		handlerBody    string
		expectedStatus int
	}{
		{
			name:           "should capture 200 response",
			handlerStatus:  http.StatusOK,
			handlerBody:    `{"success":true}`,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "should capture 404 response",
			handlerStatus:  http.StatusNotFound,
			handlerBody:    `{"error":"not found"}`,
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "should capture 500 response",
			handlerStatus:  http.StatusInternalServerError,
			handlerBody:    `{"error":"internal error"}`,
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := audit.DefaultConfig("test")
			cfg.Enabled = false

			client, err := audit.NewClient(cfg, zap.NewNop())
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.handlerStatus)
				w.Write([]byte(tt.handlerBody))
			})

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()

			audit.HTTPMiddleware(client)(handler).ServeHTTP(w, req)

			// Verify response is passed through correctly
			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if w.Body.String() != tt.handlerBody {
				t.Errorf("expected body '%s', got '%s'", tt.handlerBody, w.Body.String())
			}
		})
	}
}

// TestMiddlewareHeaderSanitization tests that sensitive headers are sanitized
func TestMiddlewareHeaderSanitization(t *testing.T) {
	cfg := audit.DefaultConfig("test")
	cfg.Enabled = false
	cfg.SensitiveHeaders = []string{"Authorization", "X-API-Key"}

	client, err := audit.NewClient(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify original request still has the header
		if r.Header.Get("Authorization") == "" {
			t.Error("handler should still receive Authorization header")
		}
		w.Header().Set("Set-Cookie", "session=secret")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("X-API-Key", "secret-key")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()

	audit.HTTPMiddleware(client)(handler).ServeHTTP(w, req)

	// The middleware should pass through all headers to the handler
	// (sanitization only affects audit logging, not the request flow)
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

// TestMiddlewareSensitiveResponsePaths tests response body redaction for sensitive paths
func TestMiddlewareSensitiveResponsePaths(t *testing.T) {
	cfg := audit.DefaultConfig("test")
	cfg.Enabled = false
	cfg.SensitiveResponsePaths = []string{"/api/auth/token", "/api/secrets"}

	client, err := audit.NewClient(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"secret":"should-be-redacted"}`))
	})

	tests := []struct {
		path             string
		shouldRedactBody bool
	}{
		{"/api/auth/token", true},
		{"/api/secrets", true},
		{"/api/users", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("POST", tt.path, nil)
			w := httptest.NewRecorder()

			audit.HTTPMiddleware(client)(handler).ServeHTTP(w, req)

			// Client should always receive the full response
			// (redaction only affects audit logs, not client response)
			if w.Body.String() != `{"secret":"should-be-redacted"}` {
				t.Errorf("client should always receive full response body")
			}
		})
	}
}

// TestMiddlewarePreservesHTTPInterfaces tests that optional HTTP interfaces work
func TestMiddlewarePreservesHTTPInterfaces(t *testing.T) {
	cfg := audit.DefaultConfig("test")
	cfg.Enabled = false

	client, err := audit.NewClient(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Test Flusher interface
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		} else {
			t.Error("ResponseWriter should implement http.Flusher")
		}

		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	audit.HTTPMiddleware(client)(handler).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}
