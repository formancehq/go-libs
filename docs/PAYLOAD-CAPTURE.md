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

JSON and form content require an application-owned `Record.Sanitize` callback.
The callback receives a private copy of the bounded bytes and returns safe content
or false to omit. Missing sanitization omits structured content. No configured-value
registry or structured redactor is supplied by this package. Explicitly trusted plain text may pass through unchanged; callers must supply
a sanitizer when that content needs masking. The shared redaction package is removed.
Malformed, unsupported, oversized or rejected payloads emit metadata-only omission;
producer error text is never logged. Sanitized output must remain valid UTF-8,
JSON output must remain valid JSON, and output cannot exceed the effective limit.
The application owns schema, key-preservation and optional known-value policy.

Caller fields are cloned before adding capture metadata. The complete `payload.*`
namespace is reserved and replaced on both success and omission. Media-type
metadata is limited to fixed JSON/form/plain/unsupported categories; provider
subtypes and parameters are never echoed.

The engine sanitizes the complete admitted payload through the caller policy before SHA-256 hashing, base64
encoding and 64 KiB encoded chunks. Chunk identity, order, counts and the digest
permit exact reconstruction of the redacted document, including multibyte text.
Business schema selection, host identity fields, CLI names, transport admission,
source observations and accounting policy remain outside this engine.

The withdrawn configured/structured redaction proposal is not a dependency.
This remains a draft API, not released-dependency or deployment evidence.
