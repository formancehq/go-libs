package auth

import (
	"fmt"
	"net/http"

	"github.com/formancehq/go-libs/v4/oidc"
)

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
