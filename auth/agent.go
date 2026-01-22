package auth

import "github.com/formancehq/go-libs/v3/oidc"

type Agent interface {
	GetOrganizationID() string
	HasScope(scope string) bool
	Subject() string
	GetScopes() []string
	GetClientID() string
}

type DefaultAgent struct {
	claims oidc.AccessTokenClaims
}

func (a DefaultAgent) GetScopes() []string {
	return a.claims.Scopes
}

func (a DefaultAgent) GetOrganizationID() string {
	organizationID := a.claims.Claims["organization_id"]
	if organizationIDStr, ok := organizationID.(string); ok {
		return organizationIDStr
	}
	return ""
}

func (a DefaultAgent) HasScope(scope string) bool {
	for _, agentScope := range a.claims.Scopes {
		if scope == agentScope {
			return true
		}
	}
	return false
}

func (a DefaultAgent) Subject() string {
	return a.claims.Subject
}

func (a DefaultAgent) GetClientID() string {
	return a.claims.ClientID
}

func NewDefaultAgent(claims oidc.AccessTokenClaims) DefaultAgent {
	return DefaultAgent{
		claims: claims,
	}
}

var _ Agent = &DefaultAgent{}
