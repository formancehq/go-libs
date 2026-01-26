package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/formancehq/go-libs/v3/logging"
	"github.com/formancehq/go-libs/v3/oidc"
)

const (
	ContextKeyAuthClaimOrganizationID = "AuthClaim-OrganizationID"
	ContextKeyAuthClaimClientID       = "AuthClaim-ClientID"
)

func Middleware(ja Authenticator) func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authenticated, err := ja.Authenticate(w, r)
			if err != nil {
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

func ControlPlaneMiddleware(ja Authenticator) func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			agt, err := ja.AuthenticateOnControlPlane(r)
			if err != nil {
				logging.FromContext(r.Context()).Debugf("failed authentication: %v", err)
				// client is authenticated but doesn't have permission to access this resource
				if errors.Is(err, oidc.ErrOrgIDNotPresent) || errors.Is(err, oidc.ErrOrgIDInvalid) {
					w.WriteHeader(http.StatusForbidden)
					return
				}

				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			orgID := agt.GetOrganizationID()
			clientID := agt.GetClientID()
			ctx := context.WithValue(r.Context(), ContextKeyAuthClaimOrganizationID, orgID)
			ctx = context.WithValue(ctx, ContextKeyAuthClaimClientID, clientID)

			handler.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
