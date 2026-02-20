package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"golang.org/x/oauth2"

	"github.com/formancehq/go-libs/v4/oidc"
	httphelper "github.com/formancehq/go-libs/v4/oidc/http"
	"github.com/formancehq/go-libs/v4/time"
)

func newDeviceClientCredentialsRequest(scopes []string, rp RelyingParty) (*oidc.ClientCredentialsRequest, error) {
	confg := rp.OAuthConfig()
	req := &oidc.ClientCredentialsRequest{
		Scope:        scopes,
		ClientID:     confg.ClientID,
		ClientSecret: confg.ClientSecret,
	}

	if signer := rp.Signer(); signer != nil {
		assertion, err := SignedJWTProfileAssertion(rp.OAuthConfig().ClientID, []string{rp.Issuer()}, time.Hour, signer)
		if err != nil {
			return nil, fmt.Errorf("failed to build assertion: %w", err)
		}
		req.ClientAssertion = assertion
		req.ClientAssertionType = oidc.ClientAssertionTypeJWTAssertion
	}

	return req, nil
}

// DeviceAuthorization starts a new Device Authorization flow as defined
// in RFC 8628, section 3.1 and 3.2:
// https://www.rfc-editor.org/rfc/rfc8628#section-3.1
func DeviceAuthorization(ctx context.Context, scopes []string, rp RelyingParty, opts ...func(values url.Values)) (*oidc.DeviceAuthorizationResponse, error) {
	req, err := newDeviceClientCredentialsRequest(scopes, rp)
	if err != nil {
		return nil, err
	}

	return callDeviceAuthorizationEndpoint(ctx, req, rp, opts...)
}

// DeviceAccessToken attempts to obtain tokens from a Device Authorization,
// by means of polling as defined in RFC, section 3.3 and 3.4:
// https://www.rfc-editor.org/rfc/rfc8628#section-3.4
func DeviceAccessToken[C oidc.IDClaims](ctx context.Context, deviceCode string, interval time.Duration, rp RelyingParty, opts ...func(values url.Values)) (resp *oidc.Tokens[C], err error) {
	return PollDeviceAccessTokenEndpoint[C](ctx, interval, &DeviceAccessTokenRequest{
		DeviceAccessTokenRequest: oidc.DeviceAccessTokenRequest{
			GrantType:    oidc.GrantTypeDeviceCode,
			DeviceCode:   deviceCode,
			ClientID:     rp.OAuthConfig().ClientID,
			ClientSecret: rp.OAuthConfig().ClientSecret,
		},
	}, tokenEndpointCaller{rp}, opts...)
}

type DeviceAuthorizationCaller interface {
	GetDeviceAuthorizationEndpoint() string
	HttpClient() *http.Client
}

func callDeviceAuthorizationEndpoint(ctx context.Context, request *oidc.ClientCredentialsRequest, caller DeviceAuthorizationCaller, opts ...func(values url.Values)) (*oidc.DeviceAuthorizationResponse, error) {

	endpoint := caller.GetDeviceAuthorizationEndpoint()
	if endpoint == "" {
		return nil, fmt.Errorf("device authorization %w", ErrEndpointNotSet)
	}

	req, err := httphelper.FormRequest(
		ctx,
		endpoint,
		request,
		Encoder,
		httphelper.FormAuthorization(func(values url.Values) { // Abuse of FormAuthorization to add extra parameters
			for _, opt := range opts {
				opt(values)
			}
		}),
	)
	if err != nil {
		return nil, err
	}

	if request.ClientSecret != "" {
		req.SetBasicAuth(request.ClientID, request.ClientSecret)
	}

	resp := new(oidc.DeviceAuthorizationResponse)
	if err := httphelper.HttpRequest(caller.HttpClient(), req, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

type DeviceAccessTokenRequest struct {
	oidc.DeviceAccessTokenRequest
}

func CallDeviceAccessTokenEndpoint(ctx context.Context, request *DeviceAccessTokenRequest, caller TokenEndpointCaller, opts ...func(values url.Values)) (*oidc.AccessTokenResponse, error) {
	req, err := httphelper.FormRequest(ctx, caller.TokenEndpoint(), request, Encoder, nil)
	if err != nil {
		return nil, err
	}
	if request.ClientSecret != "" {
		req.SetBasicAuth(request.ClientID, request.ClientSecret)
	}
	values := url.Values{}
	for _, opt := range opts {
		opt(values)
	}
	req.URL.RawQuery = values.Encode()

	resp := new(oidc.AccessTokenResponse)
	if err := httphelper.HttpRequest(caller.HttpClient(), req, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func PollDeviceAccessTokenEndpoint[C oidc.IDClaims](ctx context.Context, interval time.Duration, request *DeviceAccessTokenRequest, caller TokenEndpointCaller, opts ...func(values url.Values)) (*oidc.Tokens[C], error) {

	for {
		timer := time.After(interval)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer:
		}

		tokens, err := func() (*oidc.Tokens[C], error) {
			ctx, cancel := context.WithTimeout(ctx, interval)
			defer cancel()

			resp, err := CallDeviceAccessTokenEndpoint(ctx, request, caller, opts...)
			if err != nil {
				return nil, err
			}

			var idTokenClaims C
			if resp.IDToken != "" {
				idTokenClaims, err = VerifyTokens[C](ctx, resp.AccessToken, resp.IDToken, caller.IDTokenVerifier())
				if err != nil {
					return nil, err
				}
			}

			return &oidc.Tokens[C]{
				Token: &oauth2.Token{
					AccessToken:  resp.AccessToken,
					TokenType:    resp.TokenType,
					RefreshToken: resp.RefreshToken,
					Expiry:       time.Now().UTC().Add(time.Duration(resp.ExpiresIn) * time.Second).Time,
					ExpiresIn:    int64(resp.ExpiresIn),
				},
				IDTokenClaims: idTokenClaims,
				IDToken:       resp.IDToken,
			}, nil
		}()
		if err == nil {
			return tokens, nil
		}
		if errors.Is(err, context.DeadlineExceeded) {
			interval += 5 * time.Second
			continue
		}
		var target *oidc.Error
		if !errors.As(err, &target) {
			return nil, err
		}
		switch target.ErrorType {
		case oidc.AuthorizationPending:
			continue
		case oidc.SlowDown:
			interval += 5 * time.Second
			continue
		default:
			return nil, err
		}
	}
}
