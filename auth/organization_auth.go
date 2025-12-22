package auth

import (
	"net/http"

	"github.com/formancehq/go-libs/v3/oidc"
)

// OrganizationIDGetterFn should give the authorizer the ability
// to know what orgID is associated with the resource the requester is attempting to access
type OrganizationIDGetterFn func(*http.Request) (orgID string, err error)

type JWTOrganizationAuth struct {
	issuer      string
	checkScopes bool
	service     string
	keySet      oidc.KeySet

	orgIDgetterFn OrganizationIDGetterFn
}

func NewJWTOrganizationAuth(
	keySet oidc.KeySet,
	issuer string,
	service string,
	checkScopes bool,
	orgIDgetterFn OrganizationIDGetterFn,
) *JWTOrganizationAuth {
	return &JWTOrganizationAuth{
		issuer:        issuer,
		checkScopes:   checkScopes,
		service:       service,
		keySet:        keySet,
		orgIDgetterFn: orgIDgetterFn,
	}
}

// Authenticate validates the JWT in the request whether the user is valid or not.
func (ja *JWTOrganizationAuth) Authenticate(_ http.ResponseWriter, r *http.Request) (bool, error) {
	claims := &oidc.OrganizationAwareAccessTokenClaims{}
	err := claimsFromRequest(r, claims, ja.keySet)
	if err != nil {
		return false, err
	}

	if err := oidc.CheckIssuer(claims, ja.issuer); err != nil {
		return false, err
	}

	if err = oidc.CheckExpiration(claims, 0); err != nil {
		return false, err
	}

	if ja.checkScopes {
		valid, err := checkScopes(ja.service, r.Method, claims.Scopes)
		if err != nil || !valid {
			return false, err
		}
	}

	// run the getter func once we're sure the rest of the token is valid
	expectedOrgID, err := ja.orgIDgetterFn(r)
	if err != nil {
		return false, err
	}

	// if the endpoint doesn't require a particular orgID we consider it valid
	if expectedOrgID == "" {
		return true, nil
	}

	orgID := claims.GetOrganizationID()
	if orgID == "" {
		return false, oidc.ErrOrgIDNotPresent
	}

	if expectedOrgID != "" && orgID != expectedOrgID {
		return false, oidc.ErrOrgIDInvalid
	}
	return true, nil
}
