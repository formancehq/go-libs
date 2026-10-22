package jwt

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

func TestConfiguredAuthRequiresScopes(t *testing.T) {
	t.Parallel()
	keySet, privateKey, issuer := setupTestKeySet(t)
	for _, tc := range []struct {
		name    string
		enabled bool
		method  string
		scopes  []string
		allowed bool
	}{
		{name: "missing scopes", enabled: true, method: http.MethodGet},
		{name: "another service", enabled: true, method: http.MethodGet, scopes: []string{"other:read"}},
		{name: "read permits GET", enabled: true, method: http.MethodGet, scopes: []string{"test-service:read"}, allowed: true},
		{name: "write permits GET", enabled: true, method: http.MethodGet, scopes: []string{"test-service:write"}, allowed: true},
		{name: "read cannot POST", enabled: true, method: http.MethodPost, scopes: []string{"test-service:read"}},
		{name: "write permits POST", enabled: true, method: http.MethodPost, scopes: []string{"test-service:write"}, allowed: true},
		{name: "disabled auth", method: http.MethodPost, allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authenticator := NewAuthenticatorFromConfig(Config{Enabled: tc.enabled, Service: "test-service"}, map[string]oidc.KeySet{issuer: keySet})
			req := httptest.NewRequest(tc.method, "/test", nil).WithContext(logging.TestingContext())
			if tc.enabled {
				req.Header.Set("Authorization", "Bearer "+createAccessToken(t, privateKey, issuer, "", tc.scopes, "test-user"))
			}
			authenticated, err := authenticator.Authenticate(nil, req)
			if tc.allowed {
				require.NoError(t, err)
				require.True(t, authenticated)
			} else {
				require.ErrorContains(t, err, "missing access")
				require.False(t, authenticated)
			}
			_, err = authenticator.AuthenticateOnControlPlane(req)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "missing access")
			}
			for name, middleware := range map[string]func(http.Handler) http.Handler{
				"standard":      Middleware(authenticator),
				"control plane": ControlPlaneMiddleware(authenticator),
			} {
				t.Run(name, func(t *testing.T) {
					called := false
					handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						called = true
						w.WriteHeader(http.StatusOK)
					}))
					recorder := httptest.NewRecorder()
					handler.ServeHTTP(recorder, req)
					require.Equal(t, tc.allowed, called)
					if tc.allowed {
						require.Equal(t, http.StatusOK, recorder.Code)
					} else {
						require.Equal(t, http.StatusUnauthorized, recorder.Code)
					}
				})
			}
		})
	}
}
