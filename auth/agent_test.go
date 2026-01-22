package auth_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/formancehq/go-libs/v3/auth"
	"github.com/formancehq/go-libs/v3/oidc"
)

func TestDefaultAgent_GetScopes(t *testing.T) {
	t.Parallel()
	claims := oidc.AccessTokenClaims{
		Scopes: []string{"scope1", "scope2"},
	}
	agent := auth.NewDefaultAgent(claims)

	assert.Equal(t, []string{"scope1", "scope2"}, agent.GetScopes())
}

func TestDefaultAgent_GetOrganizationID(t *testing.T) {
	t.Parallel()
	claims := oidc.AccessTokenClaims{
		Claims: map[string]interface{}{"organization_id": "org123"},
	}
	agent := auth.NewDefaultAgent(claims)

	assert.Equal(t, "org123", agent.GetOrganizationID())
}

func TestDefaultAgent_HasScope(t *testing.T) {
	t.Parallel()
	claims := oidc.AccessTokenClaims{
		Scopes: []string{"scope1", "scope2"},
	}
	agent := auth.NewDefaultAgent(claims)

	assert.True(t, agent.HasScope("scope1"))
	assert.False(t, agent.HasScope("scope3"))
}

func TestDefaultAgent_Subject(t *testing.T) {
	t.Parallel()
	claims := oidc.AccessTokenClaims{
		TokenClaims: oidc.TokenClaims{
			Subject: "subject123",
		},
	}
	agent := auth.NewDefaultAgent(claims)

	assert.Equal(t, "subject123", agent.Subject())
}

func TestDefaultAgent_GetClientID(t *testing.T) {
	t.Parallel()
	claims := oidc.AccessTokenClaims{
		TokenClaims: oidc.TokenClaims{
			ClientID: "client123",
		},
	}
	agent := auth.NewDefaultAgent(claims)

	assert.Equal(t, "client123", agent.GetClientID())
}
