package jwt

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers"

	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
)

var ErrMissingScope = errors.New("missing scope")

type AdditionalCheck func(*http.Request, *oidc.AccessTokenClaims) error

// OrganizationIDProvider should give the authorizer the ability
// to know what orgID (if any) is associated with the resource the requester is attempting to access
// if no orgID is required, a blank string can be returned
type OrganizationIDProvider func(*http.Request) (orgID string, err error)

func CheckOrganizationIDClaim(fn OrganizationIDProvider) AdditionalCheck {
	return func(r *http.Request, rawClaims *oidc.AccessTokenClaims) error {
		if rawClaims == nil {
			return fmt.Errorf("claims cannot be nil")
		}
		claims := &oidc.OrganizationAwareAccessTokenClaims{AccessTokenClaims: *rawClaims}

		expectedOrgID, err := fn(r)
		if err != nil {
			return err
		}

		// if the endpoint doesn't require a particular orgID we consider it valid
		if expectedOrgID == "" {
			return nil
		}

		orgID := claims.GetOrganizationID()
		if orgID == "" {
			return oidc.ErrOrgIDNotPresent
		}

		if expectedOrgID != "" && orgID != expectedOrgID {
			return oidc.ErrOrgIDInvalid
		}
		return nil
	}
}

func CheckAudienceClaim(expectedAudienceUrl string) AdditionalCheck {
	return func(_ *http.Request, claims *oidc.AccessTokenClaims) error {
		if claims == nil {
			return fmt.Errorf("claims cannot be nil")
		}

		for _, aud := range claims.GetAudience() {
			if aud == expectedAudienceUrl {
				return nil
			}
		}
		return oidc.ErrAudience
	}
}

// use apispec.NewRouter to build a router from an openapi file
// this function can then check if the scopes claim contains the expected scope documented in the spec
func CheckEndpointSpecificScopesClaim(router routers.Router) AdditionalCheck {
	return func(r *http.Request, claims *oidc.AccessTokenClaims) error {
		if claims == nil {
			return fmt.Errorf("claims cannot be nil")
		}
		if router == nil {
			return fmt.Errorf("router cannot be nil")
		}

		route, _, err := router.FindRoute(r)
		if err != nil {
			// if the service is misconfigured it's better to deny access
			return fmt.Errorf("error finding route: %w", err)
		}

		if neededScope := scopeFromOperation(route.Operation); neededScope != "" {
			if !NewDefaultControlPlaneAgent(*claims).HasScope(neededScope) {
				return fmt.Errorf("%w: %q", ErrMissingScope, neededScope)
			}
		}
		return nil
	}
}

// scopeFromOperation returns the first OAuth2/OIDC scope listed in the
// operation's security requirements, or an empty string if none is found.
func scopeFromOperation(op *openapi3.Operation) string {
	if op == nil || op.Security == nil {
		return ""
	}
	for _, secReq := range *op.Security {
		for _, scopes := range secReq {
			if len(scopes) > 0 {
				return scopes[0]
			}
		}
	}
	return ""
}
