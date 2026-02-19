package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/formancehq/go-libs/v4/auth"
	"github.com/formancehq/go-libs/v4/logging"
	"github.com/formancehq/go-libs/v4/oidc"
)

// setupTestOIDCServer creates an HTTP test server that simulates an OIDC provider
// Returns the server, the issuer URL, and a channel to track discovery requests
func setupTestOIDCServer(t *testing.T) (*httptest.Server, string, chan bool) {
	discoveryCalled := make(chan bool, 1)

	mux := http.NewServeMux()

	// Discovery endpoint
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		discoveryCalled <- true

		config := oidc.DiscoveryConfiguration{
			Issuer:  r.URL.Scheme + "://" + r.Host,
			JwksURI: r.URL.Scheme + "://" + r.Host + "/.well-known/jwks.json",
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(config)
	})

	// JWKS endpoint (not used in tests, but required for a valid OIDC server)
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"keys": []interface{}{},
		})
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	issuer := server.URL

	return server, issuer, discoveryCalled
}

func TestModule(t *testing.T) {
	t.Parallel()

	t.Run("module calls discovery endpoint when enabled", func(t *testing.T) {
		t.Parallel()
		_, issuer, discoveryCalled := setupTestOIDCServer(t)

		var authenticator auth.Authenticator

		options := []fx.Option{
			auth.Module(auth.ModuleConfig{
				Enabled:     true,
				Issuer:      issuer,
				Service:     "test-service",
				CheckScopes: false,
			}),
			fx.Provide(func() context.Context {
				return context.Background()
			}),
			fx.Provide(func() logging.Logger {
				return logging.Testing()
			}),
			fx.Populate(&authenticator),
		}

		if !testing.Verbose() {
			options = append(options, fx.NopLogger)
		}

		app := fxtest.New(t, options...)
		app.RequireStart()
		defer app.RequireStop()

		require.NotNil(t, authenticator)

		// Verify that the discovery endpoint was called
		select {
		case called := <-discoveryCalled:
			require.True(t, called, "Discovery endpoint should have been called")
		default:
			t.Fatal("Discovery endpoint was not called")
		}
	})

	t.Run("module with additional checks calls discovery endpoint when enabled", func(t *testing.T) {
		t.Parallel()
		_, issuer, discoveryCalled := setupTestOIDCServer(t)

		var authenticator auth.Authenticator

		provider := func(*http.Request) (string, error) { return "dummy", nil }
		additionalChecks := []auth.AdditionalCheck{
			auth.CheckOrganizationIDClaim(provider),
		}

		options := []fx.Option{
			auth.Module(auth.ModuleConfig{
				Enabled:          true,
				Issuer:           issuer,
				Service:          "test-service-with-orgId-aware-auth",
				CheckScopes:      false,
				AdditionalChecks: additionalChecks,
			}),
			fx.Provide(func() context.Context {
				return context.Background()
			}),
			fx.Provide(func() logging.Logger {
				return logging.Testing()
			}),
			fx.Populate(&authenticator),
		}

		if !testing.Verbose() {
			options = append(options, fx.NopLogger)
		}

		app := fxtest.New(t, options...)
		app.RequireStart()
		defer app.RequireStop()

		require.NotNil(t, authenticator)

		// Verify that the discovery endpoint was called
		select {
		case called := <-discoveryCalled:
			require.True(t, called, "Discovery endpoint should have been called")
		default:
			t.Fatal("Discovery endpoint was not called")
		}
	})

	t.Run("module can be overridden with fx.Decorate", func(t *testing.T) {
		t.Parallel()

		// Create a custom KeySet with a different key
		customPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)

		customJWK := jose.JSONWebKey{
			Key:       &customPrivateKey.PublicKey,
			KeyID:     "custom-key-id",
			Algorithm: string(jose.RS256),
			Use:       oidc.KeyUseSignature,
		}

		customKeySet := oidc.NewStaticKeySet(customJWK)

		_, issuer, discoveryCalled := setupTestOIDCServer(t)

		var authenticator auth.Authenticator

		// Use fx.Decorate to intercept and override the KeySet provider
		// fx.Decorate wraps the original provider and allows us to return our custom KeySet
		// This prevents the module's provider from executing the OIDC discovery
		options := []fx.Option{
			auth.Module(auth.ModuleConfig{
				Enabled:     true,
				Issuer:      issuer,
				Service:     "test-service",
				CheckScopes: false,
			}),
			fx.Provide(func() context.Context {
				return context.Background()
			}),
			fx.Provide(func() logging.Logger {
				return logging.Testing()
			}),
			// Decorate the KeySet provider to return our custom KeySet
			// This intercepts the provider before it tries to discover the OIDC endpoint
			fx.Decorate(func(ctx context.Context, httpClient *http.Client) (oidc.KeySet, error) {
				// Return our custom KeySet instead of calling the original provider
				return customKeySet, nil
			}),
			fx.Populate(&authenticator),
		}

		if !testing.Verbose() {
			options = append(options, fx.NopLogger)
		}

		app := fxtest.New(t, options...)
		app.RequireStart()
		defer app.RequireStop()

		require.NotNil(t, authenticator)

		// Verify that the discovery endpoint was NOT called (because we used fx.Decorate)
		select {
		case <-discoveryCalled:
			t.Fatal("Discovery endpoint should NOT have been called when using fx.Decorate")
		default:
			// Good, discovery was not called
		}
	})

	t.Run("module with disabled auth does not call discovery", func(t *testing.T) {
		t.Parallel()

		_, issuer, discoveryCalled := setupTestOIDCServer(t)

		var authenticator auth.Authenticator

		options := []fx.Option{
			auth.Module(auth.ModuleConfig{
				Enabled:     false,
				Issuer:      issuer,
				Service:     "test-service",
				CheckScopes: false,
			}),
			fx.Populate(&authenticator),
		}

		if !testing.Verbose() {
			options = append(options, fx.NopLogger)
		}

		app := fxtest.New(t, options...)
		app.RequireStart()
		defer app.RequireStop()

		require.NotNil(t, authenticator)

		// Verify that the discovery endpoint was NOT called when auth is disabled
		select {
		case <-discoveryCalled:
			t.Fatal("Discovery endpoint should NOT have been called when auth is disabled")
		default:
			// Good, discovery was not called
		}
	})

	t.Run("module can be annotated and used distinctly", func(t *testing.T) {
		t.Parallel()

		_, issuer1, discoveryCalled1 := setupTestOIDCServer(t)
		_, issuer2, discoveryCalled2 := setupTestOIDCServer(t)

		var authenticator auth.Authenticator
		var authenticator2 auth.Authenticator

		options := []fx.Option{
			auth.AnnotatedModule(auth.ModuleConfig{
				Enabled:     true,
				Issuer:      issuer1,
				Service:     "test-service",
				CheckScopes: false,
			}, "one"),
			auth.AnnotatedModule(auth.ModuleConfig{
				Enabled:     true,
				Issuer:      issuer2,
				Service:     "test-service",
				CheckScopes: false,
			}, "two"),
			fx.Populate(
				fx.Annotate(&authenticator, fx.ParamTags(`name:"one"`)),
				fx.Annotate(&authenticator2, fx.ParamTags(`name:"two"`)),
			),
		}

		if !testing.Verbose() {
			options = append(options, fx.NopLogger)
		}

		app := fxtest.New(t, options...)
		app.RequireStart()
		defer app.RequireStop()

		require.NotNil(t, authenticator)
		require.NotNil(t, authenticator2)

		select {
		case called := <-discoveryCalled1:
			require.True(t, called, "Discovery endpoint for issuer 1 should have been called")
		default:
			t.Fatal("Discovery endpoint for issuer 1 was not called")
		}

		select {
		case called := <-discoveryCalled2:
			require.True(t, called, "Discovery endpoint for issuer 2 should have been called")
		default:
			t.Fatal("Discovery endpoint for issuer 2 was not called")
		}
	})
}
