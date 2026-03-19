package jwt

import "net/http"

//go:generate mockgen -source authenticator.go -destination authenticator_generated.go -package jwt . Authenticator
type Authenticator interface {
	Authenticate(w http.ResponseWriter, r *http.Request) (bool, error)
	AuthenticateOnControlPlane(r *http.Request) (ControlPlaneAgent, error)
}
