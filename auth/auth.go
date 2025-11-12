package auth

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/formancehq/go-libs/v3/collectionutils"
	"github.com/formancehq/go-libs/v3/logging"
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
	logger := logging.FromContext(r.Context()).WithField("auth", "authenticate")
	authHeader := r.Header.Get("authorization")
	if authHeader == "" {
		logger.Error("no authorization header")
		return false, fmt.Errorf("no authorization header")
	}

	if !strings.HasPrefix(authHeader, "bearer") &&
		!strings.HasPrefix(authHeader, "Bearer") {
		return false, fmt.Errorf("malformed authorization header")
	}

	token := authHeader[6:]
	token = strings.TrimSpace(token)

	claims := &oidc.AccessTokenClaims{}
	decrypted, err := oidc.DecryptToken(token)
	if err != nil {
		return false, err
	}
	payload, err := oidc.ParseToken(decrypted, &claims)
	if err != nil {
		return false, err
	}

	if err := oidc.CheckIssuer(claims, ja.issuer); err != nil {
		return false, err
	}

	if _, err = oidc.CheckSignature(
		r.Context(),
		decrypted,
		payload,
		[]string{}, // Default to RS256
		ja.keySet,
	); err != nil {
		return false, err
	}

	if err = oidc.CheckExpiration(claims, 0); err != nil {
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
			logger.Info("not enough scopes")
			return false, fmt.Errorf("missing access, found scopes: '%s' need %s:read|write", strings.Join(scope, ", "), ja.service)
		}
	}

	return true, nil
}
