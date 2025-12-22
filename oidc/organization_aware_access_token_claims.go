package oidc

import "github.com/formancehq/go-libs/v3/time"

const ClaimOrganizationID = "organization_id"

// Convenience wrapper for fetching orgID from custom claims
type OrganizationAwareAccessTokenClaims struct {
	AccessTokenClaims
}

func NewOrganizationAwareAccessTokenClaims(issuer, subject string, audience []string, expiration time.Time, jwtid, clientID string) *OrganizationAwareAccessTokenClaims {
	atc := NewAccessTokenClaims(issuer, subject, audience, expiration, jwtid, clientID)
	return &OrganizationAwareAccessTokenClaims{*atc}
}

func (o *OrganizationAwareAccessTokenClaims) GetOrganizationID() string {
	val, ok := o.Claims[ClaimOrganizationID]
	if !ok {
		return ""
	}

	if orgID, ok := val.(string); ok {
		return orgID
	}
	return ""
}
