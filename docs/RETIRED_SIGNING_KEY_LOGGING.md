# Rejected tokens signed with the retired key

When `oidc.CheckSignature` fails to verify a JWT using the configured key set,
it checks the signature against the public half of the retired default signing
key. A match emits this event through the context logger:

```json
{
  "level": "error",
  "msg": "Rejected token signed with retired compromised key",
  "event": "retired_signing_key_rejected",
  "key_fingerprint_sha256": "679e01c92ee655f2d88fa3612f120e897ba219fcb84cec36045e581e85b323f8"
}
```

The logger supplies the timestamp, existing context fields, and configured trace
correlation. Its formatting depends on the service's logger; the shared `Logger`
interface uses error severity for warning conditions (see [LOGGING.md](LOGGING.md)).
`key_fingerprint_sha256` is the SHA-256 of the DER SubjectPublicKeyInfo.
Services can attach their own incident or vulnerability reference using trusted
request-context logger fields, such as `incident_id` or `vulnerability_id`.
These references are preserved by the diagnostic; never populate them from
untrusted JWT headers or claims.

## Interpretation

This proves that a rejected token was signed with the retired key. It does not
distinguish an old legitimate token from a forged one. Continued events after
the expected token lifetime merit investigation, including stale client caches
and repeated retries. No event is not evidence of historical non-exploitation.

Classification uses the pinned public key, regardless of the token's `kid`.
The event adds no token, token hash, claims, raw headers, or private key material.
Expired tokens can produce the event because the signature check precedes expiry
validation. Malformed tokens, unsupported algorithms, and requests rejected
before `CheckSignature` (for example, an untrusted issuer) do not produce it.

## Delivery and limits

Authentication results and errors are unchanged. The diagnostic never adds the
retired key to accepted JWKS and is only called after signature verification
fails. It is **not** a key-revocation control: a verifier still configured to
trust the old key can accept it and will not emit this event. Rotation and cache
refresh remain required.

Services must adopt a go-libs release containing this change. The shared stack
JWT validator and OIDC ID-token validator use this function; Membership's API
access-token and OAuth token-exchange validation also call it. Other validator
implementations and older go-libs major versions need separate adoption or
backports. A library merge alone does not enable production telemetry.

One event is emitted for every matching rejected signature. Configure log
ingestion capacity, rate limits, retention and alert thresholds for retries or
hostile traffic; account for any dropped records during investigations. The
diagnostic adds one local public-key verification to failed signature checks,
no network requests, and no extra verification to the success path.
