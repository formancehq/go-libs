package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/formancehq/go-libs/v3/auth"
	"github.com/formancehq/go-libs/v3/logging"
	"github.com/formancehq/go-libs/v3/oidc"
	"github.com/formancehq/go-libs/v3/publish"
)

// Regression test for TS-456 (ledger):
// auth.Module (with cfg.Enabled=true) and publish's HTTP publisher both supply
// an unnamed *http.Client at the parent fx scope. uber/fx rejects the duplicate
// provider, so a ledger started with --auth-enabled and --publisher-http-enabled
// fails to boot.
func TestAuthAndHTTPPublisherCoexist(t *testing.T) {
	t.Parallel()

	oidcServer := newFakeOIDCServerForTest(t)

	cmd := &cobra.Command{}
	publish.AddFlags("test", cmd.Flags())
	require.NoError(t, cmd.Flags().Set(publish.PublisherHttpEnabledFlag, "true"))
	require.NoError(t, cmd.Flags().Set(publish.PublisherTopicMappingFlag, "*:"+oidcServer.URL))

	var authenticator auth.Authenticator

	app := fxtest.New(t,
		fx.NopLogger,
		fx.Provide(func() context.Context { return context.Background() }),
		fx.Provide(func() logging.Logger { return logging.Testing() }),
		auth.Module(auth.ModuleConfig{
			Enabled: true,
			Issuers: []string{oidcServer.URL},
			Service: "test-service",
		}),
		publish.FXModuleFromFlags(cmd, false),
		fx.Populate(&authenticator),
	)
	require.NoError(t, app.Err())
	app.RequireStart()
	t.Cleanup(func() { app.RequireStop() })

	require.NotNil(t, authenticator)
}

func newFakeOIDCServerForTest(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		_ = json.NewEncoder(w).Encode(oidc.DiscoveryConfiguration{
			Issuer:  scheme + "://" + r.Host,
			JwksURI: scheme + "://" + r.Host + "/.well-known/jwks.json",
		})
	})
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"keys":[]}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}
