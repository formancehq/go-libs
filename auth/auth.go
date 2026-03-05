package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/formancehq/go-libs/v4/oidc"
)

type JWTAuth struct {
	keySets          map[string]oidc.KeySet // issuer -> keySet
	checkScopes      bool
	service          string
	additionalChecks []AdditionalCheck
}

func NewJWTAuth(
	keySets map[string]oidc.KeySet,
	service string,
	checkScopes bool,
	additionalChecks []AdditionalCheck,
) *JWTAuth {
	return &JWTAuth{
		keySets:          keySets,
		checkScopes:      checkScopes,
		service:          service,
		additionalChecks: additionalChecks,
	}
}

func (ja *JWTAuth) authenticate(r *http.Request) (ControlPlaneAgent, error) {
	claims, err := ClaimsFromRequest(r, ja.keySets)
	if err != nil {
		return nil, err
	}

	// DefaultControlPlaneAgent provides access to claims that are expected to be present when authenticating via the Control Plane
	// in the case of another issuer (eg. Stack authentication) some of these claims may not be present
	agt := NewDefaultControlPlaneAgent(*claims)
	for _, check := range ja.additionalChecks {
		err := check(r, claims)
		if err != nil {
			return agt, err
		}
	}

	if !ja.checkScopes {
		return agt, nil
	}
	valid, err := checkScopes(ja.service, r.Method, claims.Scopes)
	if err != nil || !valid {
		return agt, fmt.Errorf("scopes not valid: %w", err)
	}
	return agt, nil
}

func (ja *JWTAuth) AuthenticateOnControlPlane(r *http.Request) (ControlPlaneAgent, error) {
	return ja.authenticate(r)
}

// Authenticate validates the JWT in the request and returns the user, if valid.
func (ja *JWTAuth) Authenticate(_ http.ResponseWriter, r *http.Request) (bool, error) {
	_, err := ja.authenticate(r)
	if err != nil {
		return false, err
	}
	return true, nil
}

var (
	ErrNoAuthorizationHeader = errors.New("no authorization header")
	ErrMalformedHeader       = errors.New("malformed authorization header")
)

func ClaimsFromRequest(r *http.Request, keySets map[string]oidc.KeySet) (*oidc.AccessTokenClaims, error) {
	authHeader := r.Header.Get("authorization")
	if authHeader == "" {
		return nil, ErrNoAuthorizationHeader
	}

	if !strings.HasPrefix(authHeader, "bearer") &&
		!strings.HasPrefix(authHeader, "Bearer") {
		return nil, ErrMalformedHeader
	}

	token := authHeader[6:]
	token = strings.TrimSpace(token)

	claims := &oidc.AccessTokenClaims{}
	decrypted, err := oidc.DecryptToken(token)
	if err != nil {
		return nil, err
	}
	payload, err := oidc.ParseToken(decrypted, claims)
	if err != nil {
		return nil, err
	}

	keySet, ok := keySets[claims.Issuer]
	if !ok {
		issuers := make([]string, 0, len(keySets))
		for iss := range keySets {
			issuers = append(issuers, iss)
		}
		return claims, fmt.Errorf(
			"%w: got: %s, trusted: %v",
			oidc.ErrIssuerInvalid,
			claims.Issuer,
			issuers,
		)
	}

	if _, err = oidc.CheckSignature(
		r.Context(),
		decrypted,
		payload,
		[]string{}, // Default to RS256
		keySet,
	); err != nil {
		return claims, err
	}

	if err := oidc.CheckExpiration(claims, 0); err != nil {
		return claims, err
	}

	return claims, nil
}
