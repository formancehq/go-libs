package client

import (
	"context"

	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
	httphelper "github.com/formancehq/go-libs/v5/pkg/authn/oidc/http"
)

func Introspect(ctx context.Context, relyingParty RelyingParty, token string) (*oidc.IntrospectionResponse, error) {

	req, err := httphelper.FormRequest(
		ctx,
		relyingParty.GetIntrospectionEndpoint(),
		&oidc.IntrospectionRequest{
			Token: token,
		},
		Encoder,
		httphelper.AuthorizeBasic(relyingParty.OAuthConfig().ClientID, relyingParty.OAuthConfig().ClientSecret),
	)
	if err != nil {
		return nil, err
	}

	resp := &oidc.IntrospectionResponse{}
	if err := httphelper.HttpRequest(relyingParty.HttpClient(), req, &resp); err != nil {
		return resp, err
	}

	return resp, nil
}
