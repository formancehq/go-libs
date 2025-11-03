package client

import (
	"context"

	"github.com/formancehq/go-libs/v3/time"

	"github.com/go-jose/go-jose/v4"

	"github.com/formancehq/go-libs/v3/oidc"
)

// VerifyTokens implement the Token Response Validation as defined in OIDC specification
// https://openid.net/specs/openid-connect-core-1_0.html#TokenResponseValidation
func VerifyTokens[C oidc.IDClaims](ctx context.Context, accessToken, idToken string, v *Verifier) (claims C, err error) {
	var nilClaims C

	claims, sigAlgorithm, err := VerifyIDToken[C](ctx, idToken, v)
	if err != nil {
		return nilClaims, err
	}
	if err := VerifyAccessToken(accessToken, claims.GetAccessTokenHash(), sigAlgorithm); err != nil {
		return nilClaims, err
	}

	return claims, nil
}

// VerifyIDToken validates the id token according to
// https://openid.net/specs/openid-connect-core-1_0.html#IDTokenValidation
func VerifyIDToken[C oidc.Claims](ctx context.Context, token string, v *Verifier) (claims C, algorithm jose.SignatureAlgorithm, err error) {

	var nilClaims C

	decrypted, err := oidc.DecryptToken(token)
	if err != nil {
		return nilClaims, "", err
	}
	payload, err := oidc.ParseToken(decrypted, &claims)
	if err != nil {
		return nilClaims, "", err
	}

	if err := oidc.CheckSubject(claims); err != nil {
		return nilClaims, "", err
	}

	if v.Issuer != nil {
		if !v.Issuer(claims.GetIssuer()) {
			return nilClaims, "", oidc.ErrIssuerInvalid
		}
	}

	if err = oidc.CheckAudience(claims, v.ClientID); err != nil {
		return nilClaims, "", err
	}

	if err = oidc.CheckAuthorizedParty(claims, v.ClientID); err != nil {
		return nilClaims, "", err
	}

	sigAlgorithm, err := oidc.CheckSignature(ctx, decrypted, payload, v.SupportedSignAlgs, v.KeySet)
	if err != nil {
		return nilClaims, "", err
	}

	if err = oidc.CheckExpiration(claims, v.Offset); err != nil {
		return nilClaims, "", err
	}

	if err = oidc.CheckIssuedAt(claims, v.MaxAgeIAT, v.Offset); err != nil {
		return nilClaims, "", err
	}

	if v.Nonce != nil {
		if err = oidc.CheckNonce(claims, v.Nonce(ctx)); err != nil {
			return nilClaims, "", err
		}
	}

	if err = oidc.CheckAuthorizationContextClassReference(claims, v.ACR); err != nil {
		return nilClaims, "", err
	}

	if err = oidc.CheckAuthTime(claims, v.MaxAge); err != nil {
		return nilClaims, "", err
	}
	return claims, sigAlgorithm, nil
}

// VerifyAccessToken validates the access token according to
// https://openid.net/specs/openid-connect-core-1_0.html#CodeFlowTokenValidation
func VerifyAccessToken(accessToken, atHash string, sigAlgorithm jose.SignatureAlgorithm) error {
	if atHash == "" {
		return nil
	}

	actual, err := oidc.ClaimHash(accessToken, sigAlgorithm)
	if err != nil {
		return err
	}
	if actual != atHash {
		return oidc.ErrAtHash
	}
	return nil
}

// NewIDTokenVerifier returns a oidc.Verifier suitable for ID token verification.
func NewIDTokenVerifier(clientID string, keySet oidc.KeySet, options ...VerifierOption) *Verifier {
	v := &Verifier{
		ClientID: clientID,
		KeySet:   keySet,
		Offset:   time.Second,
	}

	for _, opts := range append(defaultOptions, options...) {
		opts(v)
	}

	return v
}

type Verifier struct {
	Issuer            func(string) bool
	MaxAgeIAT         time.Duration
	Offset            time.Duration
	ClientID          string
	SupportedSignAlgs []string
	MaxAge            time.Duration
	ACR               oidc.ACRVerifier
	KeySet            oidc.KeySet
	Nonce             func(ctx context.Context) string
}

var defaultOptions = []VerifierOption{
	WithOffset(time.Second),
	WithNonce(func(_ context.Context) string {
		return ""
	}),
}

type VerifierOption func(*Verifier)

func WithOffset(offset time.Duration) VerifierOption {
	return func(v *Verifier) {
		v.Offset = offset
	}
}

func WithNonce(nonce func(ctx context.Context) string) VerifierOption {
	return func(v *Verifier) {
		v.Nonce = nonce
	}
}

func WithIssuer(issuer func(string) bool) VerifierOption {
	return func(v *Verifier) {
		v.Issuer = issuer
	}
}

// WithIssuedAtOffset mitigates the risk of iat to be in the future
// because of clock skews with the ability to add an offset to the current time
func WithIssuedAtOffset(offset time.Duration) VerifierOption {
	return func(v *Verifier) {
		v.Offset = offset
	}
}

// WithIssuedAtMaxAge provides the ability to define the maximum duration between iat and now
func WithIssuedAtMaxAge(maxAge time.Duration) VerifierOption {
	return func(v *Verifier) {
		v.MaxAgeIAT = maxAge
	}
}

// WithACRVerifier sets the verifier for the acr claim
func WithACRVerifier(verifier oidc.ACRVerifier) VerifierOption {
	return func(v *Verifier) {
		v.ACR = verifier
	}
}

// WithAuthTimeMaxAge provides the ability to define the maximum duration between auth_time and now
func WithAuthTimeMaxAge(maxAge time.Duration) VerifierOption {
	return func(v *Verifier) {
		v.MaxAge = maxAge
	}
}

// WithSupportedSigningAlgorithms overwrites the default RS256 signing algorithm
func WithSupportedSigningAlgorithms(algs ...string) VerifierOption {
	return func(v *Verifier) {
		v.SupportedSignAlgs = algs
	}
}
