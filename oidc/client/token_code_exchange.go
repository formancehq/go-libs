package client

import (
	"context"

	"golang.org/x/oauth2"

	"github.com/formancehq/go-libs/v3/oidc"
)

type CodeExchangeOpt func() []oauth2.AuthCodeOption

// CodeExchange handles the oauth2 code exchange, extracting and validating the id_token
// returning it parsed together with the oauth2 tokens (access, refresh)
func CodeExchange[C oidc.IDClaims](ctx context.Context, code string, rp RelyingParty, opts ...CodeExchangeOpt) (tokens *oidc.Tokens[C], err error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, rp.HttpClient())
	codeOpts := make([]oauth2.AuthCodeOption, 0)
	for _, opt := range opts {
		codeOpts = append(codeOpts, opt()...)
	}

	token, err := rp.OAuthConfig().Exchange(ctx, code, codeOpts...)
	if err != nil {
		return nil, err
	}
	return verifyTokenResponse[C](ctx, token, rp)
}
