package auth

import (
	"errors"
	"net/http"

	"github.com/formancehq/go-libs/v3/oidc"
)

//go:generate mockgen -source middleware.go -destination authenticator_generated.go -package auth . Authenticator
type Authenticator interface {
	Authenticate(w http.ResponseWriter, r *http.Request) (bool, error)
}

func Middleware(ja Authenticator) func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authenticated, err := ja.Authenticate(w, r)
			if err != nil {
				// client is authenticated but doesn't have permission to access this resource
				if errors.Is(err, oidc.ErrOrgIDNotPresent) || errors.Is(err, oidc.ErrOrgIDInvalid) {
					w.WriteHeader(http.StatusForbidden)
					return
				}

				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			if !authenticated {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			handler.ServeHTTP(w, r)
		})
	}
}
