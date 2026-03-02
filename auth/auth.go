package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/formancehq/go-libs/v3/collectionutils"
	"github.com/formancehq/go-libs/v3/oidc"
)

type JWTAuth struct {
	keySets     map[string]oidc.KeySet // issuer -> keySet
	checkScopes bool
	service     string
}

func NewJWTAuth(
	keySets map[string]oidc.KeySet,
	service string,
	checkScopes bool,
) *JWTAuth {
	return &JWTAuth{
		keySets:     keySets,
		checkScopes: checkScopes,
		service:     service,
	}
}

// Authenticate validates the JWT in the request and returns the user, if valid.
func (ja *JWTAuth) Authenticate(_ http.ResponseWriter, r *http.Request) (bool, error) {

	claims, err := ClaimsFromRequest(r, ja.keySets)
	if err != nil {
		return false, err
	}

	if ja.checkScopes {
		scope := claims.Scopes

		allowed := true //nolint:ineffassign
		switch r.Method {
		case http.MethodOptions, http.MethodGet, http.MethodHead, http.MethodTrace:
			allowed = collectionutils.Contains(scope, ja.service+":read") ||
				collectionutils.Contains(scope, ja.service+":write")
		default:
			allowed = collectionutils.Contains(scope, ja.service+":write")
		}

		if !allowed {
			return false, fmt.Errorf("missing access, found scopes: '%s' need %s:read|write", strings.Join(scope, ", "), ja.service)
		}
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

	if err = oidc.CheckExpiration(claims, 0); err != nil {
		return claims, err
	}

	return claims, nil
}
