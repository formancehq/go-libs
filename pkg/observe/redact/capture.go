package redact

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strings"
)

var sensitiveJSONKeyRe = regexp.MustCompile(`(?i)^[a-z0-9_-]*` + bodySecretKey + `$`)
var jsonKeyRe = regexp.MustCompile(`"(?:\\.|[^"\\])*"\s*:`)

// JSON redacts string credentials in one valid JSON document, retaining all
// bytes outside redacted strings. Duplicate credential keys (case-insensitive),
// escaped credential names and non-string credential values return nil, false.
// Configured credentials also match decoded string contents. Bound input first.
func JSON(data []byte) ([]byte, bool) {
	if !json.Valid(data) {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if !jsonCredentialKeysUnique(decoder) {
		return nil, false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, false
	}
	for _, location := range jsonKeyRe.FindAllIndex(data, -1) {
		match := data[location[0]:location[1]]
		colon := bytes.LastIndexByte(match, ':')
		keyToken := bytes.TrimSpace(match[:colon])
		var key string
		if err := json.Unmarshal(keyToken, &key); err != nil {
			return nil, false
		}
		if !sensitiveJSONKeyRe.MatchString(key) {
			continue
		}
		if bytes.ContainsRune(keyToken, '\\') {
			return nil, false
		}
		valueStart := location[1]
		for valueStart < len(data) && isJSONSpace(data[valueStart]) {
			valueStart++
		}
		if valueStart >= len(data) || data[valueStart] != '"' {
			return nil, false
		}
	}

	out := redactNamedValues(data, false)
	// Diagnostic patterns can consume an escape before a quote. The semantic
	// string scanner below requires valid JSON, so refuse that intermediate form.
	if !json.Valid(out) {
		return nil, false
	}
	out = redactConfiguredJSONStrings(out)
	if !json.Valid(out) {
		return nil, false
	}

	return out, true
}

func redactConfiguredJSONStrings(data []byte) []byte {
	configured := configuredValues.Load()
	if configured == nil {
		return data
	}

	out := make([]byte, 0, len(data))
	last := 0
	for index := 0; index < len(data); {
		relativeStart := bytes.IndexByte(data[index:], '"')
		if relativeStart < 0 {
			break
		}
		contentStart := index + relativeStart + 1
		contentEnd := contentStart
		for data[contentEnd] != '"' {
			if data[contentEnd] == '\\' {
				contentEnd++
			}
			contentEnd++
		}
		after := contentEnd + 1
		for after < len(data) && isJSONSpace(data[after]) {
			after++
		}
		if after < len(data) && data[after] == ':' {
			index = contentEnd + 1

			continue
		}
		var content string
		if err := json.Unmarshal(data[contentStart-1:contentEnd+1], &content); err != nil {
			return nil
		}
		redacted := configured.replace(content, Marker, configured.plain)
		if redacted != content {
			rendered, err := json.Marshal(redacted)
			if err != nil {
				return nil
			}
			out = append(out, data[last:contentStart]...)
			out = append(out, rendered[1:len(rendered)-1]...)
			last = contentEnd
		}
		index = contentEnd + 1
	}
	out = append(out, data[last:]...)

	return out
}

func isJSONSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\r' || b == '\n' }

func jsonCredentialKeysUnique(decoder *json.Decoder) bool {
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return true
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return false
			}
			key, ok := keyToken.(string)
			if !ok {
				return false
			}
			if sensitiveJSONKeyRe.MatchString(key) {
				normalizedKey := strings.ToLower(key)
				if _, duplicate := seen[normalizedKey]; duplicate {
					return false
				}
				seen[normalizedKey] = struct{}{}
			}
			if !jsonCredentialKeysUnique(decoder) {
				return false
			}
		}
		end, err := decoder.Token()

		return err == nil && end == json.Delim('}')
	case '[':
		for decoder.More() {
			if !jsonCredentialKeysUnique(decoder) {
				return false
			}
		}
		end, err := decoder.Token()

		return err == nil && end == json.Delim(']')
	default:
		return false
	}
}

// Form redacts valid URL-encoded fields without changing unrelated encoding,
// pair order or duplicate keys. Malformed encoding returns nil, false.
func Form(data []byte) ([]byte, bool) {
	if _, err := url.ParseQuery(string(data)); err != nil {
		return nil, false
	}

	parts := strings.Split(string(data), "&")
	for index, part := range parts {
		key, value, hasValue := strings.Cut(part, "=")
		decodedKey, err := url.QueryUnescape(key)
		if err != nil {
			return nil, false
		}
		if hasValue {
			if _, err := url.QueryUnescape(value); err != nil {
				return nil, false
			}
		}
		if sensitiveParamRe.MatchString(decodedKey) {
			parts[index] = key + "=" + url.QueryEscape(Marker)

			continue
		}
		if hasValue {
			decoded, _ := url.QueryUnescape(value) // validated above
			if redacted := MaskValues(decoded); redacted != decoded {
				parts[index] = key + "=" + url.QueryEscape(redacted)
			}
		}
	}

	return []byte(strings.Join(parts, "&")), true
}
