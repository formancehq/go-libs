package jwt_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	auth "github.com/formancehq/go-libs/v5/pkg/authn/jwt"
	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
	"github.com/formancehq/go-libs/v5/pkg/service/apispec"
)

var scopeCheckSpec = []byte(`
openapi: "3.0.0"
info:
  title: T
  version: "1.0"
components:
  securitySchemes:
    oauth2:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://example.com/token
          scopes: {}
paths:
  /secured:
    get:
      operationId: secured
      security:
        - oauth2: [ledger:ReadResource]
      responses:
        "200":
          description: OK
  /open:
    get:
      operationId: open
      responses:
        "200":
          description: OK
  /multi-scope:
    get:
      operationId: multiScope
      security:
        - oauth2: [ledger:ReadResource, ledger:WriteResource]
      responses:
        "200":
          description: OK
`)

func newScopeCheckRouter(t *testing.T) *apispec.Router {
	t.Helper()
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(scopeCheckSpec)
	require.NoError(t, err)
	return apispec.NewRouter(doc)
}

func claimsWithScopes(scopes ...string) *oidc.AccessTokenClaims {
	c := &oidc.AccessTokenClaims{}
	c.Scopes = scopes
	return c
}

func TestCheckEndpointSpecificScopesClaim(t *testing.T) {
	t.Parallel()

	t.Run("nil claims returns error", func(t *testing.T) {
		t.Parallel()
		router := newScopeCheckRouter(t)
		check := auth.CheckEndpointSpecificScopesClaim(router)
		req := httptest.NewRequest(http.MethodGet, "/secured", nil)
		err := check(req, nil)
		assert.EqualError(t, err, "claims cannot be nil")
	})

	t.Run("nil router returns error", func(t *testing.T) {
		t.Parallel()
		check := auth.CheckEndpointSpecificScopesClaim(nil)
		req := httptest.NewRequest(http.MethodGet, "/secured", nil)
		err := check(req, claimsWithScopes())
		assert.EqualError(t, err, "router cannot be nil")
	})

	t.Run("route not found returns error", func(t *testing.T) {
		t.Parallel()
		router := newScopeCheckRouter(t)
		check := auth.CheckEndpointSpecificScopesClaim(router)
		req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
		err := check(req, claimsWithScopes())
		require.Error(t, err)
		assert.Contains(t, err.Error(), auth.ErrUndocumentedRoute.Error())
	})

	t.Run("route with no scope requirement succeeds regardless of token scopes", func(t *testing.T) {
		t.Parallel()
		router := newScopeCheckRouter(t)
		check := auth.CheckEndpointSpecificScopesClaim(router)
		req := httptest.NewRequest(http.MethodGet, "/open", nil)
		assert.NoError(t, check(req, claimsWithScopes()))
	})

	t.Run("token has required scope succeeds", func(t *testing.T) {
		t.Parallel()
		router := newScopeCheckRouter(t)
		check := auth.CheckEndpointSpecificScopesClaim(router)
		req := httptest.NewRequest(http.MethodGet, "/secured", nil)
		assert.NoError(t, check(req, claimsWithScopes("ledger:ReadResource")))
	})

	t.Run("token has required scope among many succeeds", func(t *testing.T) {
		t.Parallel()
		router := newScopeCheckRouter(t)
		check := auth.CheckEndpointSpecificScopesClaim(router)
		req := httptest.NewRequest(http.MethodGet, "/secured", nil)
		assert.NoError(t, check(req, claimsWithScopes("ledger:WriteResource", "ledger:ReadResource", "admin")))
	})

	t.Run("token missing required scope returns ErrMissingScope", func(t *testing.T) {
		t.Parallel()
		router := newScopeCheckRouter(t)
		check := auth.CheckEndpointSpecificScopesClaim(router)
		req := httptest.NewRequest(http.MethodGet, "/secured", nil)
		err := check(req, claimsWithScopes())
		require.Error(t, err)
		assert.ErrorIs(t, err, auth.ErrMissingScope)
	})

	t.Run("token has unrelated scope returns ErrMissingScope", func(t *testing.T) {
		t.Parallel()
		router := newScopeCheckRouter(t)
		check := auth.CheckEndpointSpecificScopesClaim(router)
		req := httptest.NewRequest(http.MethodGet, "/secured", nil)
		err := check(req, claimsWithScopes("ledger:WriteResource", "admin"))
		require.Error(t, err)
		assert.ErrorIs(t, err, auth.ErrMissingScope)
	})

	t.Run("token has all required scopes for multi-scope endpoint succeeds", func(t *testing.T) {
		t.Parallel()
		router := newScopeCheckRouter(t)
		check := auth.CheckEndpointSpecificScopesClaim(router)
		req := httptest.NewRequest(http.MethodGet, "/multi-scope", nil)
		assert.NoError(t, check(req, claimsWithScopes("ledger:ReadResource", "ledger:WriteResource")))
	})

	t.Run("token missing one scope for multi-scope endpoint returns ErrMissingScope", func(t *testing.T) {
		t.Parallel()
		router := newScopeCheckRouter(t)
		check := auth.CheckEndpointSpecificScopesClaim(router)
		req := httptest.NewRequest(http.MethodGet, "/multi-scope", nil)
		err := check(req, claimsWithScopes("ledger:ReadResource"))
		require.Error(t, err)
		assert.ErrorIs(t, err, auth.ErrMissingScope)
	})
}

func TestCheckAudienceClaim(t *testing.T) {
	tests := map[string]struct {
		expectedAudienceStr string
		claims              *oidc.AccessTokenClaims
		expectedError       error
	}{
		"NilClaims": {
			claims:        nil,
			expectedError: errors.New("claims cannot be nil"),
		},
		"MatchingAudience with url scheme": {
			expectedAudienceStr: "http://example.com",
			claims: &oidc.AccessTokenClaims{
				TokenClaims: oidc.TokenClaims{
					Audience: []string{"http://example.com"},
				},
			},
			expectedError: nil,
		},
		"NonMatchingAudience with url scheme": {
			expectedAudienceStr: "http://example.com",
			claims: &oidc.AccessTokenClaims{
				TokenClaims: oidc.TokenClaims{
					Audience: []string{"http://another.com"},
				},
			},
			expectedError: oidc.ErrAudience,
		},
		"Multiple audiences in claim; one matches": {
			expectedAudienceStr: "example.com",
			claims: &oidc.AccessTokenClaims{
				TokenClaims: oidc.TokenClaims{
					Audience: []string{"otherdomain.com", "example.com", "123.com"},
				},
			},
			expectedError: nil,
		},
		"Multiple audiences in claim but none match": {
			expectedAudienceStr: "http://example.com",
			claims: &oidc.AccessTokenClaims{
				TokenClaims: oidc.TokenClaims{
					Audience: []string{"another.com", "ple.com", "subdomain.example.com"},
				},
			},
			expectedError: oidc.ErrAudience,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			check := auth.CheckAudienceClaim(tt.expectedAudienceStr)
			err := check(nil, tt.claims)
			assert.Equal(t, tt.expectedError, err)
		})
	}
}
