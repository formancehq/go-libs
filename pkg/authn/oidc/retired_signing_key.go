package oidc

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"sync"

	jose "github.com/go-jose/go-jose/v4"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

// This is the public half of the retired default signing key. It is used only
// to classify rejected signatures, never as an authentication trust anchor.
const retiredSigningPublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAth3atoCXldJgHH9EWnZQ
Mvw5O+vVNKMcvrllEGQsLxvIA5xyYPnFt2xU7k1dcN5ViBqPiigVHZNeyyHcdVcl
g26zqjEwYUqH+OPiRFeBn0SwOG+dZLpOIJdKt7OjmUG0xN9egq81dbPVPBPckuWq
B9XMWmM+dtqydBX4lekj+Q1hFn5EWXXuAs9aLIc8DzPz8B+oqwLKZ6k6kC5vpj+E
aBt8ExVywrWftkWewGWRO7fLw0Fj7hamaA1ZTYEqCN+MLDLEd6qmtC2cdgVhZM0R
G2OnTiq5lGzNFmLXGOsquc35HSQjOQqcLL+e/72K3giJ1YCqYWAIIJcc/kNKU8Ht
pwIDAQAB
-----END PUBLIC KEY-----`

type retiredSigningKey struct {
	publicKey   *rsa.PublicKey
	fingerprint string
}

var retiredKey = sync.OnceValue(func() retiredSigningKey {
	// This embedded constant is validated by tests. A parse failure indicates a
	// programming error, not bad input from a request.
	block, _ := pem.Decode([]byte(retiredSigningPublicKeyPEM))
	if block == nil {
		panic("invalid retired signing public key PEM")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		panic(err)
	}
	digest := sha256.Sum256(block.Bytes)
	return retiredSigningKey{
		publicKey:   key.(*rsa.PublicKey),
		fingerprint: hex.EncodeToString(digest[:]),
	}
})

func logRetiredSigningKeyRejection(ctx context.Context, jws *jose.JSONWebSignature, key retiredSigningKey) {
	// Verify against the pinned public key directly: kid and claims are
	// attacker-controlled, and must not determine this classification.
	if _, err := jws.Verify(key.publicKey); err != nil {
		return
	}
	// A matching signature proves use of this key, not whether the token was
	// issued legitimately or forged. The original authentication error is kept.
	logging.FromContext(ctx).WithFields(map[string]any{
		"event":                  "retired_signing_key_rejected",
		"key_fingerprint_sha256": key.fingerprint,
	}).Error("Rejected token signed with retired compromised key")
}
