// Package redact renders HTTP diagnostics safe for logs.
//
// It is the shared vocabulary for credential-bearing headers, query
// parameters, URL path segments and body fields, so every transport that dumps
// traffic applies the same policy. Everything here is display-only: callers
// redact a copy and send the original bytes on the wire.
//
// The policy is name-based where the data names itself (headers, query
// parameters, body keys) and shape-based where it does not (path segments).
// Redact a complete bounded payload before applying any display cut:
// truncating first can slice a secret in half and leave the visible head
// unmatched by every pattern. Reader does both in the right order.
package redact

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Marker replaces every masked value.
const Marker = "[REDACTED]"

const (
	// defaultMaxBody is the preview cap Reader applies to a non-positive limit.
	defaultMaxBody = 4096
	// minOpaqueSegment is the shortest path segment treated as a possible
	// credential. Shorter mixed segments (v2, page1) are routing, not secrets.
	minOpaqueSegment = 16
	credentialKey    = `(?:api[_-]?key|access[_-]?key|secret[_-]?access[_-]?key|client[_-]?secret|secret|passphrase|passcode|password|token|authorization|credentials?|signature)`
	// Signing payloads sometimes distinguish the key from its key ID (for
	// example api_key_id). Keep this vocabulary explicit: a generic "id" suffix
	// would incorrectly classify domain fields such as an NFT token_id as
	// authentication material.
	credentialIDKey = `(?:api[_-]?key|access[_-]?key)[_-]?id`
	bodySecretKey   = `(?:auth|` + credentialIDKey + `|` + credentialKey + `)`
)

var (
	// The string-value matcher accepts JSON escape sequences: a [^"]* shape
	// stops at an escaped quote and exposes the rest of the secret.
	kvSecretRe = regexp.MustCompile(`(?i)("?[a-z0-9]*` + bodySecretKey + `"?\s*[:=]\s*")((?:\\.|[^"\\])*)(")`)
	// Error bodies are not always JSON. Cover unquoted key/value echoes while
	// stopping at the usual text and JSON delimiters.
	headerSecretRe = regexp.MustCompile(`(?i)((?:x-[a-z0-9-]*(?:key|secret|token|signature|passphrase)|authorization|` + bodySecretKey + `)\s*[:=]\s*)([^\s",}]+)`)
	bearerRe       = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._\-]+`)
	// A bounded read may end inside a quoted secret. Mask the incomplete tail
	// before the display cut is applied.
	danglingSecretRe  = regexp.MustCompile(`(?i)("?[a-z0-9]*` + bodySecretKey + `"?\s*[:=]\s*"?)(?:\\.|[^"\\\r\n])*\\?$`)
	sensitiveHeaderRe = regexp.MustCompile(
		`(?i)^(?:(?:x-)?auth(?:entication)?|` +
			`authorization|proxy-authorization|cookie|set-cookie|` +
			`x-[a-z0-9-]*(?:key|secret|token|signature|passphrase|nonce)[a-z0-9-]*|` +
			`[a-z0-9-]*` + credentialKey + `[a-z0-9-]*)$`)
	sensitiveParamRe = regexp.MustCompile(`(?i)^[a-z0-9_-]*` + bodySecretKey + `[a-z0-9_-]*$`)
)

// Bytes masks credential-bearing values in a complete bounded payload: JSON
// and key=value fields named like credentials, bearer tokens, and echoed
// header lines whose name SensitiveHeader classifies as a credential. It
// returns a copy and never mutates data.
func Bytes(data []byte) []byte {
	out := redactHeaderEchoes(data)
	out = danglingSecretRe.ReplaceAll(out, []byte("${1}"+Marker))
	out = kvSecretRe.ReplaceAll(out, []byte("${1}"+Marker+"${3}"))
	out = bearerRe.ReplaceAll(out, []byte("${1}"+Marker))
	out = headerSecretRe.ReplaceAll(out, []byte("${1}"+Marker))

	return out
}

// redactHeaderEchoes masks the whole value of every "Name: value" line whose
// name is a sensitive header, preserving the line ending.
func redactHeaderEchoes(data []byte) []byte {
	lines := bytes.SplitAfter(data, []byte("\n"))
	for index, line := range lines {
		content := bytes.TrimSuffix(line, []byte("\n"))
		ending := line[len(content):]
		content = bytes.TrimSuffix(content, []byte("\r"))
		if len(content) < len(line)-len(ending) {
			ending = append([]byte("\r"), ending...)
		}
		name, _, found := bytes.Cut(content, []byte(":"))
		if !found || !SensitiveHeader(strings.TrimSpace(string(name))) {
			continue
		}
		lines[index] = append(append(append([]byte(nil), name...), ':', ' '), Marker...)
		lines[index] = append(lines[index], ending...)
	}

	return bytes.Join(lines, nil)
}

// Reader reads at most maxBytes from r, redacts the complete read window, then
// applies the display cut and marks it "...[truncated]". A non-positive
// maxBytes uses a 4 KiB default. Reader never reads more than maxBytes+1 bytes
// from r, so it is safe on arbitrarily large bodies.
func Reader(r io.Reader, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = defaultMaxBody
	}
	data := make([]byte, maxBytes+1)
	n, _ := io.ReadFull(r, data)
	data = data[:n]
	truncated := len(data) > maxBytes
	if truncated {
		data = danglingSecretRe.ReplaceAll(data, []byte("${1}"+Marker))
	}

	s := strings.TrimSpace(string(Bytes(data)))
	if truncated {
		if len(s) > maxBytes {
			s = s[:maxBytes]
		}
		s += "...[truncated]"
	}

	return s
}

// SensitiveHeader reports whether an HTTP header value must be masked: the
// standard credential headers (Authorization, Proxy-Authorization, Cookie,
// Set-Cookie), Auth and X-Auth, vendor X- headers containing key, secret,
// token, signature, passphrase or nonce, and any name containing a credential
// term such as api-key, client-secret or password. Diagnostic headers such as
// Content-Type, ETag, Retry-After and rate-limit fields stay visible.
func SensitiveHeader(name string) bool { return sensitiveHeaderRe.MatchString(name) }

// Headers renders headers as a sorted, single-line "Name: value; Name: value"
// list. Sensitive values are replaced by Marker; every other value is shown in
// full, joined by commas, because a content type, an ETag or a rate-limit
// header is what a diagnostic is usually for.
func Headers(h http.Header) string {
	if len(h) == 0 {
		return ""
	}

	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for i, name := range names {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(name)
		b.WriteString(": ")
		if SensitiveHeader(name) {
			b.WriteString(Marker)

			continue
		}
		b.WriteString(strings.Join(h.Values(name), ","))
	}

	return b.String()
}

// URL renders u for a log line. url.URL.Redacted is not sufficient: it masks
// the userinfo password and nothing else, so a credential carried in the path
// or the query survives it verbatim. URL applies three rules, matching how
// much each part of a URL says about itself:
//   - userinfo is dropped entirely, not masked, since even the username is a
//     credential half;
//   - query values are masked by parameter name, the way headers are, so
//     cursors, limits and filters stay readable;
//   - path segments have no name, so they are masked by shape (see Path).
//
// The fragment is dropped: it is never sent on the wire.
func URL(u *url.URL) string {
	if u == nil {
		return ""
	}

	var b strings.Builder
	if u.Scheme != "" {
		b.WriteString(u.Scheme)
		b.WriteString("://")
	}
	b.WriteString(u.Host)
	b.WriteString(Path(u.EscapedPath()))
	if q := query(u.Query()); q != "" {
		b.WriteString("?")
		b.WriteString(q)
	}

	return b.String()
}

// RequestURI is the redacted counterpart of url.URL.RequestURI: the
// origin-form request target (path and query) with the same masking as URL.
// It returns "/" for an empty path, and renders an opaque URL's opaque part
// through Path.
func RequestURI(u *url.URL) string {
	if u == nil {
		return "/"
	}

	path := u.EscapedPath()
	if u.Opaque != "" {
		path = u.Opaque
	}
	uri := Path(path)
	if uri == "" {
		uri = "/"
	}
	if q := query(u.Query()); q != "" {
		uri += "?" + q
	}

	return uri
}

// Path masks escaped path segments whose shape is consistent with an opaque
// credential: at least 16 bytes mixing letters and digits. Endpoint names,
// numeric identifiers and 0x-prefixed chain addresses and hashes stay
// readable. The rule deliberately over-masks opaque resource identifiers such
// as UUIDs: losing an identifier from a log line is recoverable, a credential
// in one is not.
func Path(path string) string {
	if path == "" {
		return ""
	}

	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if opaqueSegment(segment) {
			segments[i] = Marker
		}
	}

	return strings.Join(segments, "/")
}

// query re-encodes values with every credential-named parameter masked.
// Pairs that url.ParseQuery rejects never reach it, so a malformed pair is
// omitted rather than echoed.
func query(values url.Values) string {
	if len(values) == 0 {
		return ""
	}

	out := make(url.Values, len(values))
	for name, value := range values {
		if sensitiveParamRe.MatchString(name) {
			out[name] = []string{Marker}

			continue
		}
		out[name] = value
	}

	return out.Encode()
}

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
