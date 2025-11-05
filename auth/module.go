package auth

import (
	"context"
	"net/http"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/formancehq/go-libs/v3/oidc"
	"github.com/formancehq/go-libs/v3/oidc/client"
	"go.uber.org/fx"
)

type ModuleConfig struct {
	Enabled              bool
	Issuer               string
	ReadKeySetMaxRetries int
	CheckScopes          bool
	Service              string
}

func Module(cfg ModuleConfig) fx.Option {
	options := make([]fx.Option, 0)

	options = append(options,
		fx.Provide(func() Authenticator {
			return NewNoAuth()
		}),
	)

	if cfg.Enabled {
		options = append(options,
			fx.Provide(func() *http.Client {
				httpClient := retryablehttp.NewClient()
				httpClient.RetryMax = cfg.ReadKeySetMaxRetries
				return httpClient.StandardClient()
			}),
			fx.Provide(func(ctx context.Context, httpClient *http.Client) (oidc.KeySet, error) {
				discovery, err := client.Discover[oidc.DiscoveryConfiguration](ctx, cfg.Issuer, httpClient)
				if err != nil {
					return nil, err
				}

				return client.NewRemoteKeySet(httpClient, discovery.JwksURI), nil
			}),
			fx.Decorate(func(keySet oidc.KeySet) Authenticator {
				return NewJWTAuth(
					keySet,
					cfg.Issuer,
					cfg.Service,
					cfg.CheckScopes,
				)
			}),
		)
	}

	return fx.Options(options...)
}
