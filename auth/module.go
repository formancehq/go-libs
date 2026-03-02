package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/hashicorp/go-retryablehttp"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v3/oidc"
	"github.com/formancehq/go-libs/v3/oidc/client"
)

type ModuleConfig struct {
	Enabled              bool
	Issuers              []string
	ReadKeySetMaxRetries int
	CheckScopes          bool
	Service              string

	// Deprecated: use Issuers instead.
	Issuer string
}

func (cfg ModuleConfig) resolveIssuers() []string {
	issuers := cfg.Issuers
	if cfg.Issuer != "" {
		found := false
		for _, iss := range issuers {
			if iss == cfg.Issuer {
				found = true
				break
			}
		}
		if !found {
			issuers = append(issuers, cfg.Issuer)
		}
	}
	return issuers
}

func Module(cfg ModuleConfig) fx.Option {
	options := make([]fx.Option, 0)

	issuers := cfg.resolveIssuers()

	if cfg.Enabled && len(issuers) == 0 {
		return fx.Error(errors.New("auth is enabled but no issuers are configured"))
	}

	if cfg.Enabled {
		options = append(options,
			fx.Supply(http.DefaultClient),
			fx.Provide(func(httpClient *http.Client) (Authenticator, error) {
				retryableHttpClient := retryablehttp.NewClient()
				retryableHttpClient.RetryMax = cfg.ReadKeySetMaxRetries
				retryableHttpClient.HTTPClient = httpClient
				discoveryHTTPClient := retryableHttpClient.StandardClient()

				keySets := make(map[string]oidc.KeySet, len(issuers))
				for _, issuer := range issuers {
					discovery, err := client.Discover[oidc.DiscoveryConfiguration](
						context.Background(),
						issuer,
						discoveryHTTPClient,
					)
					if err != nil {
						return nil, err
					}
					keySets[issuer] = client.NewRemoteKeySet(httpClient, discovery.JwksURI)
				}

				return NewJWTAuth(
					keySets,
					cfg.Service,
					cfg.CheckScopes,
				), nil
			}),
		)
	} else {
		options = append(options,
			fx.Provide(func() Authenticator {
				return NewNoAuth()
			}),
		)
	}

	return fx.Module("auth", options...)
}
