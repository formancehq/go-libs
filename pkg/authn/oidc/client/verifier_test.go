package client

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"testing"
	stdtime "time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
	libtime "github.com/formancehq/go-libs/v5/pkg/types/time"
)

func TestVerifyIDTokenRejectsTokenBeforeNotBeforeTime(t *testing.T) {
	t.Parallel()

	keySet, privateKey := setupVerifierTestKeySet(t)
	now := stdtime.Now().UTC()
	issuer := "https://test-issuer.example.com"
	clientID := "test-client"
	claims := oidc.NewIDTokenClaims(
		"test-session",
		issuer,
		"test-subject",
		[]string{clientID},
		libtime.New(now.Add(stdtime.Hour)),
		libtime.New(now),
		clientID,
	)
	claims.NotBefore = oidc.FromTime(libtime.New(now.Add(30 * stdtime.Minute)))

	token := signIDTokenClaims(t, privateKey, claims)
	verifier := NewIDTokenVerifier(
		clientID,
		keySet,
		WithIssuer(func(value string) bool { return value == issuer }),
		WithNonce(nil),
	)

	_, _, err := VerifyIDToken[*oidc.IDTokenClaims](context.Background(), token, verifier)

	require.ErrorIs(t, err, oidc.ErrNotBefore)
}

func TestVerifyIDTokenRejectsCustomClaimsBeforeNotBeforeTime(t *testing.T) {
	t.Parallel()

	keySet, privateKey := setupVerifierTestKeySet(t)
	now := stdtime.Now().UTC()
	issuer := "https://test-issuer.example.com"
	clientID := "test-client"
	claims := &customIDTokenClaims{
		Issuer:     issuer,
		Subject:    "test-subject",
		Audience:   []string{clientID},
		Expiration: oidc.FromTime(libtime.New(now.Add(stdtime.Hour))),
		IssuedAt:   oidc.FromTime(libtime.New(now)),
		NotBefore:  oidc.FromTime(libtime.New(now.Add(30 * stdtime.Minute))),
	}

	token := signIDTokenClaims(t, privateKey, claims)
	verifier := NewIDTokenVerifier(
		clientID,
		keySet,
		WithIssuer(func(value string) bool { return value == issuer }),
		WithNonce(nil),
	)

	_, _, err := VerifyIDToken[*customIDTokenClaims](context.Background(), token, verifier)

	require.ErrorIs(t, err, oidc.ErrNotBefore)
}

func setupVerifierTestKeySet(t *testing.T) (oidc.KeySet, *rsa.PrivateKey) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	jwk := jose.JSONWebKey{
		Key:       &privateKey.PublicKey,
		KeyID:     "test-key-id",
		Algorithm: string(jose.RS256),
		Use:       oidc.KeyUseSignature,
	}

	return oidc.NewStaticKeySet(jwk), privateKey
}

func signIDTokenClaims(t *testing.T, privateKey *rsa.PrivateKey, claims any) string {
	t.Helper()

	signer, err := jose.NewSigner(
		jose.SigningKey{
			Algorithm: jose.RS256,
			Key:       privateKey,
		},
		(&jose.SignerOptions{}).WithHeader("kid", "test-key-id"),
	)
	require.NoError(t, err)

	claimsJSON, err := json.Marshal(claims)
	require.NoError(t, err)

	signed, err := signer.Sign(claimsJSON)
	require.NoError(t, err)

	token, err := signed.CompactSerialize()
	require.NoError(t, err)

	return token
}

type customIDTokenClaims struct {
	Issuer     string        `json:"iss,omitempty"`
	Subject    string        `json:"sub,omitempty"`
	Audience   oidc.Audience `json:"aud,omitempty"`
	Expiration oidc.Time     `json:"exp,omitempty"`
	IssuedAt   oidc.Time     `json:"iat,omitempty"`
	NotBefore  oidc.Time     `json:"nbf,omitempty"`
}

func (c *customIDTokenClaims) GetIssuer() string {
	return c.Issuer
}

func (c *customIDTokenClaims) GetSubject() string {
	return c.Subject
}

func (c *customIDTokenClaims) GetAudience() []string {
	return c.Audience
}

func (c *customIDTokenClaims) GetExpiration() libtime.Time {
	return c.Expiration.AsTime()
}

func (c *customIDTokenClaims) GetIssuedAt() libtime.Time {
	return c.IssuedAt.AsTime()
}

func (c *customIDTokenClaims) GetNonce() string {
	return ""
}

func (c *customIDTokenClaims) GetAuthenticationContextClassReference() string {
	return ""
}

func (c *customIDTokenClaims) GetAuthTime() libtime.Time {
	return libtime.Time{}
}

func (c *customIDTokenClaims) GetAuthorizedParty() string {
	return ""
}
