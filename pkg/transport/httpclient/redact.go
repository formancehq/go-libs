package httpclient

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// DefaultMaxDumpBody caps how much of a request or response body a debug dump
// renders. A dump is a diagnostic, not an archive: the cap is what keeps a
// 32 MiB payload from being copied into a log line.
const DefaultMaxDumpBody = 4096

// RedactedMarker replaces every masked value in a dump.
const RedactedMarker = "[REDACTED]"

// minOpaqueSegment is the length below which a path segment is never treated as
// a credential. It is a floor, not the test: see opaqueSegment.
const minOpaqueSegment = 16

// secretKey is the shared secret-name vocabulary. Header names, query-parameter
// names and body keys are all classified against it, so the three surfaces
// cannot drift apart. The key matcher allows a leading word segment so
// camelCase/compound names (clientSecret, secretAccessKey, X-CB-ACCESS-KEY) are
// covered, not just the bare keyword.
const secretKey = `(?:api[_-]?key|access[_-]?key|secret[_-]?access[_-]?key|client[_-]?secret|secret|passphrase|passcode|password|token|authorization|signature)`

// sensitiveHeaderRe matches header NAMES whose value must never be logged. It
// covers the standard credential carriers plus the vendor spellings connectors
// meet in practice (X-CB-ACCESS-KEY, X-API-Key, X-Auth, X-Auth-Signature,
// X-Auth-Nonce). TestSensitiveHeaderNames holds it against those real spellings.
//
// The bare-auth alternative is anchored exactly rather than matched as a prefix,
// and that distinction is load-bearing in both directions. Bitstamp's X-Auth
// carries "BITSTAMP <apiKey>", so the credential IS the value and the header must
// be masked. Its siblings X-Auth-Timestamp, X-Auth-Version and
// X-Auth-Subaccount-Id carry no secret and stay legible, while X-Auth-Signature
// and X-Auth-Nonce are caught by the word list below.
var sensitiveHeaderRe = regexp.MustCompile(
	`(?i)^(?:(?:x-)?auth(?:entication)?|` +
		`authorization|proxy-authorization|cookie|set-cookie|` +
		`x-[a-z0-9-]*(?:key|secret|token|signature|passphrase|nonce)[a-z0-9-]*|` +
		`[a-z0-9-]*` + secretKey + `[a-z0-9-]*)$`)

// sensitiveParamRe matches query-parameter NAMES whose value must not be logged.
// A parameter is self-describing, so it is classified by name exactly like a
// header; only path segments need the shape heuristic below.
var sensitiveParamRe = regexp.MustCompile(`(?i)^[a-z0-9_-]*` + secretKey + `[a-z0-9_-]*$`)

// Body redaction patterns: a payload can echo back a credential it was sent (an
// OAuth token response is exactly that), so anything that follows a
// secret-bearing key is masked before it can reach a log line.
var (
	kvSecretRe     = regexp.MustCompile(`(?i)("?[a-z0-9]*` + secretKey + `"?\s*[:=]\s*")([^"]*)(")`)
	bearerRe       = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._\-]+`)
	headerSecretRe = regexp.MustCompile(`(?i)((?:x-[a-z0-9-]*(?:key|secret|token|signature|passphrase)|authorization|` + secretKey + `)\s*[:=]\s*)([^\s",}]+)`)
	// danglingSecretRe catches a secret value left unterminated at the very end
	// of a truncated read: the closing quote lies beyond the read window, so
	// neither kvSecretRe (needs the quote) nor headerSecretRe (quoted values)
	// would mask it.
	danglingSecretRe = regexp.MustCompile(`(?i)("?[a-z0-9]*` + secretKey + `"?\s*[:=]\s*"?)[^"]*$`)
)

// IsSensitiveHeader reports whether a header's VALUE is a credential and must be
// masked before it reaches a log line. Exported so a caller with its own
// transport or dump format reuses this vocabulary instead of re-deriving it.
func IsSensitiveHeader(name string) bool {
	return sensitiveHeaderRe.MatchString(name)
}

// RedactHeaders renders headers as a sorted, single-line "name: value" list with
// credential-bearing values masked.
//
// Non-sensitive values are shown in full, which is the point of a dump: a
// Content-Type, an ETag or a rate-limit header is what you are usually looking
// for, and reducing every header to a presence marker makes the dump useless.
func RedactHeaders(h http.Header) string {
	if len(h) == 0 {
		return ""
	}

	var b strings.Builder
	for i, name := range sortedHeaderNames(h) {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(name)
		b.WriteString(": ")
		b.WriteString(redactedHeaderValue(h, name))
	}

	return b.String()
}

// sortedHeaderNames returns the header names in a stable order, so two dumps of
// the same exchange are comparable.
func sortedHeaderNames(h http.Header) []string {
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

// redactedHeaderValue renders one header's value, masked if the name says the
// value is a credential. It indexes the map directly rather than going through
// Header.Values, so a non-canonical key set by hand still resolves.
func redactedHeaderValue(h http.Header, name string) string {
	if IsSensitiveHeader(name) {
		return RedactedMarker
	}

	return strings.Join(h[name], ",")
}

// RedactURL renders a URL safe to put in a log line.
//
// url.URL.Redacted is NOT sufficient here: it masks the userinfo password and
// nothing else, so a credential carried in the path or the query survives it
// verbatim. That is not hypothetical - Alchemy's base URL is
// https://<chain>.g.alchemy.com/v2/<apiKey>, so the API key IS a path segment on
// every JSON-RPC call, and the NFT surface rewrites it to /nft/v3/<apiKey>/...
//
// Three rules, matching how much each part of a URL tells us about itself:
//   - userinfo is dropped entirely, not masked, since even the username is a
//     credential half.
//   - query values are masked by parameter NAME, the same way headers are, because
//     a parameter says what it is. Cursors, limits and filters stay readable.
//   - path segments have no name, so they are masked by shape: long plus
//     mixed letters-and-digits means masked. This over-masks, taking opaque
//     resource ids with it, while keeping endpoint names legible.
//
// Losing an id from the dump is an accepted cost: it is still in the response
// body the dump prints, and the span carries the full URL to a controlled
// backend. A credential in a log line is not recoverable in the same way.
func RedactURL(u *url.URL) string {
	if u == nil {
		return ""
	}

	var b strings.Builder
	if u.Scheme != "" {
		b.WriteString(u.Scheme)
		b.WriteString("://")
	}
	b.WriteString(u.Host) // userinfo deliberately omitted
	b.WriteString(redactRequestURI(u))

	return b.String()
}

// redactRequestURI renders the origin-form "path?query" of a URL with the same
// rules as RedactURL. It is what an HTTP-shaped dump puts on the request line,
// where the host already appears on its own Host line.
func redactRequestURI(u *url.URL) string {
	if u == nil {
		return ""
	}

	out := redactPath(u.EscapedPath())
	if q := redactQuery(u.Query()); q != "" {
		out += "?" + q
	}

	return out
}

// redactPath masks the path segments that could be credentials.
func redactPath(p string) string {
	if p == "" {
		return ""
	}

	segments := strings.Split(p, "/")
	for i, segment := range segments {
		if opaqueSegment(segment) {
			segments[i] = RedactedMarker
		}
	}

	return strings.Join(segments, "/")
}

// opaqueSegment reports whether a path segment looks like a credential.
//
// A path segment has no name, so the only available signal is shape. Length alone
// is not it: real endpoint names reach 16 characters and beyond
// (getContractMetadata, positions_paginated, withdrawal-requests, ...), and
// masking those would hide the single most useful part of a dump.
//
// What separates them is digits. A credential is random, so at this length it
// mixes digits into letters: an Alchemy key (alcht_0123456789abcdef...), a UUID
// resource id. A name someone typed is words, and long endpoint segments are
// letters only. So the test is long AND mixed letters-and-digits, with two
// exemptions for shapes that cannot be credentials and that a dump most needs: a
// 0x-prefixed chain address or transaction hash, and an all-digits id.
//
// The residual gap is a letters-only credential of 16 or more characters, which
// would read as an endpoint name and survive. If a vendor issues one, mask it by
// declaring the value rather than by widening this heuristic, which would start
// eating endpoint names.
func opaqueSegment(segment string) bool {
	if len(segment) < minOpaqueSegment {
		return false
	}
	if strings.HasPrefix(segment, "0x") || strings.HasPrefix(segment, "0X") {
		return false
	}

	var hasDigit, hasLetter bool
	for _, r := range segment {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			hasLetter = true
		}
	}

	return hasDigit && hasLetter
}

// redactQuery re-encodes a query with credential-bearing values masked by name.
func redactQuery(q url.Values) string {
	if len(q) == 0 {
		return ""
	}

	out := make(url.Values, len(q))
	for name, values := range q {
		if sensitiveParamRe.MatchString(name) {
			out[name] = []string{RedactedMarker}

			continue
		}
		out[name] = values
	}

	return out.Encode()
}

// RedactBody reads at most maxBytes of a body and returns it trimmed,
// secret-redacted, and marked when truncated. A non-positive maxBytes uses
// DefaultMaxDumpBody. Redaction runs on the full read buffer BEFORE the display
// cut: truncating first would slice a secret in half and leave the visible head
// unmatched by every pattern.
//
// The reader is bounded here rather than by the caller, so a dump of a 32 MiB
// response never materialises more than the cap.
func RedactBody(r io.Reader, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxDumpBody
	}
	data, _ := io.ReadAll(io.LimitReader(r, int64(maxBytes)+1))
	truncated := len(data) > maxBytes
	if truncated {
		// The read window itself may have cut a value: mask an unterminated
		// secret at the buffer tail before the pattern passes.
		data = danglingSecretRe.ReplaceAll(data, []byte("${1}"+RedactedMarker))
	}

	out := kvSecretRe.ReplaceAll(data, []byte("${1}"+RedactedMarker+"${3}"))
	out = bearerRe.ReplaceAll(out, []byte("${1}"+RedactedMarker))
	out = headerSecretRe.ReplaceAll(out, []byte("${1}"+RedactedMarker))
	s := strings.TrimSpace(string(out))
	if truncated {
		if len(s) > maxBytes {
			s = s[:maxBytes]
		}
		s += "...[truncated]"
	}

	return s
}
