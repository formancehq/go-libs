package auth

import "github.com/formancehq/go-libs/v3/oidc"

//go:generate mockgen -source control_plane_agent.go -destination control_plane_agent_generated.go -package auth . ControlPlaneAgent
type ControlPlaneAgent interface {
	GetOrganizationID() string
	HasScope(scope string) bool
	Subject() string
	GetScopes() []string
	GetClientID() string
}

type DefaultControlPlaneAgent struct {
	claims oidc.AccessTokenClaims
}

func (a DefaultControlPlaneAgent) GetScopes() []string {
	return a.claims.Scopes
}

func (a DefaultControlPlaneAgent) GetOrganizationID() string {
	organizationID := a.claims.Claims["organization_id"]
	if organizationIDStr, ok := organizationID.(string); ok {
		return organizationIDStr
	}
	return ""
}

func (a DefaultControlPlaneAgent) HasScope(scope string) bool {
	for _, agentScope := range a.claims.Scopes {
		if scope == agentScope {
			return true
		}
	}
	return false
}

func (a DefaultControlPlaneAgent) Subject() string {
	return a.claims.Subject
}

func (a DefaultControlPlaneAgent) GetClientID() string {
	return a.claims.ClientID
}

func NewDefaultControlPlaneAgent(claims oidc.AccessTokenClaims) DefaultControlPlaneAgent {
	return DefaultControlPlaneAgent{
		claims: claims,
	}
}

var _ ControlPlaneAgent = &DefaultControlPlaneAgent{}
