package client

import (
	"context"
	"errors"
	"net/http"

	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
	httphelper "github.com/formancehq/go-libs/v5/pkg/authn/oidc/http"
)

// Discover calls the discovery endpoint of the provided issuer and returns its configuration
// It accepts an optional argument "wellknownUrl" which can be used to override the discovery endpoint url
func Discover[V any](ctx context.Context, issuer string, httpClient *http.Client, wellKnownUrl ...string) (*V, error) {

	wellKnown := oidc.NormalizeIssuer(issuer) + oidc.DiscoveryEndpoint
	if len(wellKnownUrl) == 1 && wellKnownUrl[0] != "" {
		wellKnown = wellKnownUrl[0]
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return nil, err
	}
	discoveryConfig := new(V)
	err = httphelper.HttpRequest(httpClient, req, &discoveryConfig)
	if err != nil {
		return nil, errors.Join(oidc.ErrDiscoveryFailed, err)
	}
	if err := validateDiscoveredIssuer(issuer, discoveryConfig); err != nil {
		return nil, errors.Join(oidc.ErrDiscoveryFailed, err)
	}

	return discoveryConfig, nil
}

func validateDiscoveredIssuer[V any](issuer string, discoveryConfig *V) error {
	issuerConfig, ok := any(discoveryConfig).(oidc.IssuerGetter)
	if !ok {
		return nil
	}
	return oidc.CheckDiscoveredIssuer(issuer, issuerConfig)
}
