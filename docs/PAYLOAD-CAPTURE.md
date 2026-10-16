# Bounded application payload capture

`pkg/observe/payloadlog` emits opt-in, redacted, reassemblable Debug records.
Configure the complete process-wide enable flag and optional maximum before
starting transports. Both explicit enablement and Debug logging are required.
Disabled capture does not invoke producers or perform payload work.

Adapters supply lazy producers, a positive hard ceiling, content type and fields.
The configured maximum can only lower the adapter ceiling; zero leaves it intact
and a negative value omits capture. Read at most the effective limit plus one
sentinel byte. Capture never consumes transport streams itself or returns a
transport error. Adapters retain their original request/response/frame outcomes.

JSON and form content use `observe/redact` structured redaction. Plain text and
untyped empty bodies require explicit trust declarations from the adapter.
Malformed, unsupported, oversized or unsafe payloads emit metadata-only omission
records; producer error text is never logged. Content-type parameters are stripped.
Caller fields are cloned before adding capture metadata. The complete `payload.*`
namespace is reserved: stale capture fields are removed from the clone for both
success and omission records, without mutating caller context. Emitted media-type
metadata masks configured credential values; content admission still uses the
original media type.

The engine redacts the complete admitted payload before SHA-256 hashing, base64
encoding and 64 KiB encoded chunks. Chunk identity, order, counts and the digest
permit exact reconstruction of the redacted document, including multibyte text.
Business schema selection, host identity fields, CLI names, transport admission,
source observations and accounting policy remain outside this engine.

This source proposal depends on the separately reviewed structured-redaction
APIs. It is not released-dependency availability or deployment evidence.
