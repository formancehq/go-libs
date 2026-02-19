package client

import (
	"context"
	"errors"

	"github.com/formancehq/go-libs/v4/oidc"
)

func (t tokenEndpointCaller) TokenEndpoint() string {
	return t.OAuthConfig().Endpoint.TokenURL
}

type RefreshTokenRequest struct {
	RefreshToken        string                   `schema:"refresh_token"`
	Scopes              oidc.SpaceDelimitedArray `schema:"scope,omitempty"`
	ClientID            string                   `schema:"client_id,omitempty"`
	ClientSecret        string                   `schema:"client_secret,omitempty"`
	ClientAssertion     string                   `schema:"client_assertion,omitempty"`
	ClientAssertionType string                   `schema:"client_assertion_type,omitempty"`
	GrantType           oidc.GrantType           `schema:"grant_type"`
}

// RefreshTokens performs a token refresh. If it doesn't error, it will always
// provide a new AccessToken. It may provide a new RefreshToken, and if it does, then
// the old one should be considered invalid.
//
// In case the RP is not OAuth2 only and an IDToken was part of the response,
// the IDToken and AccessToken will be verified
// and the IDToken and IDTokenClaims fields will be populated in the returned object.
func RefreshTokens[C oidc.IDClaims](ctx context.Context, rp RelyingParty, refreshToken, clientAssertion, clientAssertionType string) (*oidc.Tokens[C], error) {
	request := RefreshTokenRequest{
		RefreshToken:        refreshToken,
		ClientID:            rp.OAuthConfig().ClientID,
		ClientSecret:        rp.OAuthConfig().ClientSecret,
		ClientAssertion:     clientAssertion,
		ClientAssertionType: clientAssertionType,
		GrantType:           oidc.GrantTypeRefreshToken,
	}
	newToken, err := CallTokenEndpoint(ctx, request, tokenEndpointCaller{RelyingParty: rp})
	if err != nil {
		return nil, err
	}
	tokens, err := verifyTokenResponse[C](ctx, newToken, rp)
	if err == nil || errors.Is(err, ErrMissingIDToken) {
		// https://openid.net/specs/openid-connect-core-1_0.html#RefreshTokenResponse
		// ...except that it might not contain an id_token.
		return tokens, nil
	}
	return nil, err
}
