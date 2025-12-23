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
}

// if the authenticator needs to also check that the organizationID found in the token's claims match the orgID of a
// given resource, OrganizationIDGetterFn can be provided as a way for the authenticator to find the resource's orgID
// if OrganizationIDGetterFn is left nil, a standard JWT authenticator is returned instead
func Module(cfg ModuleConfig, orgIdGetterFn OrganizationIDGetterFn) fx.Option {
	options := make([]fx.Option, 0)

	if !cfg.Enabled {
		options = append(options,
			fx.Provide(func() Authenticator {
				return NewNoAuth()
			}),
		)
		return fx.Module("auth", options...)
	}

	options = append(options,
		fx.Supply(http.DefaultClient),
		fx.Provide(func(httpClient *http.Client) (oidc.KeySet, error) {
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

	if orgIdGetterFn != nil {
		options = append(options,
			fx.Provide(func(keySet oidc.KeySet) Authenticator {
				return NewJWTOrganizationAuth(
					keySet,
					cfg.Issuer,
					cfg.Service,
					cfg.CheckScopes,
					orgIdGetterFn,
				)
			}),
		)
	} else {
		options = append(options,
			fx.Provide(func(keySet oidc.KeySet) Authenticator {
				return NewJWTAuth(
					keySet,
					cfg.Issuer,
					cfg.Service,
					cfg.CheckScopes,
				)
			}),
		)
	}
	return fx.Module("auth", options...)
}
