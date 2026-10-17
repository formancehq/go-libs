package oidc

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

func TestRetiredSigningPublicKeyFingerprint(t *testing.T) {
	t.Parallel()

	key := retiredKey()
	der, err := x509.MarshalPKIXPublicKey(key.publicKey)
	require.NoError(t, err)
	digest := sha256.Sum256(der)
	// Pin the same SPKI SHA-256 fingerprint rejected by Auth and Membership
	// at startup. No legacy private-key material is needed for this test.
	require.Equal(t, "679e01c92ee655f2d88fa3612f120e897ba219fcb84cec36045e581e85b323f8", hex.EncodeToString(digest[:]))
	require.Equal(t, hex.EncodeToString(digest[:]), key.fingerprint)
	require.Equal(t, 2048, key.publicKey.N.BitLen())
}

func TestLogRetiredSigningKeyRejection(t *testing.T) {
	t.Parallel()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err)
	digest := sha256.Sum256(der)
	key := retiredSigningKey{publicKey: &privateKey.PublicKey, fingerprint: hex.EncodeToString(digest[:])}
	// Intentionally expired, synthetic claims: detection concerns signatures,
	// not whether the signed claims would otherwise be valid or trustworthy.
	payload := []byte(`{"sub":"do-not-log-subject","jti":"do-not-log-jti","exp":1,"scope":"do-not-log-scope"}`)

	for _, tc := range []struct {
		name    string
		kid     string
		alg     jose.SignatureAlgorithm
		key     *rsa.PrivateKey
		tamper  bool
		wantLog bool
	}{
		{name: "matching key", kid: "default", alg: jose.RS256, key: privateKey, wantLog: true},
		{name: "changed kid", kid: "do-not-log-kid", alg: jose.RS256, key: privateKey, wantLog: true},
		{name: "missing kid", alg: jose.RS256, key: privateKey, wantLog: true},
		{name: "RSA PSS", kid: "do-not-log-kid", alg: jose.PS256, key: privateKey, wantLog: true},
		{name: "spoofed kid", kid: "default", alg: jose.RS256, key: otherKey},
		{name: "tampered payload", kid: "default", alg: jose.RS256, key: privateKey, tamper: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			token := signRetiredKeyTestToken(t, tc.key, tc.alg, tc.kid, payload)
			if tc.tamper {
				parts := strings.Split(token, ".")
				parts[1] = base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"tampered"}`))
				token = strings.Join(parts, ".")
			}
			jws, err := jose.ParseSigned(token, []jose.SignatureAlgorithm{jose.RS256, jose.PS256})
			require.NoError(t, err)
			var output bytes.Buffer
			ctx := logging.ContextWithLogger(context.Background(), logging.NewDefaultLogger(&output, false, true, false))
			ctx = logging.ContextWithField(ctx, "vulnerability_id", "test-reference")
			logRetiredSigningKeyRejection(ctx, jws, key)
			if !tc.wantLog {
				require.Empty(t, output.String())
				return
			}

			var event map[string]any
			require.NoError(t, json.Unmarshal(output.Bytes(), &event))
			require.Equal(t, "error", event["level"])
			require.Equal(t, "Rejected token signed with retired compromised key", event["msg"])
			require.Equal(t, "retired_signing_key_rejected", event["event"])
			require.Equal(t, "test-reference", event["vulnerability_id"])
			require.Equal(t, key.fingerprint, event["key_fingerprint_sha256"])
			// Standard envelope, two static fields, and the trusted context reference.
			require.Len(t, event, 6)
			require.NotContains(t, output.String(), token)
			require.NotContains(t, output.String(), "do-not-log")
			require.NotContains(t, output.String(), "forged")
		})
	}
}

func TestCheckSignatureRetainsResultsWithoutFalseRetiredKeyEvents(t *testing.T) {
	t.Parallel()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	payload := []byte(`{"sub":"synthetic-current-key"}`)
	keySet := NewStaticKeySet(jose.JSONWebKey{Key: &privateKey.PublicKey, KeyID: "default", Use: KeyUseSignature})
	valid := signRetiredKeyTestToken(t, privateKey, jose.RS256, "default", payload)
	invalid := signRetiredKeyTestToken(t, otherKey, jose.RS256, "default", payload)
	for _, tc := range []struct {
		name    string
		token   string
		payload []byte
		algs    []string
		wantErr error
	}{
		{name: "valid current key with legacy kid", token: valid, payload: payload},
		{name: "unrelated invalid signature with legacy kid", token: invalid, payload: payload, wantErr: ErrSignatureInvalid},
		{name: "payload mismatch", token: valid, payload: []byte(`{}`), wantErr: ErrSignatureInvalidPayload},
		{name: "malformed", token: "not-a-jwt", wantErr: ErrParse},
		// The current go-jose error text maps this to ErrParse. Preserve that
		// existing result rather than changing error classification here.
		{name: "unsupported algorithm", token: valid, payload: payload, algs: []string{string(jose.ES256)}, wantErr: ErrParse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			ctx := logging.ContextWithLogger(context.Background(), logging.NewDefaultLogger(&output, false, true, false))
			alg, err := CheckSignature(ctx, tc.token, tc.payload, tc.algs, keySet)
			if tc.wantErr == nil {
				require.NoError(t, err)
				require.Equal(t, jose.RS256, alg)
			} else {
				require.ErrorIs(t, err, tc.wantErr)
				require.Empty(t, alg)
			}
			require.Empty(t, output.String())
		})
	}
}

func signRetiredKeyTestToken(t *testing.T, key *rsa.PrivateKey, alg jose.SignatureAlgorithm, kid string, payload []byte) string {
	t.Helper()
	options := &jose.SignerOptions{}
	if kid != "" {
		options.WithHeader("kid", kid)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: key}, options)
	require.NoError(t, err)
	jws, err := signer.Sign(payload)
	require.NoError(t, err)
	token, err := jws.CompactSerialize()
	require.NoError(t, err)
	return token
}
