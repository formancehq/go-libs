package client

import (
	"github.com/go-jose/go-jose/v4"
	"github.com/zitadel/oidc/v3/pkg/crypto"

	"github.com/formancehq/go-libs/v3/oidc"
	httphelper "github.com/formancehq/go-libs/v3/oidc/http"
	"github.com/formancehq/go-libs/v3/time"
)

var (
	Encoder = httphelper.Encoder(oidc.NewEncoder())
)

func SignedJWTProfileAssertion(clientID string, audience []string, expiration time.Duration, signer jose.Signer) (string, error) {
	iat := time.Now()
	exp := iat.Add(expiration)
	return crypto.Sign(&oidc.JWTTokenRequest{
		Issuer:    clientID,
		Subject:   clientID,
		Audience:  audience,
		ExpiresAt: oidc.FromTime(exp),
		IssuedAt:  oidc.FromTime(iat),
	}, signer)
}
