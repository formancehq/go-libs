package audit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/formancehq/go-libs/v3/audit"
	"github.com/formancehq/go-libs/v3/auth"
	"github.com/formancehq/go-libs/v3/oidc"
	"go.uber.org/zap"
)

func TestClientDisabled(t *testing.T) {
	cfg := audit.DefaultConfig("test")
	cfg.Enabled = false

	client, err := audit.NewClient(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	audit.HTTPMiddleware(client)(handler).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestDisableIdentityExtraction(t *testing.T) {
	cfg := audit.DefaultConfig("test")
	cfg.Enabled = false // Disable to avoid needing a real publisher
	cfg.DisableIdentityExtraction = true

	client, err := audit.NewClient(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer fake.jwt.token")
	w := httptest.NewRecorder()

	audit.HTTPMiddleware(client)(handler).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestExtractIdentityFromContext(t *testing.T) {
	// Test that ExtractIdentity prefers context claims over JWT parsing
	logger := zap.NewNop()

	// Create a context with claims
	claims := &oidc.AccessTokenClaims{
		TokenClaims: oidc.TokenClaims{
			Subject: "user-from-context",
		},
	}
	ctx := context.WithValue(context.Background(), auth.ClaimsContextKey, claims)

	// Call ExtractIdentity with a fake JWT in the header
	// It should use the context claims, not parse the JWT
	identity := audit.ExtractIdentity(ctx, "Bearer fake.jwt.token", logger)

	if identity != "user-from-context" {
		t.Errorf("expected 'user-from-context', got '%s'", identity)
	}
}

func TestExtractIdentityFallbackToJWT(t *testing.T) {
	// Test that ExtractIdentity falls back to JWT parsing when no context claims
	logger := zap.NewNop()
	ctx := context.Background()

	// Without context claims, it should try to parse JWT
	// Since we're using a fake token, it should return empty string
	identity := audit.ExtractIdentity(ctx, "Bearer fake.jwt.token", logger)

	// With a fake token, we expect empty string (parsing fails gracefully)
	if identity != "" {
		t.Errorf("expected empty string with fake token, got '%s'", identity)
	}
}
