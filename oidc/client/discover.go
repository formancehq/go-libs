package client

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/formancehq/go-libs/v3/oidc"
	httphelper "github.com/zitadel/oidc/v3/pkg/http"
)

// Discover calls the discovery endpoint of the provided issuer and returns its configuration
// It accepts an optional argument "wellknownUrl" which can be used to overide the dicovery endpoint url
func Discover[V any](ctx context.Context, issuer string, httpClient *http.Client, wellKnownUrl ...string) (*V, error) {

	wellKnown := strings.TrimSuffix(issuer, "/") + oidc.DiscoveryEndpoint
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

	return discoveryConfig, nil
}
