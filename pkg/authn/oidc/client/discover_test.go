package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
)

func TestDiscoverValidatesIssuer(t *testing.T) {
	t.Parallel()

	t.Run("returns discovery configuration when issuer matches", func(t *testing.T) {
		t.Parallel()

		issuer := "https://issuer.example.com"
		server := httptest.NewServer(discoveryHandler(t, issuer))
		t.Cleanup(server.Close)

		discoveryConfig, err := Discover[oidc.DiscoveryConfiguration](
			context.Background(),
			issuer,
			server.Client(),
			server.URL,
		)

		require.NoError(t, err)
		require.Equal(t, issuer, discoveryConfig.Issuer)
	})

	t.Run("rejects discovery configuration when issuer differs", func(t *testing.T) {
		t.Parallel()

		issuer := "https://issuer.example.com"
		discoveredIssuer := "https://other-issuer.example.com"
		server := httptest.NewServer(discoveryHandler(t, discoveredIssuer))
		t.Cleanup(server.Close)

		discoveryConfig, err := Discover[oidc.DiscoveryConfiguration](
			context.Background(),
			issuer,
			server.Client(),
			server.URL,
		)

		require.Nil(t, discoveryConfig)
		require.ErrorIs(t, err, oidc.ErrDiscoveryFailed)
		require.ErrorIs(t, err, oidc.ErrIssuerInvalid)
		require.ErrorContains(t, err, "Expected: "+issuer+", got: "+discoveredIssuer)
	})
}

func discoveryHandler(t *testing.T, issuer string) http.Handler {
	t.Helper()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(oidc.DiscoveryConfiguration{
			Issuer:  issuer,
			JwksURI: issuer + "/.well-known/jwks.json",
		})
		require.NoError(t, err)
	})
}
