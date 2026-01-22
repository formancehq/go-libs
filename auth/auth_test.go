package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func createAccessToken(t *testing.T, privateKey *rsa.PrivateKey, issuer string, audience string, scopes []string, subject string) string {
	now := stdtime.Now().UTC()
	expirationTime := libtime.New(now.Add(1 * stdtime.Hour))

	audiences := make([]string, 0, 1)
	if audience != "" {
		audiences = append(audiences, audience)
	}

	accessTokenClaims := oidc.NewAccessTokenClaims(
		issuer,
		subject,
		audiences,
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

func createAccessTokenWithOrgClaims(
	t *testing.T,
	privateKey *rsa.PrivateKey,
	issuer string,
	audience string,
	scopes []string,
	subject string,
	organizationID string,
) string {
	now := stdtime.Now().UTC()
	expirationTime := libtime.New(now.Add(1 * stdtime.Hour))

	audiences := make([]string, 0, 1)
	if audience != "" {
		audiences = append(audiences, audience)
	}

	accessTokenClaims := oidc.NewOrganizationAwareAccessTokenClaims(
		issuer,
		subject,
		audiences,
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

func TestJWTAuth_Authenticate(t *testing.T) {
	t.Parallel()

	autoPassingAdditionalChecks := []AdditionalCheck{
		func(*http.Request, *oidc.AccessTokenClaims) error { return nil },
	}

	t.Run("success with valid token", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		auth := NewJWTAuth(keySet, issuer, "test-service", false, []AdditionalCheck{})

		// Create access token
		token := createAccessToken(t, privateKey, issuer, "", []string{}, "test-user")

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
		tests := []struct {
			name string
			auth Authenticator
		}{
			{
				name: "JWTAuth",
				auth: NewJWTAuth(keySet, issuer, "test-service", false, []AdditionalCheck{}),
			},
			{
				name: "JWTAuth with additional checks",
				auth: NewJWTAuth(keySet, issuer, "test-service", false, autoPassingAdditionalChecks),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {

				req := httptest.NewRequest("GET", "/test", nil)
				req = req.WithContext(logging.TestingContext())

				authenticated, err := tt.auth.Authenticate(nil, req)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "no authorization header")
				assert.False(t, authenticated)
			})
		}
	})

	t.Run("failure with malformed authorization header", func(t *testing.T) {
		t.Parallel()
		keySet, _, issuer := setupTestKeySet(t)
		tests := []struct {
			name string
			auth Authenticator
		}{
			{
				name: "JWTAuth",
				auth: NewJWTAuth(keySet, issuer, "test-service", false, nil),
			},
			{
				name: "JWTAuth with additional checks",
				auth: NewJWTAuth(keySet, issuer, "test-service", false, autoPassingAdditionalChecks),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Authorization", "Invalid token")
				req = req.WithContext(logging.TestingContext())

				authenticated, err := tt.auth.Authenticate(nil, req)
				require.Error(t, err)
				assert.False(t, authenticated)
				assert.Contains(t, err.Error(), "malformed authorization header")
			})
		}
	})

	t.Run("failure with invalid token", func(t *testing.T) {
		t.Parallel()
		keySet, _, issuer := setupTestKeySet(t)
		tests := []struct {
			name string
			auth Authenticator
		}{
			{
				name: "JWTAuth",
				auth: NewJWTAuth(keySet, issuer, "test-service", false, nil),
			},
			{
				name: "JWTAuth with additional checks",
				auth: NewJWTAuth(keySet, issuer, "test-service", false, autoPassingAdditionalChecks),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Authorization", "Bearer invalid-token")
				req = req.WithContext(logging.TestingContext())

				authenticated, err := tt.auth.Authenticate(nil, req)
				require.Error(t, err)
				require.False(t, authenticated)
			})
		}
	})

	t.Run("failure with expired token", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)
		tests := []struct {
			name string
			auth Authenticator
		}{
			{
				name: "JWTAuth",
				auth: NewJWTAuth(keySet, issuer, "test-service", false, nil),
			},
			{
				name: "JWTAuth with additional checks",
				auth: NewJWTAuth(keySet, issuer, "test-service", false, autoPassingAdditionalChecks),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
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

				authenticated, err := tt.auth.Authenticate(nil, req)
				require.Error(t, err)
				assert.False(t, authenticated)
			})
		}
	})

	t.Run("success with valid scopes for GET request", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		tests := []struct {
			name string
			auth Authenticator
		}{
			{
				name: "JWTAuth",
				auth: NewJWTAuth(keySet, issuer, "test-service", true, nil),
			},
			{
				name: "JWTAuth with additional checks",
				auth: NewJWTAuth(keySet, issuer, "test-service", true, autoPassingAdditionalChecks),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {

				// Create access token with read scope
				token := createAccessToken(t, privateKey, issuer, "", []string{"test-service:read"}, "test-user")

				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				req = req.WithContext(logging.TestingContext())

				authenticated, err := tt.auth.Authenticate(nil, req)
				require.NoError(t, err)
				assert.True(t, authenticated)
			})
		}
	})

	t.Run("success with write scope for POST request", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		tests := []struct {
			name string
			auth Authenticator
		}{
			{
				name: "JWTAuth",
				auth: NewJWTAuth(keySet, issuer, "test-service", true, nil),
			},
			{
				name: "JWTAuth with additional checks",
				auth: NewJWTAuth(keySet, issuer, "test-service", true, autoPassingAdditionalChecks),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// Create access token with write scope
				token := createAccessToken(t, privateKey, issuer, "", []string{"test-service:write"}, "test-user")

				req := httptest.NewRequest("POST", "/test", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				req = req.WithContext(logging.TestingContext())

				authenticated, err := tt.auth.Authenticate(nil, req)
				require.NoError(t, err)
				assert.True(t, authenticated)
			})
		}
	})

	t.Run("failure with insufficient scopes for POST request", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		tests := []struct {
			name string
			auth Authenticator
		}{
			{
				name: "JWTAuth",
				auth: NewJWTAuth(keySet, issuer, "test-service", true, nil),
			},
			{
				name: "JWTAuth with additional checks",
				auth: NewJWTAuth(keySet, issuer, "test-service", true, autoPassingAdditionalChecks),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// Create access token with only read scope (not enough for POST)
				token := createAccessToken(t, privateKey, issuer, "", []string{"test-service:read"}, "test-user")

				req := httptest.NewRequest("POST", "/test", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				req = req.WithContext(logging.TestingContext())

				authenticated, err := tt.auth.Authenticate(nil, req)
				require.Error(t, err)
				assert.False(t, authenticated)
				assert.Contains(t, err.Error(), "missing access")
			})
		}
	})

	t.Run("success with write scope for GET request", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		tests := []struct {
			name string
			auth Authenticator
		}{
			{
				name: "JWTAuth",
				auth: NewJWTAuth(keySet, issuer, "test-service", true, nil),
			},
			{
				name: "JWTAuth with additional checks",
				auth: NewJWTAuth(keySet, issuer, "test-service", true, autoPassingAdditionalChecks),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// Create access token with write scope
				token := createAccessToken(t, privateKey, issuer, "", []string{"test-service:write"}, "test-user")

				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				req = req.WithContext(logging.TestingContext())

				authenticated, err := tt.auth.Authenticate(nil, req)
				require.NoError(t, err)
				assert.True(t, authenticated)
			})
		}
	})

	t.Run("failure with different issuer", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)
		unexpectedIssuer := "https://test-issuer.differentdomain.com"

		tests := []struct {
			name string
			auth Authenticator
		}{
			{
				name: "JWTAuth",
				auth: NewJWTAuth(keySet, issuer, "test-service", false, nil),
			},
			{
				name: "JWTAuth with additional checks",
				auth: NewJWTAuth(keySet, issuer, "test-service", false, autoPassingAdditionalChecks),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// Create access token
				token := createAccessToken(t, privateKey, unexpectedIssuer, "", []string{}, "test-user")

				// Create request with valid token
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				req = req.WithContext(logging.TestingContext())

				authenticated, err := tt.auth.Authenticate(nil, req)
				require.Error(t, err)
				assert.False(t, authenticated)
				assert.ErrorIs(t, err, oidc.ErrIssuerInvalid)
			})
		}
	})

	t.Run("failure due to additional check", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		var additionalChecksPerformed = 0

		expectedErr := errors.New("expected")
		autoFailingAdditionalChecks := []AdditionalCheck{
			func(*http.Request, *oidc.AccessTokenClaims) error {
				additionalChecksPerformed++
				return nil
			},
			func(*http.Request, *oidc.AccessTokenClaims) error {
				additionalChecksPerformed++
				return expectedErr
			},
			func(*http.Request, *oidc.AccessTokenClaims) error {
				additionalChecksPerformed++
				return nil
			},
		}

		auth := NewJWTAuth(keySet, issuer, "test-service", false, autoFailingAdditionalChecks)

		// Create access token
		token := createAccessToken(t, privateKey, issuer, "", []string{}, "test-user")

		// Create request with valid token
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.Error(t, err)
		require.False(t, authenticated)
		assert.ErrorIs(t, err, expectedErr)
		assert.Equal(t, 2, additionalChecksPerformed)
	})

	t.Run("CheckOrganizationIDClaim success with valid token and correct orgID", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)
		expectedOrgID := "abcdefghijkl"

		provider := func(*http.Request) (string, error) { return expectedOrgID, nil }
		additionalChecks := []AdditionalCheck{
			CheckOrganizationIDClaim(provider),
		}

		auth := NewJWTAuth(keySet, issuer, "test-service", false, additionalChecks)

		// Create access token
		token := createAccessTokenWithOrgClaims(t, privateKey, issuer, "", []string{}, "test-user", expectedOrgID)

		// Create request with valid token
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.NoError(t, err)
		assert.True(t, authenticated)
	})

	t.Run("CheckOrganizationIDClaim success with valid token and no expected orgID", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		provider := func(*http.Request) (string, error) { return "", nil }
		additionalChecks := []AdditionalCheck{
			CheckOrganizationIDClaim(provider),
		}
		auth := NewJWTAuth(keySet, issuer, "test-service", false, additionalChecks)

		// Create access token
		token := createAccessTokenWithOrgClaims(t, privateKey, issuer, "", []string{}, "test-user", "")

		// Create request with valid token
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.NoError(t, err)
		assert.True(t, authenticated)
	})

	t.Run("CheckOrganizationIDClaim failure with valid token and mismatched orgID", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)
		expectedOrgID := "abcdefghijkl"

		provider := func(*http.Request) (string, error) { return expectedOrgID, nil }
		additionalChecks := []AdditionalCheck{
			CheckOrganizationIDClaim(provider),
		}
		auth := NewJWTAuth(keySet, issuer, "test-service", false, additionalChecks)

		// Create access token
		token := createAccessTokenWithOrgClaims(t, privateKey, issuer, "", []string{}, "test-user", "someotherorgid")

		// Create request with valid token
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.Error(t, err)
		assert.ErrorIs(t, err, oidc.ErrOrgIDInvalid)
		assert.False(t, authenticated)
	})

	t.Run("CheckOrganizationIDClaim failure with token that doesn't contain orgID", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)
		expectedOrgID := "abcdefghijkl"

		provider := func(*http.Request) (string, error) { return expectedOrgID, nil }
		additionalChecks := []AdditionalCheck{
			CheckOrganizationIDClaim(provider),
		}
		auth := NewJWTAuth(keySet, issuer, "test-service", false, additionalChecks)

		// Create access token
		token := createAccessTokenWithOrgClaims(t, privateKey, issuer, "", []string{}, "test-user", "")

		// Create request with valid token
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.Error(t, err)
		assert.ErrorIs(t, err, oidc.ErrOrgIDNotPresent)
		assert.False(t, authenticated)
	})

	t.Run("CheckAudienceClaim audience mismatches", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)
		expectedAudience, err := url.Parse("http://expected.mydomain.com")
		require.NoError(t, err)

		additionalChecks := []AdditionalCheck{
			CheckAudienceClaim(*expectedAudience),
		}
		auth := NewJWTAuth(keySet, issuer, "test-service", false, additionalChecks)

		// Create access token
		token := createAccessTokenWithOrgClaims(t, privateKey, issuer, "", []string{}, "test-user", "")

		// Create request with valid token
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.Error(t, err)
		assert.ErrorIs(t, err, oidc.ErrAudience)
		assert.False(t, authenticated)
	})

	t.Run("CheckAudienceClaim audience matches", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)
		expectedAudience, err := url.Parse("http://expected.mydomain.com")
		require.NoError(t, err)

		additionalChecks := []AdditionalCheck{
			CheckAudienceClaim(*expectedAudience),
		}
		auth := NewJWTAuth(keySet, issuer, "test-service", false, additionalChecks)

		// Create access token
		tokenAudience := expectedAudience.Host
		token := createAccessTokenWithOrgClaims(t, privateKey, issuer, tokenAudience, []string{}, "test-user", "")

		// Create request with valid token
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		authenticated, err := auth.Authenticate(nil, req)
		require.NoError(t, err)
		assert.True(t, authenticated)
	})
}
