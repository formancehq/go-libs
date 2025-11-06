package auth

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/formancehq/go-libs/v3/oidc"

	"github.com/formancehq/go-libs/v3/collectionutils"
	"github.com/formancehq/go-libs/v3/logging"
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

// validateToken validates and parses the JWT token, returning the claims if valid
func (ja *JWTAuth) validateToken(r *http.Request) (*oidc.AccessTokenClaims, error) {
	logger := logging.FromContext(r.Context()).WithField("auth", "authenticate")
	authHeader := r.Header.Get("authorization")
	if authHeader == "" {
		logger.Error("no authorization header")
		return nil, fmt.Errorf("no authorization header")
	}

	// Extract token using case-insensitive "Bearer " prefix check
	const bearerPrefix = "bearer "
	if len(authHeader) < len(bearerPrefix) ||
		!strings.EqualFold(authHeader[:len(bearerPrefix)], bearerPrefix) {
		return nil, fmt.Errorf("malformed authorization header")
	}

	token := strings.TrimSpace(authHeader[len(bearerPrefix):])

	claims := &oidc.AccessTokenClaims{}
	decrypted, err := oidc.DecryptToken(token)
	if err != nil {
		return nil, err
	}
	payload, err := oidc.ParseToken(decrypted, &claims)
	if err != nil {
		return nil, err
	}

	if err := oidc.CheckIssuer(claims, ja.issuer); err != nil {
		return nil, err
	}

	if _, err = oidc.CheckSignature(
		r.Context(),
		decrypted,
		payload,
		[]string{}, // Default to RS256
		ja.keySet,
	); err != nil {
		return nil, err
	}

	if err = oidc.CheckExpiration(claims, 0); err != nil {
		return nil, err
	}

	if ja.checkScopes {
		scope := claims.Scopes

		allowed := true
		switch r.Method {
		case http.MethodOptions, http.MethodGet, http.MethodHead, http.MethodTrace:
			allowed = collectionutils.Contains(scope, ja.service+":read") ||
				collectionutils.Contains(scope, ja.service+":write")
		default:
			allowed = collectionutils.Contains(scope, ja.service+":write")
		}

		if !allowed {
			logger.Info("not enough scopes")
			return nil, fmt.Errorf("missing access, found scopes: '%s' need %s:read|write", strings.Join(scope, ", "), ja.service)
		}
	}

	return claims, nil
}

// Authenticate validates the JWT in the request and returns the user, if valid.
// This method is kept for backwards compatibility.
func (ja *JWTAuth) Authenticate(_ http.ResponseWriter, r *http.Request) (bool, error) {
	_, err := ja.validateToken(r)
	if err != nil {
		return false, err
	}
	return true, nil
}

// AuthenticateWithClaims validates the JWT in the request and returns the claims.
// This implements the AuthenticatorWithClaims interface, allowing downstream
// middlewares to access validated claims from the request context.
func (ja *JWTAuth) AuthenticateWithClaims(_ http.ResponseWriter, r *http.Request) (bool, *oidc.AccessTokenClaims, error) {
	claims, err := ja.validateToken(r)
	if err != nil {
		return false, nil, err
	}
	return true, claims, nil
}
