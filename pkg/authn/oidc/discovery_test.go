package oidc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiscoverValidatesIssuer(t *testing.T) {
	t.Parallel()

	t.Run("returns discovery configuration when issuer matches", func(t *testing.T) {
		t.Parallel()

		var discoveredIssuer string
		server := httptest.NewServer(discoveryConfigurationHandler(&discoveredIssuer))
		t.Cleanup(server.Close)
		discoveredIssuer = server.URL + "/"

		discoveryConfig, err := Discover(context.Background(), server.URL+"/", DiscoveryEndpoint)

		require.NoError(t, err)
		require.Equal(t, discoveredIssuer, discoveryConfig.Issuer)
	})

	t.Run("rejects discovery configuration when issuer differs", func(t *testing.T) {
		t.Parallel()

		discoveredIssuer := "https://other-issuer.example.com"
		server := httptest.NewServer(discoveryConfigurationHandler(&discoveredIssuer))
		t.Cleanup(server.Close)

		discoveryConfig, err := Discover(context.Background(), server.URL, DiscoveryEndpoint)

		require.Nil(t, discoveryConfig)
		require.ErrorIs(t, err, ErrDiscoveryFailed)
		require.ErrorIs(t, err, ErrIssuerInvalid)
		require.ErrorContains(t, err, "Expected: "+server.URL+", got: "+discoveredIssuer)
	})

	t.Run("rejects discovery issuer that only matches after trimming", func(t *testing.T) {
		t.Parallel()

		var discoveredIssuer string
		server := httptest.NewServer(discoveryConfigurationHandler(&discoveredIssuer))
		t.Cleanup(server.Close)
		discoveredIssuer = server.URL + "/"

		discoveryConfig, err := Discover(context.Background(), server.URL, DiscoveryEndpoint)

		require.Nil(t, discoveryConfig)
		require.ErrorIs(t, err, ErrDiscoveryFailed)
		require.ErrorIs(t, err, ErrIssuerInvalid)
		require.ErrorContains(t, err, "Expected: "+server.URL+", got: "+discoveredIssuer)
	})
}

func discoveryConfigurationHandler(issuer *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(DiscoveryConfiguration{
			Issuer:  *issuer,
			JwksURI: *issuer + "/.well-known/jwks.json",
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}
