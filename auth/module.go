package auth

import (
	"context"
	"net/http"

	"github.com/hashicorp/go-retryablehttp"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v3/oidc"
	"github.com/formancehq/go-libs/v3/oidc/client"
)

type ModuleConfig struct {
	Enabled              bool
	Issuer               string
	ReadKeySetMaxRetries int
	CheckScopes          bool
	Service              string

	AdditionalChecks []AdditionalCheck
}

func Module(cfg ModuleConfig) fx.Option {
	options := ModuleOptions()
	options = append(options, fx.Provide(func() ModuleConfig {
		return cfg
	}))
	return fx.Module("auth", options...)
}

func ModuleOptions() []fx.Option {
	options := make([]fx.Option, 0)

	options = append(options,
		fx.Supply(http.DefaultClient),
		fx.Provide(func(cfg ModuleConfig, httpClient *http.Client) (oidc.KeySet, error) {
			if !cfg.Enabled {
				// this won't be used by the NoAuth
				return oidc.NewStaticKeySet(), nil
			}
			retryableHttpClient := retryablehttp.NewClient()
			retryableHttpClient.RetryMax = cfg.ReadKeySetMaxRetries
			retryableHttpClient.HTTPClient = httpClient

			discovery, err := client.Discover[oidc.DiscoveryConfiguration](
				context.Background(),
				cfg.Issuer,
				retryableHttpClient.StandardClient(),
			)
			if err != nil {
				return nil, err
			}

			return client.NewRemoteKeySet(httpClient, discovery.JwksURI), nil
		}),
	)

	options = append(options,
		fx.Provide(func(cfg ModuleConfig, keySet oidc.KeySet) Authenticator {
			if !cfg.Enabled {
				return NewNoAuth()
			}

			return NewJWTAuth(
				keySet,
				cfg.Issuer,
				cfg.Service,
				cfg.CheckScopes,
				cfg.AdditionalChecks,
			)
		}),
	)
	return options
}
