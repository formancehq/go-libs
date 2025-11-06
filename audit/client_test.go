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
	// Test that ExtractIdentity reads validated claims from context
	logger := zap.NewNop()

	// Create a context with claims (as stored by auth middleware)
	claims := &oidc.AccessTokenClaims{
		TokenClaims: oidc.TokenClaims{
			Subject: "user-from-context",
		},
	}
	ctx := context.WithValue(context.Background(), auth.ClaimsContextKey, claims)

	// Extract identity from context
	identity := audit.ExtractIdentity(ctx, logger)

	if identity != "user-from-context" {
		t.Errorf("expected 'user-from-context', got '%s'", identity)
	}
}

func TestExtractIdentityNoContext(t *testing.T) {
	// Test that ExtractIdentity returns empty string when no claims in context
	logger := zap.NewNop()
	ctx := context.Background()

	// Without context claims, should return empty string
	identity := audit.ExtractIdentity(ctx, logger)

	if identity != "" {
		t.Errorf("expected empty string without context claims, got '%s'", identity)
	}
}

func TestExtractIdentityEmptySubject(t *testing.T) {
	// Test that ExtractIdentity returns empty string when subject is empty
	logger := zap.NewNop()

	// Create claims with empty subject
	claims := &oidc.AccessTokenClaims{
		TokenClaims: oidc.TokenClaims{
			Subject: "",
		},
	}
	ctx := context.WithValue(context.Background(), auth.ClaimsContextKey, claims)

	identity := audit.ExtractIdentity(ctx, logger)

	if identity != "" {
		t.Errorf("expected empty string with empty subject, got '%s'", identity)
	}
}
