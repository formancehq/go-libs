package client

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"

	"github.com/formancehq/go-libs/v4/oidc"
	httphelper "github.com/formancehq/go-libs/v4/oidc/http"
)

const (
	idTokenKey = "id_token"
)

// RelyingParty declares the minimal interface for oidc clients
type RelyingParty interface {
	// OAuthConfig returns the oauth2 Config
	OAuthConfig() *oauth2.Config

	// Issuer returns the issuer of the oidc config
	Issuer() string

	// IsPKCE returns if authorization is done using `Authorization Code Flow with Proof Key for Code Exchange (PKCE)`
	IsPKCE() bool

	// CookieHandler returns a http cookie handler used for various state transfer cookies
	CookieHandler() *httphelper.CookieHandler

	// HttpClient returns a http client used for calls to the openid provider, e.g. calling token endpoint
	HttpClient() *http.Client

	// IsOAuth2Only specifies whether relaying party handles only oauth2 or oidc calls
	IsOAuth2Only() bool

	// Signer is used if the relaying party uses the JWT Profile
	Signer() jose.Signer

	// GetEndSessionEndpoint returns the endpoint to sign out on a IDP
	GetEndSessionEndpoint() string

	// GetRevokeEndpoint returns the endpoint to revoke a specific token
	GetRevokeEndpoint() string

	// UserinfoEndpoint returns the userinfo
	UserinfoEndpoint() string

	// GetDeviceAuthorizationEndpoint returns the endpoint which can
	// be used to start a DeviceAuthorization flow.
	GetDeviceAuthorizationEndpoint() string

	// GetIntrospectionEndpoint returns the endpoint to introspect a specific token
	GetIntrospectionEndpoint() string

	// IDTokenVerifier returns the verifier used for oidc id_token verification
	IDTokenVerifier() *Verifier

	// ErrorHandler returns the handler used for callback errors
	ErrorHandler() func(http.ResponseWriter, *http.Request, string, string, string)
}
type ErrorHandler func(w http.ResponseWriter, r *http.Request, errorType string, errorDesc string, state string)
type UnauthorizedHandler func(w http.ResponseWriter, r *http.Request, desc string, state string)

var DefaultErrorHandler ErrorHandler = func(w http.ResponseWriter, r *http.Request, errorType string, errorDesc string, state string) {
	http.Error(w, errorType+": "+errorDesc, http.StatusInternalServerError)
}
var DefaultUnauthorizedHandler UnauthorizedHandler = func(w http.ResponseWriter, r *http.Request, desc string, state string) {
	http.Error(w, desc, http.StatusUnauthorized)
}

type relyingParty struct {
	issuer                      string
	DiscoveryEndpoint           string
	endpoints                   Endpoints
	oauthConfig                 *oauth2.Config
	oauth2Only                  bool
	pkce                        bool
	useSigningAlgsFromDiscovery bool

	httpClient    *http.Client
	cookieHandler *httphelper.CookieHandler

	oauthAuthStyle oauth2.AuthStyle

	errorHandler        func(http.ResponseWriter, *http.Request, string, string, string)
	unauthorizedHandler func(http.ResponseWriter, *http.Request, string, string)
	idTokenVerifier     *Verifier
	verifierOpts        []VerifierOption
	signer              jose.Signer
}

func (rp *relyingParty) OAuthConfig() *oauth2.Config {
	return rp.oauthConfig
}

func (rp *relyingParty) Issuer() string {
	return rp.issuer
}

func (rp *relyingParty) IsPKCE() bool {
	return rp.pkce
}

func (rp *relyingParty) CookieHandler() *httphelper.CookieHandler {
	return rp.cookieHandler
}

func (rp *relyingParty) HttpClient() *http.Client {
	return rp.httpClient
}

func (rp *relyingParty) IsOAuth2Only() bool {
	return rp.oauth2Only
}

func (rp *relyingParty) Signer() jose.Signer {
	return rp.signer
}

func (rp *relyingParty) UserinfoEndpoint() string {
	return rp.endpoints.UserinfoURL
}

func (rp *relyingParty) GetDeviceAuthorizationEndpoint() string {
	return rp.endpoints.DeviceAuthorizationURL
}

func (rp *relyingParty) GetEndSessionEndpoint() string {
	return rp.endpoints.EndSessionURL
}

func (rp *relyingParty) GetRevokeEndpoint() string {
	return rp.endpoints.RevokeURL
}

func (rp *relyingParty) GetIntrospectionEndpoint() string {
	return rp.endpoints.IntrospectURL
}

func (rp *relyingParty) IDTokenVerifier() *Verifier {
	if rp.idTokenVerifier == nil {
		rp.idTokenVerifier = NewIDTokenVerifier(rp.oauthConfig.ClientID, NewRemoteKeySet(rp.httpClient, rp.endpoints.JKWsURL), append(
			rp.verifierOpts,
			WithIssuer(func(v string) bool {
				return v == rp.issuer
			}),
		)...)
	}
	return rp.idTokenVerifier
}

func (rp *relyingParty) ErrorHandler() func(http.ResponseWriter, *http.Request, string, string, string) {
	if rp.errorHandler == nil {
		rp.errorHandler = DefaultErrorHandler
	}
	return rp.errorHandler
}

func (rp *relyingParty) UnauthorizedHandler() func(http.ResponseWriter, *http.Request, string, string) {
	if rp.unauthorizedHandler == nil {
		rp.unauthorizedHandler = DefaultUnauthorizedHandler
	}
	return rp.unauthorizedHandler
}

// NewRelyingPartyOIDC creates an (OIDC) RelyingParty with the given
// issuer, clientID, clientSecret, redirectURI, scopes and possible configOptions
// it will run discovery on the provided issuer and use the found endpoints
func NewRelyingPartyOIDC(ctx context.Context, issuer, clientID, clientSecret, redirectURI string, scopes []string, options ...Option) (RelyingParty, error) {
	rp := &relyingParty{
		issuer: issuer,
		oauthConfig: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURI,
			Scopes:       scopes,
		},
		httpClient:     httphelper.DefaultHTTPClient,
		oauth2Only:     false,
		oauthAuthStyle: oauth2.AuthStyleAutoDetect,
	}

	for _, optFunc := range options {
		if err := optFunc(rp); err != nil {
			return nil, err
		}
	}
	discoveryConfiguration, err := Discover[oidc.DiscoveryConfiguration](ctx, rp.issuer, rp.httpClient, rp.DiscoveryEndpoint)
	if err != nil {
		return nil, err
	}
	if rp.useSigningAlgsFromDiscovery {
		rp.verifierOpts = append(rp.verifierOpts, WithSupportedSigningAlgorithms(discoveryConfiguration.IDTokenSigningAlgValuesSupported...))
	}
	endpoints := GetEndpoints(discoveryConfiguration)
	rp.oauthConfig.Endpoint = endpoints.Endpoint
	rp.endpoints = endpoints

	rp.oauthConfig.Endpoint.AuthStyle = rp.oauthAuthStyle
	rp.endpoints.AuthStyle = rp.oauthAuthStyle

	// avoid races by calling these early
	_ = rp.IDTokenVerifier()     // sets idTokenVerifier
	_ = rp.ErrorHandler()        // sets errorHandler
	_ = rp.UnauthorizedHandler() // sets unauthorizedHandler

	return rp, nil
}

// Option is the type for providing dynamic options to the relyingParty
type Option func(*relyingParty) error

// WithHTTPClient provides the ability to set an http client to be used for the relaying party and verifier
func WithHTTPClient(client *http.Client) Option {
	return func(rp *relyingParty) error {
		rp.httpClient = client
		return nil
	}
}

func WithAuthStyle(oauthAuthStyle oauth2.AuthStyle) Option {
	return func(rp *relyingParty) error {
		rp.oauthAuthStyle = oauthAuthStyle
		return nil
	}
}

type SignerFromKey func() (jose.Signer, error)

type AuthURLOpt func() []oauth2.AuthCodeOption

// AuthURL returns the auth request url
// (wrapping the oauth2 `AuthCodeURL`)
func AuthURL(state string, rp RelyingParty, opts ...AuthURLOpt) string {
	authOpts := make([]oauth2.AuthCodeOption, 0)
	for _, opt := range opts {
		authOpts = append(authOpts, opt()...)
	}
	return rp.OAuthConfig().AuthCodeURL(state, authOpts...)
}

// ErrMissingIDToken is returned when an id_token was expected,
// but not received in the token response.
var ErrMissingIDToken = errors.New("id_token missing")

func verifyTokenResponse[C oidc.IDClaims](ctx context.Context, token *oauth2.Token, rp RelyingParty) (*oidc.Tokens[C], error) {
	if rp.IsOAuth2Only() {
		return &oidc.Tokens[C]{Token: token}, nil
	}
	idTokenString, ok := token.Extra(idTokenKey).(string)
	if !ok {
		return &oidc.Tokens[C]{Token: token}, ErrMissingIDToken
	}
	idToken, err := VerifyTokens[C](ctx, token.AccessToken, idTokenString, rp.IDTokenVerifier())
	if err != nil {
		return nil, err
	}
	return &oidc.Tokens[C]{Token: token, IDTokenClaims: idToken, IDToken: idTokenString}, nil
}

type CodeExchangeCallback[C oidc.IDClaims] func(w http.ResponseWriter, r *http.Request, tokens *oidc.Tokens[C], state string, rp RelyingParty)

type SubjectGetter interface {
	GetSubject() string
}

type CodeExchangeUserinfoCallback[C oidc.IDClaims, U SubjectGetter] func(w http.ResponseWriter, r *http.Request, tokens *oidc.Tokens[C], state string, provider RelyingParty, info U)

type OptionFunc func(RelyingParty)

type Endpoints struct {
	oauth2.Endpoint
	IntrospectURL          string
	UserinfoURL            string
	JKWsURL                string
	EndSessionURL          string
	RevokeURL              string
	DeviceAuthorizationURL string
}

func GetEndpoints(discoveryConfig *oidc.DiscoveryConfiguration) Endpoints {
	return Endpoints{
		Endpoint: oauth2.Endpoint{
			AuthURL:  discoveryConfig.AuthorizationEndpoint,
			TokenURL: discoveryConfig.TokenEndpoint,
		},
		IntrospectURL:          discoveryConfig.IntrospectionEndpoint,
		UserinfoURL:            discoveryConfig.UserinfoEndpoint,
		JKWsURL:                discoveryConfig.JwksURI,
		EndSessionURL:          discoveryConfig.EndSessionEndpoint,
		RevokeURL:              discoveryConfig.RevocationEndpoint,
		DeviceAuthorizationURL: discoveryConfig.DeviceAuthorizationEndpoint,
	}
}

type URLParamOpt func() []oauth2.AuthCodeOption

type tokenEndpointCaller struct {
	RelyingParty
}
