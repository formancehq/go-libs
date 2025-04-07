package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/formancehq/go-libs/v2/logging"
	"github.com/stretchr/testify/require"
	"github.com/zitadel/oidc/v2/pkg/oidc"
)

func TestNewNoAuth(t *testing.T) {
	auth := NewNoAuth()
	require.NotNil(t, auth, "NoAuth ne devrait pas être nil")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)

	authenticated, err := auth.Authenticate(w, r)
	require.NoError(t, err, "Authenticate ne devrait pas échouer")
	require.True(t, authenticated, "L'authentification devrait toujours réussir avec NoAuth")
}

func TestNewJWTAuth(t *testing.T) {
	logger := logging.Testing()
	auth := newJWTAuth(logger, 3, "https://issuer.example.com", "test-service", true)
	require.NotNil(t, auth, "JWTAuth ne devrait pas être nil")
	require.Equal(t, "https://issuer.example.com", auth.issuer, "L'issuer devrait être correctement défini")
	require.Equal(t, "test-service", auth.service, "Le service devrait être correctement défini")
	require.True(t, auth.checkScopes, "La vérification des scopes devrait être activée")
}

func TestJWTAuth_Authenticate_NoAuthHeader(t *testing.T) {
	logger := logging.Testing()
	auth := newJWTAuth(logger, 3, "https://issuer.example.com", "test-service", true)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)

	authenticated, err := auth.Authenticate(w, r)
	require.Error(t, err, "Authenticate devrait échouer sans en-tête d'autorisation")
	require.False(t, authenticated, "L'authentification devrait échouer sans en-tête d'autorisation")
	require.Contains(t, err.Error(), "no authorization header", "L'erreur devrait mentionner l'absence d'en-tête d'autorisation")
}

func TestJWTAuth_Authenticate_MalformedAuthHeader(t *testing.T) {
	logger := logging.Testing()
	auth := newJWTAuth(logger, 3, "https://issuer.example.com", "test-service", true)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.Header.Set("authorization", "NotBearer token123")

	authenticated, err := auth.Authenticate(w, r)
	require.Error(t, err, "Authenticate devrait échouer avec un en-tête d'autorisation mal formé")
	require.False(t, authenticated, "L'authentification devrait échouer avec un en-tête d'autorisation mal formé")
	require.Contains(t, err.Error(), "malformed authorization header", "L'erreur devrait mentionner un en-tête d'autorisation mal formé")
}

func TestNewOtlpHttpClient(t *testing.T) {
	client := newOtlpHttpClient(5)
	require.NotNil(t, client, "Le client HTTP ne devrait pas être nil")
}

func TestJWTAuth_Authenticate_ValidBearerPrefix(t *testing.T) {
	t.Run("lowercase bearer", func(t *testing.T) {
		logger := logging.Testing()
		auth := newJWTAuth(logger, 3, "https://issuer.example.com", "test-service", true)

		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/test", nil)
		r.Header.Set("authorization", "bearer token123")

		_, _ = auth.Authenticate(w, r)
	})

	t.Run("uppercase Bearer", func(t *testing.T) {
		logger := logging.Testing()
		auth := newJWTAuth(logger, 3, "https://issuer.example.com", "test-service", true)

		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/test", nil)
		r.Header.Set("authorization", "Bearer token123")

		_, _ = auth.Authenticate(w, r)
	})
}

func TestJWTAuth_GetAccessTokenVerifier(t *testing.T) {
	originalAuthServicePort := os.Getenv("AUTH_SERVICE_PORT")
	originalStackPublicURL := os.Getenv("STACK_PUBLIC_URL")
	defer func() {
		os.Setenv("AUTH_SERVICE_PORT", originalAuthServicePort)
		os.Setenv("STACK_PUBLIC_URL", originalStackPublicURL)
	}()

	os.Setenv("AUTH_SERVICE_PORT", "9090")
	os.Setenv("STACK_PUBLIC_URL", "https://stack.example.com")

	logger := logging.Testing()
	auth := newJWTAuth(logger, 3, "https://issuer.example.com", "test-service", true)

	auth.accessTokenVerifier = &mockAccessTokenVerifier{
		shouldFail: false,
		claims: &oidc.AccessTokenClaims{
			Scopes: []string{"test-service:read"},
		},
	}

	verifier, err := auth.getAccessTokenVerifier(context.Background())
	require.NoError(t, err, "Aucune erreur ne devrait être retournée")
	require.NotNil(t, verifier, "Le vérificateur de token d'accès ne devrait pas être nil")
	require.Same(t, auth.accessTokenVerifier, verifier, "Le vérificateur devrait être mis en cache")
}

type mockAccessTokenVerifier struct {
	shouldFail bool
	claims     *oidc.AccessTokenClaims
}

func (m *mockAccessTokenVerifier) Verify(ctx context.Context, token string) (string, error) {
	if m.shouldFail {
		return "", fmt.Errorf("token verification failed")
	}
	return "bearer", nil
}

func (m *mockAccessTokenVerifier) Claims(ctx context.Context, token string, claims interface{}) error {
	if m.shouldFail {
		return fmt.Errorf("claims extraction failed")
	}
	
	if accessTokenClaims, ok := claims.(**oidc.AccessTokenClaims); ok {
		*accessTokenClaims = m.claims
		return nil
	}
	
	return fmt.Errorf("unsupported claims type")
}

func (m *mockAccessTokenVerifier) Issuer() string {
	return "https://issuer.example.com"
}

func (m *mockAccessTokenVerifier) KeySet() oidc.KeySet {
	return nil
}

func (m *mockAccessTokenVerifier) MaxAgeIAT() time.Duration {
	return 5 * time.Minute
}

func (m *mockAccessTokenVerifier) Offset() time.Duration {
	return 2 * time.Minute
}

func (m *mockAccessTokenVerifier) SupportedSignAlgs() []string {
	return []string{"RS256"}
}
