package client

import (
	"context"
	"errors"

	"golang.org/x/oauth2"

	"github.com/formancehq/go-libs/v4/oidc"
	httphelper "github.com/formancehq/go-libs/v4/oidc/http"
	"github.com/formancehq/go-libs/v4/time"
)

// TokenExchange performs a token exchange as defined in RFC 8693.
// It exchanges a subject token for a new access token with potentially different scopes or audience.
//
// Token Exchange is a pure OAuth2 grant type and does not return ID tokens.
// As there is no user authentication associated to the request, an ID Token cannot be returned.
//
// The function accepts:
//   - subjectToken: The token to be exchanged (required)
//   - subjectTokenType: The type of the subject token (e.g., "urn:ietf:params:oauth:token-type:access_token")
//   - requestedScopes: Optional scopes to request for the new token
//   - requestedTokenType: Optional token type to request (defaults to access_token)
//   - actorToken: Optional actor token for delegation scenarios
//   - actorTokenType: Optional actor token type
//   - resource: Optional target resource URI
//   - audience: Optional intended audience
//
// Returns oauth2.Token with the new access token.
func TokenExchange(
	ctx context.Context,
	rp RelyingParty,
	subjectToken string,
	subjectTokenType oidc.TokenType,
	opts ...TokenExchangeOpt,
) (*oauth2.Token, error) {
	request := &oidc.TokenExchangeRequest{
		GrantType:        oidc.GrantTypeTokenExchange,
		SubjectToken:     subjectToken,
		SubjectTokenType: subjectTokenType,
		ClientID:         rp.OAuthConfig().ClientID,
		ClientSecret:     rp.OAuthConfig().ClientSecret,
	}

	// Apply options
	for _, opt := range opts {
		opt(request)
	}

	// Set default requested_token_type if not provided
	if request.RequestedTokenType == "" {
		request.RequestedTokenType = oidc.AccessTokenType
	}

	// Call token endpoint with TokenExchangeResponse
	resp, err := callTokenExchangeEndpoint(ctx, request, tokenEndpointCaller{RelyingParty: rp})
	if err != nil {
		return nil, err
	}

	// Convert TokenExchangeResponse to oauth2.Token
	token := &oauth2.Token{
		AccessToken: resp.AccessToken,
		TokenType:   resp.TokenType,
		Expiry:      time.Now().UTC().Add(time.Duration(resp.ExpiresIn) * time.Second).Time,
	}

	if resp.RefreshToken != "" {
		token.RefreshToken = resp.RefreshToken
	}

	return token, nil
}

// TokenExchangeOpt is a function type for configuring TokenExchangeRequest options
type TokenExchangeOpt func(*oidc.TokenExchangeRequest)

// WithRequestedScopes sets the requested scopes for the token exchange
func WithRequestedScopes(scopes []string) TokenExchangeOpt {
	return func(req *oidc.TokenExchangeRequest) {
		req.Scopes = scopes
	}
}

// WithRequestedTokenType sets the requested token type
func WithRequestedTokenType(tokenType oidc.TokenType) TokenExchangeOpt {
	return func(req *oidc.TokenExchangeRequest) {
		req.RequestedTokenType = tokenType
	}
}

// WithActorToken sets the actor token and its type for delegation scenarios
func WithActorToken(actorToken string, actorTokenType oidc.TokenType) TokenExchangeOpt {
	return func(req *oidc.TokenExchangeRequest) {
		req.ActorToken = actorToken
		req.ActorTokenType = actorTokenType
	}
}

// WithResource sets the target resource URI
func WithResource(resource string) TokenExchangeOpt {
	return func(req *oidc.TokenExchangeRequest) {
		req.Resource = resource
	}
}

// WithAudience sets the intended audience for the new token
func WithAudience(audience string) TokenExchangeOpt {
	return func(req *oidc.TokenExchangeRequest) {
		req.Audience = audience
	}
}

// callTokenExchangeEndpoint calls the token endpoint with a TokenExchangeRequest
// and returns a TokenExchangeResponse
func callTokenExchangeEndpoint(ctx context.Context, request *oidc.TokenExchangeRequest, caller TokenEndpointCaller) (*oidc.TokenExchangeResponse, error) {
	req, err := httphelper.FormRequest(ctx, caller.TokenEndpoint(), request, Encoder, nil)
	if err != nil {
		return nil, err
	}

	// Set Basic Auth if client credentials are provided
	if request.ClientSecret != "" {
		req.SetBasicAuth(request.ClientID, request.ClientSecret)
	}

	client := caller.HttpClient()
	if client == nil {
		client = httphelper.DefaultHTTPClient
	}

	resp := new(oidc.TokenExchangeResponse)
	if err := httphelper.HttpRequest(client, req, &resp); err != nil {
		return nil, err
	}

	// Validate that we got a response
	if resp.AccessToken == "" {
		return nil, errors.New("token exchange response missing access_token")
	}

	return resp, nil
}
