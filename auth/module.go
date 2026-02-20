package auth

import (
	"context"
	"net/http"

	"github.com/hashicorp/go-retryablehttp"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v4/oidc"
	"github.com/formancehq/go-libs/v4/oidc/client"
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
			fx.Provide(newKeySet),
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
		fx.Annotate(newKeySet, fx.ParamTags(nameAnnotation, nameAnnotation), fx.ResultTags(nameAnnotation, ``)),
	))
	options = append(options, fx.Provide(
		fx.Annotate(newAuthenticator, fx.ParamTags(nameAnnotation, nameAnnotation), fx.ResultTags(nameAnnotation)),
	))
	return options
}

func newKeySet(cfg ModuleConfig, httpClient *http.Client) (oidc.KeySet, error) {
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
}

func newAuthenticator(cfg ModuleConfig, keySet oidc.KeySet) Authenticator {
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
