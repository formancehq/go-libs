package jwt

import (
	"context"
	"net/http"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
	"github.com/formancehq/go-libs/v5/pkg/authn/oidc/client"
)

type ModuleConfig struct {
	Enabled              bool
	Issuer               string
	ReadKeySetMaxRetries int
	CheckScopes          bool
	Service              string

	AdditionalChecks []AdditionalCheck
}

func NewKeySet(cfg ModuleConfig, httpClient *http.Client) (oidc.KeySet, error) {
	if !cfg.Enabled {
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
}

func NewAuthenticatorFromConfig(cfg ModuleConfig, keySet oidc.KeySet) Authenticator {
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
}
