package client

import (
	"context"
	"net/url"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// ClientCredentials requests an access token using the `client_credentials` grant,
// as defined in [RFC 6749, section 4.4].
//
// As there is no user associated to the request an ID Token can never be returned.
// Client Credentials are undefined in OpenID Connect and is a pure OAuth2 grant.
// Furthermore the server SHOULD NOT return a refresh token.
//
// [RFC 6749, section 4.4]: https://datatracker.ietf.org/doc/html/rfc6749#section-4.4
func ClientCredentials(ctx context.Context, rp RelyingParty, endpointParams url.Values) (token *oauth2.Token, err error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, rp.HttpClient())
	config := clientcredentials.Config{
		ClientID:       rp.OAuthConfig().ClientID,
		ClientSecret:   rp.OAuthConfig().ClientSecret,
		TokenURL:       rp.OAuthConfig().Endpoint.TokenURL,
		Scopes:         rp.OAuthConfig().Scopes,
		EndpointParams: endpointParams,
		AuthStyle:      rp.OAuthConfig().Endpoint.AuthStyle,
	}
	return config.Token(ctx)
}
