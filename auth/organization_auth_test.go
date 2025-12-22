package auth

import (
	"crypto/rsa"
	"net/http"
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

func createAccessTokenWithOrgClaims(
	t *testing.T,
	privateKey *rsa.PrivateKey,
	issuer string,
	scopes []string,
	subject string,
	organizationID string,
) string {
	now := stdtime.Now().UTC()
	expirationTime := libtime.New(now.Add(1 * stdtime.Hour))

	accessTokenClaims := oidc.NewOrganizationAwareAccessTokenClaims(
		issuer,
		subject,
		[]string{"test-client"},
		expirationTime,
		"test-jti",
		"test-client",
	)

	// Set scopes
	accessTokenClaims.Scopes = scopes

	privateClaims := map[string]interface{}{}
	if organizationID != "" {
		privateClaims[oidc.ClaimOrganizationID] = organizationID
	}
	accessTokenClaims.Claims = privateClaims

	// Create JWT using go-jose
	signer, err := jose.NewSigner(
		jose.SigningKey{
			Algorithm: jose.RS256,
			Key:       privateKey,
		},
		(&jose.SignerOptions{}).WithHeader("kid", "test-key-id"),
	)
	require.NoError(t, err)

	claimsJSON, err := accessTokenClaims.MarshalJSON()
	require.NoError(t, err)

	signed, err := signer.Sign(claimsJSON)
	require.NoError(t, err)

	token, err := signed.CompactSerialize()
	require.NoError(t, err)

	return token
}

func TestJWTOrgAuth_Authenticate(t *testing.T) {
	t.Parallel()

	noOrgGetterFn := func(*http.Request) (string, error) { return "", nil }

	t.Run("success with valid token and correct orgID", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)
		expectedOrgID := "abcdefghijkl"

		auth := NewJWTOrganizationAuth(keySet, issuer, "test-service", false, func(*http.Request) (string, error) { return expectedOrgID, nil })

		// Create access token
		token := createAccessTokenWithOrgClaims(t, privateKey, issuer, []string{}, "test-user", expectedOrgID)

		// Create request with valid token
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.NoError(t, err)
		assert.True(t, authenticated)
	})

	t.Run("success with valid token and no expected orgID", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		auth := NewJWTOrganizationAuth(keySet, issuer, "test-service", false, noOrgGetterFn)

		// Create access token
		token := createAccessTokenWithOrgClaims(t, privateKey, issuer, []string{}, "test-user", "")

		// Create request with valid token
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.NoError(t, err)
		assert.True(t, authenticated)
	})

	t.Run("failure with valid token and mismatched orgID", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)
		expectedOrgID := "abcdefghijkl"

		auth := NewJWTOrganizationAuth(keySet, issuer, "test-service", false, func(*http.Request) (string, error) { return expectedOrgID, nil })

		// Create access token
		token := createAccessTokenWithOrgClaims(t, privateKey, issuer, []string{}, "test-user", "someotherorgid")

		// Create request with valid token
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.Error(t, err)
		assert.ErrorIs(t, err, oidc.ErrOrgIDInvalid)
		assert.False(t, authenticated)
	})

	t.Run("failure with token that doesn't contain orgID", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)
		expectedOrgID := "abcdefghijkl"

		auth := NewJWTOrganizationAuth(keySet, issuer, "test-service", false, func(*http.Request) (string, error) { return expectedOrgID, nil })

		// Create access token
		token := createAccessTokenWithOrgClaims(t, privateKey, issuer, []string{}, "test-user", "")

		// Create request with valid token
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.Error(t, err)
		assert.ErrorIs(t, err, oidc.ErrOrgIDNotPresent)
		assert.False(t, authenticated)
	})

	// more generic tests for this Authenticator in auth_test.go
}
