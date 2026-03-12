package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/hashicorp/go-retryablehttp"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v4/oidc"
	"github.com/formancehq/go-libs/v4/oidc/client"
)

type ModuleConfig struct {
	Enabled              bool
	Issuers              []string
	ReadKeySetMaxRetries int
	CheckScopes          bool
	Service              string

	// Deprecated: use Issuers instead.
	Issuer string

	AdditionalChecks []AdditionalCheck
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
	options := ModuleOptions("")
	options = append(options, fx.Provide(func() ModuleConfig {
		return cfg
	}))
	return fx.Module("auth", options...)
}

// Annotated module appends a name tag to all the deps supplied in this module so that
// generic types like http.Client won't conflict with other modules
func AnnotatedModule(cfg ModuleConfig, annotationTag string) fx.Option {
	nameAnnotation := `name:"` + annotationTag + `"`
	options := ModuleOptions(nameAnnotation)
	options = append(options, fx.Provide(fx.Annotate(func() ModuleConfig {
		return cfg
	}, fx.ResultTags(nameAnnotation))))
	return fx.Module("auth", options...)
}

func ModuleOptions(nameAnnotation string) []fx.Option {
	options := make([]fx.Option, 0)
	if nameAnnotation == "" {
		options = append(options,
			fx.Supply(http.DefaultClient),
			fx.Provide(newKeySets),
			fx.Provide(newAuthenticator),
		)
		return options
	}

	options = append(options, fx.Provide(
		fx.Annotate(func() *http.Client {
			return http.DefaultClient
		}, fx.ResultTags(nameAnnotation)),
	))
	options = append(options, fx.Provide(
		fx.Annotate(newKeySets, fx.ParamTags(nameAnnotation, nameAnnotation), fx.ResultTags(nameAnnotation, ``)),
	))
	options = append(options, fx.Provide(
		fx.Annotate(newAuthenticator, fx.ParamTags(nameAnnotation, nameAnnotation), fx.ResultTags(nameAnnotation)),
	))
	return options
}

func newKeySets(cfg ModuleConfig, httpClient *http.Client) (map[string]oidc.KeySet, error) {
	issuers := cfg.resolveIssuers()

	if !cfg.Enabled {
		// this won't be used by the NoAuth
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

func newAuthenticator(cfg ModuleConfig, keySets map[string]oidc.KeySet) Authenticator {
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
