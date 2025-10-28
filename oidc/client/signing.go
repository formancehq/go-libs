package client

import (
	"github.com/formancehq/go-libs/v3/oidc"
	"github.com/formancehq/go-libs/v3/time"
	"github.com/go-jose/go-jose/v4"
	"github.com/zitadel/oidc/v3/pkg/crypto"
	httphelper "github.com/zitadel/oidc/v3/pkg/http"
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
