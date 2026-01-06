package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"

	"github.com/formancehq/go-libs/v3/logging"
	"github.com/formancehq/go-libs/v3/oidc"
)

func TestMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("success with valid token", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		authenticator := NewJWTAuth(keySet, issuer, "test-service", false, nil)

		// Create access token
		token := createAccessToken(t, privateKey, issuer, []string{}, "test-user")

		handler := Middleware(authenticator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, "OK", rr.Body.String())
	})

	t.Run("failure with invalid token", func(t *testing.T) {
		t.Parallel()
		keySet, _, issuer := setupTestKeySet(t)

		authenticator := NewJWTAuth(keySet, issuer, "test-service", false, nil)

		handler := Middleware(authenticator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		req = req.WithContext(logging.TestingContext())

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		require.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("orgID from claim is set as req header", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		expectedOrgID := "mksgleiucajh"
		provider := func(*http.Request) (orgID string, err error) {
			return expectedOrgID, nil
		}
		additionalChecks := []AdditionalCheck{CheckOrganizationIDClaim(provider)}
		authenticator := NewJWTAuth(keySet, issuer, "test-service", false, additionalChecks)

		// Create access token
		token := createAccessTokenWithOrgClaims(t, privateKey, issuer, []string{}, "test-user", expectedOrgID)

		handler := Middleware(authenticator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, "OK", rr.Body.String())
		assert.Equal(t, expectedOrgID, req.Header.Get(FormanceHeaderOrganizationID))
	})

	t.Run("forbidden", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			authError error
		}{
			{
				name:      "Invalid OrgID",
				authError: fmt.Errorf("err: %w", oidc.ErrOrgIDInvalid),
			},
			{
				name:      "OrgID missing from token",
				authError: fmt.Errorf("err: %w", oidc.ErrOrgIDNotPresent),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				ctrl := gomock.NewController(t)
				authenticator := NewMockAuthenticator(ctrl)

				handler := Middleware(authenticator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))

				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Authorization", "Bearer mock-token")
				req = req.WithContext(logging.TestingContext())

				authenticator.EXPECT().Authenticate(gomock.Any(), gomock.Any()).Return(true, tt.authError)
				rr := httptest.NewRecorder()
				handler.ServeHTTP(rr, req)

				require.Equal(t, http.StatusForbidden, rr.Code)
			})
		}
	})
}
