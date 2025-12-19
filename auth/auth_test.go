package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http/httptest"
	"testing"
	stdtime "time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v3/logging"
	"github.com/formancehq/go-libs/v3/oidc"
	libtime "github.com/formancehq/go-libs/v3/time"
)

func setupTestKeySet(t *testing.T) (oidc.KeySet, *rsa.PrivateKey, string) {
	// Generate RSA key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	// Create JSON Web Key
	jwk := jose.JSONWebKey{
		Key:       &privateKey.PublicKey,
		KeyID:     "test-key-id",
		Algorithm: string(jose.RS256),
		Use:       oidc.KeyUseSignature,
	}

	// Create KeySet
	keySet := oidc.NewStaticKeySet(jwk)

	issuer := "https://test-issuer.example.com"

	return keySet, privateKey, issuer
}

func createAccessToken(t *testing.T, privateKey *rsa.PrivateKey, issuer string, scopes []string, subject string) string {
	now := stdtime.Now().UTC()
	expirationTime := libtime.New(now.Add(1 * stdtime.Hour))

	accessTokenClaims := oidc.NewAccessTokenClaims(
		issuer,
		subject,
		[]string{"test-client"},
		expirationTime,
		"test-jti",
		"test-client",
	)

	// Set scopes
	accessTokenClaims.Scopes = scopes

	// Create JWT using go-jose
	signer, err := jose.NewSigner(
		jose.SigningKey{
			Algorithm: jose.RS256,
			Key:       privateKey,
		},
		(&jose.SignerOptions{}).WithHeader("kid", "test-key-id"),
	)
	require.NoError(t, err)

	claimsJSON, err := json.Marshal(accessTokenClaims)
	require.NoError(t, err)

	signed, err := signer.Sign(claimsJSON)
	require.NoError(t, err)

	token, err := signed.CompactSerialize()
	require.NoError(t, err)

	return token
}

func TestJWTAuth_Authenticate(t *testing.T) {
	t.Parallel()

	t.Run("success with valid token", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		auth := NewJWTAuth(keySet, issuer, "test-service", false)

		// Create access token
		token := createAccessToken(t, privateKey, issuer, []string{}, "test-user")

		// Create request with valid token
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.NoError(t, err)
		require.True(t, authenticated)
	})

	t.Run("failure without authorization header", func(t *testing.T) {
		t.Parallel()
		keySet, _, issuer := setupTestKeySet(t)

		auth := NewJWTAuth(keySet, issuer, "test-service", false)

		req := httptest.NewRequest("GET", "/test", nil)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.Error(t, err)
		require.False(t, authenticated)
		require.Contains(t, err.Error(), "no authorization header")
	})

	t.Run("failure with malformed authorization header", func(t *testing.T) {
		t.Parallel()
		keySet, _, issuer := setupTestKeySet(t)

		auth := NewJWTAuth(keySet, issuer, "test-service", false)

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Invalid token")
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.Error(t, err)
		require.False(t, authenticated)
		require.Contains(t, err.Error(), "malformed authorization header")
	})

	t.Run("failure with invalid token", func(t *testing.T) {
		t.Parallel()
		keySet, _, issuer := setupTestKeySet(t)

		auth := NewJWTAuth(keySet, issuer, "test-service", false)

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.Error(t, err)
		require.False(t, authenticated)
	})

	t.Run("failure with expired token", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		auth := NewJWTAuth(keySet, issuer, "test-service", false)

		// Create an expired token
		now := stdtime.Now().UTC()
		expirationTime := libtime.New(now.Add(-1 * stdtime.Hour)) // Expired 1 hour ago

		accessTokenClaims := oidc.NewAccessTokenClaims(
			issuer,
			"test-user",
			[]string{"test-client"},
			expirationTime,
			"test-jti",
			"test-client",
		)

		signer, err := jose.NewSigner(
			jose.SigningKey{
				Algorithm: jose.RS256,
				Key:       privateKey,
			},
			(&jose.SignerOptions{}).WithHeader("kid", "test-key-id"),
		)
		require.NoError(t, err)

		claimsJSON, err := json.Marshal(accessTokenClaims)
		require.NoError(t, err)

		signed, err := signer.Sign(claimsJSON)
		require.NoError(t, err)

		token, err := signed.CompactSerialize()
		require.NoError(t, err)

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.Error(t, err)
		require.False(t, authenticated)
	})

	t.Run("success with valid scopes for GET request", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		auth := NewJWTAuth(keySet, issuer, "test-service", true)

		// Create access token with read scope
		token := createAccessToken(t, privateKey, issuer, []string{"test-service:read"}, "test-user")

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.NoError(t, err)
		require.True(t, authenticated)
	})

	t.Run("success with write scope for POST request", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		auth := NewJWTAuth(keySet, issuer, "test-service", true)

		// Create access token with write scope
		token := createAccessToken(t, privateKey, issuer, []string{"test-service:write"}, "test-user")

		req := httptest.NewRequest("POST", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.NoError(t, err)
		require.True(t, authenticated)
	})

	t.Run("failure with insufficient scopes for POST request", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		auth := NewJWTAuth(keySet, issuer, "test-service", true)

		// Create access token with only read scope (not enough for POST)
		token := createAccessToken(t, privateKey, issuer, []string{"test-service:read"}, "test-user")

		req := httptest.NewRequest("POST", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.Error(t, err)
		require.False(t, authenticated)
		require.Contains(t, err.Error(), "missing access")
	})

	t.Run("success with write scope for GET request", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		auth := NewJWTAuth(keySet, issuer, "test-service", true)

		// Create access token with write scope
		token := createAccessToken(t, privateKey, issuer, []string{"test-service:write"}, "test-user")

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.NoError(t, err)
		require.True(t, authenticated)
	})

	t.Run("failure with different issuer", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)
		unexpectedIssuer := "https://test-issuer.differentdomain.com"

		auth := NewJWTAuth(keySet, issuer, "test-service", false)

		// Create access token
		token := createAccessToken(t, privateKey, unexpectedIssuer, []string{}, "test-user")

		// Create request with valid token
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.Error(t, err)
		assert.False(t, authenticated)
		assert.ErrorIs(t, err, oidc.ErrIssuerInvalid)
	})
}
