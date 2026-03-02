package jwt_test

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

	auth "github.com/formancehq/go-libs/v5/pkg/authn/jwt"
	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
	"github.com/formancehq/go-libs/v5/pkg/fx/authnfx"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
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
			authnfx.JWTModule(auth.Config{
				Enabled:     true,
				Issuers:     []string{issuer},
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
			authnfx.JWTModule(auth.Config{
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

		_, issuer, _ := setupTestOIDCServer(t)

		var authenticator auth.Authenticator

		// Use fx.Decorate to override the Authenticator.
		// With multi-issuer support, discovery happens inside the Authenticator
		// provider, so decorating the Authenticator itself is the correct pattern.
		options := []fx.Option{
			authnfx.JWTModule(auth.Config{
				Enabled:     true,
				Issuers:     []string{issuer},
				Service:     "test-service",
				CheckScopes: false,
			}),
			fx.Provide(func() context.Context {
				return context.Background()
			}),
			fx.Provide(func() logging.Logger {
				return logging.Testing()
			}),
			fx.Decorate(func() auth.Authenticator {
				return auth.NewJWTAuth(
					map[string]oidc.KeySet{issuer: customKeySet},
					"test-service",
					false,
					nil,
				)
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

		// Verify that the authenticator is using our custom key set
		// by checking it's a *JWTAuth (not NoAuth)
		_, ok := authenticator.(*auth.JWTAuth)
		require.True(t, ok, "Authenticator should be a JWTAuth")
	})

	t.Run("module with disabled auth does not call discovery", func(t *testing.T) {
		t.Parallel()

		_, issuer, discoveryCalled := setupTestOIDCServer(t)

		var authenticator auth.Authenticator

		options := []fx.Option{
			authnfx.JWTModule(auth.Config{
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
			authnfx.AnnotatedJWTModule(auth.Config{
				Enabled:     true,
				Issuer:      issuer1,
				Service:     "test-service",
				CheckScopes: false,
			}, "one"),
			authnfx.AnnotatedJWTModule(auth.Config{
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
