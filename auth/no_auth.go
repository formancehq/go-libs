package auth

import (
	"net/http"
)

type noAuth struct{}

func (a noAuth) AuthenticateOnControlPlane(r *http.Request) (ControlPlaneAgent, error) {
	return &NoAgent{}, nil
}

func (a noAuth) Authenticate(w http.ResponseWriter, r *http.Request) (bool, error) {
	return true, nil
}

func NewNoAuth() *noAuth {
	return &noAuth{}
}

type NoAgent struct {
}

func (a NoAgent) GetScopes() []string {
	return make([]string, 0)
}

func (a NoAgent) GetOrganizationID() string {
	return ""
}

func (a NoAgent) HasScope(_ string) bool {
	return true
}

func (a NoAgent) Subject() string {
	return ""
}

func (a NoAgent) GetClientID() string {
	return ""
}
