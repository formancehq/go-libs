// Package redact renders HTTP diagnostics safe for logs.
//
// It is the shared vocabulary for credential-bearing headers, query
// parameters, URL path segments and body fields, so every transport that dumps
// traffic applies the same policy. Everything here is display-only: callers
// redact a copy and send the original bytes on the wire.
//
// The policy is name-based where the data names itself (headers, query
// parameters, body keys) and shape-based where it does not (path segments).
// JSON text is classified as JSON decodes it: a name or value that spells
// its letters as escapes is matched like its plain spelling.
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
	"strconv"
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
	// A JSON credential is not always a string: a numeric password, PIN or
	// passcode is written bare ({"password":123456}). Mask the scalar up to
	// the next JSON delimiter; an object or array value is left to the keys
	// inside it.
	kvBareSecretRe = regexp.MustCompile(`(?i)("[a-z0-9_-]*` + bodySecretKey + `"\s*:\s*)([^\s",{}\[\]]+)`)
	// Error bodies are not always JSON. Cover unquoted key/value echoes while
	// stopping at the usual text and JSON delimiters.
	headerSecretRe = regexp.MustCompile(`(?i)((?:x-[a-z0-9-]*(?:key|secret|token|signature|passphrase)|authorization|` + bodySecretKey + `)\s*[:=]\s*)([^\s",}]+)`)
	// The whole RFC 6750 b64token alphabet, trailing "=" padding included, as
	// it appears raw or inside a JSON string, where a serializer may write "/"
	// as "\/" and any character as "\uXXXX": a narrower class masks a prefix
	// and leaves the rest of the token behind. The separator may be escaped
	// too ("Bearer\u0020..."); decodeInertEscapes leaves it escaped.
	bearerRe = regexp.MustCompile(`(?i)(bearer(?:\s|\\u0020|\\u0009|\\t)+)(?:[a-z0-9\-._~+/]|\\/|\\u[0-9a-f]{4})+(?:=|\\u003d)*`)
	// JSON may spell any character of a string as an escape, so
	// {"\u0070assword":"..."} names a password field and "\u0042earer ..."
	// carries a bearer token that no literal spelling matches. This finds
	// every escape for decodeInertEscapes; an escaped backslash is consumed
	// whole, so the text after it is not read as an escape.
	jsonEscapeRe = regexp.MustCompile(`\\(?:u[0-9a-fA-F]{4}|.)`)
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
// returns a copy and never mutates data. Letters, digits and the token
// punctuation written as JSON escapes are classified, and shown, decoded.
func Bytes(data []byte) []byte {
	out := redactHeaderEchoes(decodeInertEscapes(data))
	out = danglingSecretRe.ReplaceAll(out, []byte("${1}"+Marker))
	out = kvSecretRe.ReplaceAll(out, []byte("${1}"+Marker+"${3}"))
	out = kvBareSecretRe.ReplaceAll(out, []byte("${1}"+Marker))
	out = bearerRe.ReplaceAll(out, []byte("${1}"+Marker))
	out = headerSecretRe.ReplaceAll(out, []byte("${1}"+Marker))

	return out
}

// decodeInertEscapes decodes every JSON escape whose character is inert to
// the matchers: an ASCII letter or digit, or one of "-._~+/". No matcher ends
// a name or a value at one of those, so decoding can only widen what they
// recognise: {"\u0070assword":...} becomes the password field it is, and
// "\u0042earer ..." a bearer token. Every other escape keeps its spelling. A
// decoded quote, backslash or line break would move where a matcher sees a
// string or a line end, and a decoded space or tab would end an unquoted
// key=value secret early and expose its tail. It returns data itself when
// nothing changes and a copy otherwise, so data is never mutated.
func decodeInertEscapes(data []byte) []byte {
	var out []byte
	last := 0
	for _, m := range jsonEscapeRe.FindAllIndex(data, -1) {
		c, ok := inertEscape(data[m[0]:m[1]])
		if !ok {
			continue
		}
		out = append(out, data[last:m[0]]...)
		out = append(out, c)
		last = m[1]
	}
	if out == nil {
		return data
	}

	return append(out, data[last:]...)
}

// inertEscape returns the character a "\/" or "\uXXXX" escape stands for when
// decodeInertEscapes may write it raw.
func inertEscape(escape []byte) (byte, bool) {
	if len(escape) != len(`\u0000`) {
		// A short escape: only "\/" stands for an inert character; "\n" or
		// "\t" stand for a line break and a tab, not for the letter.
		return '/', escape[1] == '/'
	}
	code, err := strconv.ParseUint(string(escape[2:]), 16, 8)
	if err != nil {
		return 0, false
	}
	c := byte(code)
	switch {
	case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		return c, true
	default:
		return c, strings.IndexByte("-._~+/", c) >= 0
	}
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
// applies the display cut and marks it "...[truncated]". The cut applies to
// the redacted text too, so a short secret replaced by the longer Marker never
// pushes the preview past maxBytes. A non-positive maxBytes uses a 4 KiB
// default. Reader never reads more than maxBytes+1 bytes from r, so it is safe
// on arbitrarily large bodies.
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
	if truncated || len(s) > maxBytes {
		s = s[:min(len(s), maxBytes)] + "...[truncated]"
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
		b.WriteString(strings.Join(HeaderValues(name, h.Values(name)), ","))
	}

	return b.String()
}

// urlValuedHeader reports whether a header's value is a URL, which is rendered
// through URL rather than verbatim.
func urlValuedHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Location", "Content-Location", "Referer":
		return true
	}

	return false
}

// HeaderValues renders one header's values for a log line, the policy every
// header rendering shares: a credential header becomes a single Marker, a
// URL-valued header (Location, Content-Location, Referer) goes through URL,
// since a redirect to a presigned URL carries its credential in the query,
// and any other value is kept as is.
func HeaderValues(name string, values []string) []string {
	switch {
	case SensitiveHeader(name):
		return []string{Marker}
	case urlValuedHeader(name):
		return headerURLs(values)
	default:
		return values
	}
}

// headerURLs renders URL-valued header values through URL. A value that does
// not parse is masked whole: it cannot be shown to hold no credential.
func headerURLs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		u, err := url.Parse(value)
		if err != nil {
			out = append(out, Marker)

			continue
		}
		out = append(out, URL(u))
	}

	return out
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
