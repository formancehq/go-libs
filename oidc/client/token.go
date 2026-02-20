package client

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/formancehq/go-libs/v4/oidc"
	httphelper "github.com/formancehq/go-libs/v4/oidc/http"
	"github.com/formancehq/go-libs/v4/time"
)

type TokenEndpointCaller interface {
	TokenEndpoint() string
	HttpClient() *http.Client
	IDTokenVerifier() *Verifier
}

func CallTokenEndpoint(ctx context.Context, request any, caller TokenEndpointCaller) (newToken *oauth2.Token, err error) {
	return callTokenEndpoint(ctx, request, nil, caller)
}

func callTokenEndpoint(ctx context.Context, request any, authFn any, caller TokenEndpointCaller) (newToken *oauth2.Token, err error) {

	req, err := httphelper.FormRequest(ctx, caller.TokenEndpoint(), request, Encoder, authFn)
	if err != nil {
		return nil, err
	}

	client := caller.HttpClient()
	if client == nil {
		client = httphelper.DefaultHTTPClient
	}

	tokenRes := new(oidc.AccessTokenResponse)
	if err := httphelper.HttpRequest(client, req, &tokenRes); err != nil {
		return nil, err
	}
	token := &oauth2.Token{
		AccessToken:  tokenRes.AccessToken,
		TokenType:    tokenRes.TokenType,
		RefreshToken: tokenRes.RefreshToken,
		Expiry:       time.Now().UTC().Add(time.Duration(tokenRes.ExpiresIn) * time.Second).Time,
	}
	if tokenRes.IDToken != "" {
		token = token.WithExtra(map[string]any{
			"id_token": tokenRes.IDToken,
		})
	}
	return token, nil
}
