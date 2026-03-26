package jwt

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"

	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/service/apispec"
)

func TestMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("success with valid token", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		authenticator := NewJWTAuth(map[string]oidc.KeySet{issuer: keySet}, "test-service", false, nil)

		// Create access token
		token := createAccessToken(t, privateKey, issuer, "", []string{}, "test-user")

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

		authenticator := NewJWTAuth(map[string]oidc.KeySet{issuer: keySet}, "test-service", false, nil)

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
}

func TestControlPlaneMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("success with valid token", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		authenticator := NewJWTAuth(map[string]oidc.KeySet{issuer: keySet}, "test-service", false, nil)

		// Create access token
		token := createAccessToken(t, privateKey, issuer, "", []string{}, "test-user")

		handler := ControlPlaneMiddleware(authenticator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		authenticator := NewJWTAuth(map[string]oidc.KeySet{issuer: keySet}, "test-service", false, nil)

		handler := ControlPlaneMiddleware(authenticator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		req = req.WithContext(logging.TestingContext())

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		require.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("values from claim are set in context", func(t *testing.T) {
		t.Parallel()
		keySet, privateKey, issuer := setupTestKeySet(t)

		expectedOrgID := "mksgleiucajh"
		provider := func(*http.Request) (orgID string, err error) {
			return expectedOrgID, nil
		}
		additionalChecks := []AdditionalCheck{CheckOrganizationIDClaim(provider)}
		authenticator := NewJWTAuth(map[string]oidc.KeySet{issuer: keySet}, "test-service", false, additionalChecks)

		// Create access token
		token := createAccessTokenWithOrgClaims(t, privateKey, issuer, "", []string{}, "test-user", expectedOrgID)

		handler := ControlPlaneMiddleware(authenticator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, expectedOrgID, r.Context().Value(ContextKeyAuthClaimOrganizationID))
			assert.Equal(t, "test-client", r.Context().Value(ContextKeyAuthClaimClientID))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		}))

		req := httptest.NewRequest("GET", "/test2", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req = req.WithContext(logging.TestingContext())

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, "OK", rr.Body.String())
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

				handler := ControlPlaneMiddleware(authenticator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))

				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Authorization", "Bearer mock-token")
				req = req.WithContext(logging.TestingContext())

				authenticator.EXPECT().AuthenticateOnControlPlane(gomock.Any()).Return(nil, tt.authError)
				rr := httptest.NewRecorder()
				handler.ServeHTTP(rr, req)

				require.Equal(t, http.StatusForbidden, rr.Code)
			})
		}
	})

	t.Run("scope check from openapi spec", func(t *testing.T) {
		t.Parallel()

		loader := openapi3.NewLoader()
		doc, err := loader.LoadFromData([]byte(`
openapi: "3.0.0"
info:
  title: Test API
  version: "1.0"
paths:
  /items:
    get:
      operationId: listItems
      security:
        - oauth2: [someappname:ReadChannel]
      responses:
        "200":
          description: OK
  /items/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
    post:
      operationId: updateItem
      security:
        - oauth2: [someappname:WriteChannel]
      responses:
        "200":
          description: OK
components:
  securitySchemes:
    oauth2:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://example.com/token
          scopes:
            someappname:ReadChannel: Read access
            someappname:WriteChannel: Write access
`))
		require.NoError(t, err)

		keySet, privateKey, issuer := setupTestKeySet(t)
		router := apispec.NewRouter(doc)
		authenticator := NewJWTAuth(
			map[string]oidc.KeySet{issuer: keySet},
			"test-service",
			false,
			[]AdditionalCheck{CheckEndpointSpecificScopesClaim(router)},
		)

		t.Run("allowed when token has required scope", func(t *testing.T) {
			t.Parallel()

			token := createAccessToken(t, privateKey, issuer, "", []string{"someappname:ReadChannel"}, "test-user")
			handler := ControlPlaneMiddleware(authenticator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/items", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req = req.WithContext(logging.TestingContext())

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			require.Equal(t, http.StatusOK, rr.Code)
		})

		t.Run("forbidden when token is missing required scope", func(t *testing.T) {
			t.Parallel()

			token := createAccessToken(t, privateKey, issuer, "", []string{}, "test-user")
			handler := ControlPlaneMiddleware(authenticator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/items", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req = req.WithContext(logging.TestingContext())

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			require.Equal(t, http.StatusForbidden, rr.Code)
		})

		t.Run("path with parameter matched correctly", func(t *testing.T) {
			t.Parallel()

			token := createAccessToken(t, privateKey, issuer, "", []string{"someappname:WriteChannel"}, "test-user")
			handler := ControlPlaneMiddleware(authenticator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("POST", "/items/42", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req = req.WithContext(logging.TestingContext())

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			require.Equal(t, http.StatusOK, rr.Code)
		})

		t.Run("unauthorized when route is not defined in spec", func(t *testing.T) {
			t.Parallel()

			token := createAccessToken(t, privateKey, issuer, "", []string{"someappname:ReadChannel"}, "test-user")
			handler := ControlPlaneMiddleware(authenticator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/not-in-spec", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req = req.WithContext(logging.TestingContext())

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			require.Equal(t, http.StatusForbidden, rr.Code)
		})
	})
}
