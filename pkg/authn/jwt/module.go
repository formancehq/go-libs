package jwt

import (
	"context"
	"errors"
	"net/http"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
	"github.com/formancehq/go-libs/v5/pkg/authn/oidc/client"
)

type Config struct {
	Enabled              bool
	Issuers              []string
	ReadKeySetMaxRetries int
	CheckScopes          bool
	Service              string

	// Deprecated: use Issuers instead.
	Issuer string

	AdditionalChecks []AdditionalCheck
}

func (cfg Config) resolveIssuers() []string {
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

func NewKeySets(cfg Config, httpClient *http.Client) (map[string]oidc.KeySet, error) {
	issuers := cfg.resolveIssuers()

	if !cfg.Enabled {
		return make(map[string]oidc.KeySet), nil
	}

	if len(issuers) == 0 {
		return nil, errors.New("auth is enabled but no issuers are configured")
	}

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

	return keySets, nil
}

func NewAuthenticatorFromConfig(cfg Config, keySets map[string]oidc.KeySet) Authenticator {
	if !cfg.Enabled {
		return NewNoAuth()
	}

	return NewJWTAuth(
		keySets,
		cfg.Service,
		cfg.CheckScopes,
		cfg.AdditionalChecks,
	)
}
