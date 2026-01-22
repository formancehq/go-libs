package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/formancehq/go-libs/v3/oidc"
)

type JWTAuth struct {
	issuer           string
	checkScopes      bool
	service          string
	keySet           oidc.KeySet
	additionalChecks []AdditionalCheck
}

func NewJWTAuth(
	keySet oidc.KeySet,
	issuer string,
	service string,
	checkScopes bool,
	additionalChecks []AdditionalCheck,
) *JWTAuth {
	return &JWTAuth{
		issuer:           issuer,
		checkScopes:      checkScopes,
		service:          service,
		keySet:           keySet,
		additionalChecks: additionalChecks,
	}
}

func (ja *JWTAuth) AuthenticateWithAgent(r *http.Request) (Agent, error) {
	claims, err := ClaimsFromRequest(r, ja.issuer, ja.keySet)
	if err != nil {
		return nil, err
	}

	agt := NewDefaultAgent(*claims)
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

// Authenticate validates the JWT in the request and returns the user, if valid.
func (ja *JWTAuth) Authenticate(_ http.ResponseWriter, r *http.Request) (bool, error) {
	_, err := ja.AuthenticateWithAgent(r)
	if err != nil {
		return false, err
	}
	return true, nil
}

var (
	ErrNoAuthorizationHeader = errors.New("no authorization header")
	ErrMalformedHeader       = errors.New("malformed authorization header")
)

func ClaimsFromRequest(r *http.Request, expectedIssuer string, keySet oidc.KeySet) (*oidc.AccessTokenClaims, error) {
	claims := &oidc.AccessTokenClaims{}
	if err := claimsFromRequest(r, claims, keySet); err != nil {
		return claims, err
	}

	if err := oidc.CheckIssuer(claims, expectedIssuer); err != nil {
		return claims, err
	}

	if err := oidc.CheckExpiration(claims, 0); err != nil {
		return claims, err
	}

	return claims, nil
}

func claimsFromRequest[CLAIMS any](r *http.Request, claims CLAIMS, keySet oidc.KeySet) error {
	authHeader := r.Header.Get("authorization")
	if authHeader == "" {
		return ErrNoAuthorizationHeader
	}

	if !strings.HasPrefix(authHeader, "bearer") &&
		!strings.HasPrefix(authHeader, "Bearer") {
		return ErrMalformedHeader
	}

	token := authHeader[6:]
	token = strings.TrimSpace(token)

	decrypted, err := oidc.DecryptToken(token)
	if err != nil {
		return err
	}
	payload, err := oidc.ParseToken(decrypted, &claims)
	if err != nil {
		return err
	}

	if _, err = oidc.CheckSignature(
		r.Context(),
		decrypted,
		payload,
		[]string{}, // Default to RS256
		keySet,
	); err != nil {
		return err
	}

	return nil
}
