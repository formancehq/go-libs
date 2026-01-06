package auth

import (
	"errors"
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

// Authenticate validates the JWT in the request and returns the user, if valid.
func (ja *JWTAuth) Authenticate(_ http.ResponseWriter, r *http.Request) (bool, error) {
	claims, err := ClaimsFromRequest(r, ja.issuer, ja.keySet)
	if err != nil {
		return false, err
	}

	for _, check := range ja.additionalChecks {
		err := check(r, claims)
		if err != nil {
			return false, err
		}
	}

	if !ja.checkScopes {
		return true, nil
	}
	return checkScopes(ja.service, r.Method, claims.Scopes)
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
