package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/formancehq/go-libs/v3/oidc"
)

const (
	ContextKeyAuthClaimOrganizationID = "AuthClaim-OrganizationID"
	ContextKeyAuthClaimClientID       = "AuthClaim-ClientID"
)

func Middleware(ja Authenticator) func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			agt, err := ja.AuthenticateWithAgent(r)
			if err != nil {
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
