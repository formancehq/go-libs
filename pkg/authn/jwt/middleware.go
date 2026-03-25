package jwt

import (
	"context"
	"errors"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

const (
	// will be set by control plane middleware
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
			span := trace.SpanFromContext(r.Context())
			agt, err := ja.AuthenticateOnControlPlane(r)
			if err != nil {
				if agt != nil {
					span.SetAttributes(attribute.String("clientID", agt.GetClientID()))
					span.SetAttributes(attribute.String("organizationID", agt.GetOrganizationID()))
				}

				// an app using CheckEndpointSpecificScopesClaim checks should ensure all endpoints are documented
				// even if they do not require particular scopes
				if errors.Is(err, ErrUndocumentedRoute) {
					logging.FromContext(r.Context()).WithField("path", r.URL.Path).Errorf("requested route is not public: %v", err)
					w.WriteHeader(http.StatusForbidden)
					return
				}
				logging.FromContext(r.Context()).Debugf("failed authentication: %v", err)

				// client is authenticated but doesn't have permission to access this resource
				if errors.Is(err, oidc.ErrOrgIDNotPresent) || errors.Is(err, oidc.ErrOrgIDInvalid) || errors.Is(err, ErrMissingScope) {
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
