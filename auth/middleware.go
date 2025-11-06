package auth

import (
	"context"
	"net/http"

	"github.com/formancehq/go-libs/v3/oidc"
)

type contextKey string

const ClaimsContextKey contextKey = "auth_claims"

// Authenticator is the original interface (kept for backwards compatibility)
type Authenticator interface {
	Authenticate(w http.ResponseWriter, r *http.Request) (bool, error)
}

// AuthenticatorWithClaims is an optional interface that Authenticators can implement
// to provide claims that will be stored in the request context.
// This allows downstream middlewares (like audit) to access validated claims
// without re-parsing the JWT.
type AuthenticatorWithClaims interface {
	Authenticator
	AuthenticateWithClaims(w http.ResponseWriter, r *http.Request) (bool, *oidc.AccessTokenClaims, error)
}

func Middleware(ja Authenticator) func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var authenticated bool
			var err error

			// Check if the authenticator supports claims (new interface)
			if authWithClaims, ok := ja.(AuthenticatorWithClaims); ok {
				var claims *oidc.AccessTokenClaims
				authenticated, claims, err = authWithClaims.AuthenticateWithClaims(w, r)
				if err != nil || !authenticated {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				// Store claims in context for downstream middlewares
				ctx := context.WithValue(r.Context(), ClaimsContextKey, claims)
				r = r.WithContext(ctx)
			} else {
				// Fallback to old interface (backwards compatible)
				authenticated, err = ja.Authenticate(w, r)
				if err != nil || !authenticated {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
			}

			handler.ServeHTTP(w, r)
		})
	}
}

// GetClaimsFromContext extracts the validated claims from the request context.
// Returns nil if no claims are present (e.g., when using an old Authenticator implementation).
func GetClaimsFromContext(ctx context.Context) *oidc.AccessTokenClaims {
	claims, ok := ctx.Value(ClaimsContextKey).(*oidc.AccessTokenClaims)
	if !ok {
		return nil
	}
	return claims
}
