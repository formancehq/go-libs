# Configured credential and structured capture redaction

`pkg/observe/redact` owns diagnostic redaction. Resolve credentials at startup,
then call `ConfigureValues` before starting transports or emitting diagnostics.
Configuration replaces the complete process-wide set atomically. Nil clears it;
empty values and literal redaction-marker matches are ignored. Configuration has no logging
side effects and exposes no credential values through errors.

Configured values expand to plain, JSON-escaped, query/path-escaped and padded or
raw standard/URL base64 representations. Longer variants match as substrings;
variants shorter than 16 bytes require token boundaries. ASCII word/token
characters and non-ASCII bytes cannot form a short-value boundary. Longest
variants take precedence. `MaskValues` applies only this configured-value
mechanism, without classifying business field names.

Existing `Bytes`, `Reader`, `Headers`, `HeaderValues`, `URL`, `RequestURI` and
`Path` apply configured masking in addition to their diagnostic name/shape
rules. Name classification and configured matches are collected from the original
input before either replacement can fragment another match. A configured value
that looks like a credential field therefore cannot hide that field's secret. Headers and
URLs keep their existing rendering policy. Diagnostic `Bytes` still normalizes
inert JSON escapes; `Reader` still caps expanded redacted output and can append
a truncation indicator even when the original input fit within the cap. When the
bounded detection window ends inside a possible configured representation, Reader
masks that ambiguous tail without reading beyond the existing cap-plus-one limit.
Reader normalizes supported inert JSON escapes once before both tail detection
and diagnostic matching. Each call uses one immutable configured-value snapshot.

## Structured capture

`JSON(data)` returns a redacted document plus true, or nil plus false when it
cannot safely preserve the payload. Bound input before calling it. It validates
one complete JSON document, refuses escaped credential keys, case-insensitive
duplicate credential keys within the same object, and non-string credential
values. Repeated keys in separate objects are independent. Noncredential
object/array/scalar content and every byte outside redacted strings retain their
original representation, including whitespace, number spelling and escapes.
Diagnostic credential patterns and configured values match original decoded
string contents, including equivalent Unicode/slash escapes, before either
replacement fragments a complete configured match. Original keys remain available
for field-name classification; only changed value strings are re-encoded. This capture path
never applies diagnostic escape normalization or bare-value substitution.
If named diagnostic masking leaves invalid JSON (for example by consuming an
escape before an embedded quote), capture refuses it before scanning configured
strings. A valid input never permits an invalid intermediate representation to
reach that scanner.

`Form(data)` similarly validates URL-encoded form data before redacting.
Credential-named values and configured values are masked. Untouched pairs retain
their exact encoded bytes, order, separators and duplicate keys. Equivalent
encodings match after decoding. Malformed encoding is refused. A false result
means the caller must omit the payload, not fall back to a less strict renderer.

These functions redact diagnostics and opt-in capture. They do not define which
financial or other business fields must be retained. Business evidence policy
belongs to the application; use `MaskValues` there only when removing configured
credentials is intended. Never substitute the diagnostic field-name vocabulary
for an application's business schema.
