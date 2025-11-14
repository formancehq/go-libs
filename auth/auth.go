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
	issuer      string
	checkScopes bool
	service     string
	keySet      oidc.KeySet
}

func NewJWTAuth(
	keySet oidc.KeySet,
	issuer string,
	service string,
	checkScopes bool,
) *JWTAuth {
	return &JWTAuth{
		issuer:      issuer,
		checkScopes: checkScopes,
		service:     service,
		keySet:      keySet,
	}
}

// Authenticate validates the JWT in the request and returns the user, if valid.
func (ja *JWTAuth) Authenticate(_ http.ResponseWriter, r *http.Request) (bool, error) {

	claims, err := ClaimsFromRequest(r, ja.issuer, ja.keySet)
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

func ClaimsFromRequest(r *http.Request, expectedIssuer string, keySet oidc.KeySet) (*oidc.AccessTokenClaims, error) {

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
	payload, err := oidc.ParseToken(decrypted, &claims)
	if err != nil {
		return nil, err
	}

	if err := oidc.CheckIssuer(claims, expectedIssuer); err != nil {
		return claims, err
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
