package redact

import (
	"bytes"
	"regexp"
	"slices"
	"strings"
)

// All ranges refer to the same original input. Replacing one match must not
// erase evidence needed by another policy, including original field names.
type redaction struct {
	start, end int
	prefix     string
}

func redactDiagnosticValues(data []byte, maskBare bool, variants []configuredVariant, additional ...redaction) []byte {
	ranges := append(namedValueRedactions(data, maskBare), additional...)
	ranges = append(ranges, configuredValueRedactions(data, variants)...)
	return applyRedactions(data, ranges, Marker)
}

func configuredValueRedactions(data []byte, variants []configuredVariant) []redaction {
	var ranges []redaction
	if len(variants) > 0 {
		value := string(data)
		for _, known := range variants {
			variant := known.value
			for offset := 0; offset < len(value); {
				relative := strings.Index(value[offset:], variant)
				if relative < 0 {
					break
				}
				start := offset + relative
				if known.unbounded || valueBounded(value, start, len(variant)) {
					ranges = append(ranges, redaction{start: start, end: start + len(variant)})
				}
				offset = start + 1
			}
		}
	}
	return ranges
}

func namedValueRedactions(data []byte, maskBare bool) []redaction {
	var ranges []redaction
	offset := 0
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		content := bytes.TrimSuffix(bytes.TrimSuffix(line, []byte("\n")), []byte("\r"))
		colon := bytes.IndexByte(content, ':')
		if colon >= 0 && SensitiveHeader(strings.TrimSpace(string(content[:colon]))) {
			ranges = append(ranges, redaction{start: offset + colon + 1, end: offset + len(content), prefix: " "})
		}
		offset += len(line)
	}
	appendValues := func(pattern *regexp.Regexp, group int) {
		for _, match := range pattern.FindAllSubmatchIndex(data, -1) {
			ranges = append(ranges, redaction{start: match[2*group], end: match[2*group+1]})
		}
	}
	for _, match := range danglingSecretRe.FindAllSubmatchIndex(data, -1) {
		ranges = append(ranges, redaction{start: match[3], end: match[1]})
	}
	appendValues(kvSecretRe, 2)
	if maskBare {
		appendValues(kvBareSecretRe, 2)
	}
	for _, match := range bearerRe.FindAllSubmatchIndex(data, -1) {
		ranges = append(ranges, redaction{start: match[3], end: match[1]})
	}
	appendValues(headerSecretRe, 2)
	return ranges
}

func applyRedactions(data []byte, ranges []redaction, marker string) []byte {
	if len(ranges) == 0 {
		return bytes.Clone(data)
	}
	slices.SortStableFunc(ranges, func(a, b redaction) int { return a.start - b.start })
	merged := ranges[:0]
	for _, match := range ranges {
		if len(merged) > 0 && match.start <= merged[len(merged)-1].end {
			last := &merged[len(merged)-1]
			last.end = max(last.end, match.end)
		} else {
			merged = append(merged, match)
		}
	}
	var out []byte
	last := 0
	for _, match := range merged {
		out = append(out, data[last:match.start]...)
		out = append(out, match.prefix...)
		out = append(out, marker...)
		last = match.end
	}
	return append(out, data[last:]...)
}

// A detection window can end partway through any configured representation.
// A matching prefix at that edge is ambiguous; mask it without reading ahead.
func configuredTailRedactions(data []byte, variants []configuredVariant) []redaction {
	if len(variants) == 0 {
		return nil
	}
	value := string(data)
	start := len(data)
	end := incompleteEscapeStart(value)
	for _, known := range variants {
		variant := known.value
		for index := max(0, end-len(variant)+1); index < start; index++ {
			if !known.unbounded && index > 0 && credentialTokenByte(value[index-1]) {
				continue
			}
			rawMatch := strings.HasPrefix(variant, value[index:])
			inertMatch := false
			if end < len(value) && index <= end {
				prefix := value[index:end]
				inertMatch = len(prefix) < len(variant) && strings.HasPrefix(variant, prefix) &&
					partialInertEscapeMatches(value[end:], variant[len(prefix)])
			}
			if rawMatch || inertMatch {
				start = index
				break
			}
		}
	}
	if start == len(data) {
		return nil
	}
	return []redaction{{start: start, end: len(data)}}
}

// Complete escapes have already been normalized. Only an unpaired final
// backslash, optionally followed by an unfinished Unicode escape, can still
// hide the next configured byte. An escaped backslash is literal input.
func incompleteEscapeStart(value string) int {
	index := strings.LastIndexByte(value, '\\')
	if index < 0 || len(value)-index >= len(`\u0000`) {
		return len(value)
	}
	first := index
	for first > 0 && value[first-1] == '\\' {
		first--
	}
	if (index-first)%2 != 0 {
		return len(value)
	}
	if tail := value[index:]; len(tail) == 1 || strings.HasPrefix(tail, `\u`) {
		return index
	}
	return len(value)
}

func partialInertEscapeMatches(partial string, next byte) bool {
	const hex = "0123456789abcdef"
	escape := []byte{'\\', 'u', '0', '0', hex[next>>4], hex[next&15]}
	_, inert := inertEscape(escape)
	return inert && (strings.HasPrefix(string(escape), strings.ToLower(partial)) ||
		next == '/' && strings.HasPrefix(`\/`, partial))
}

// Preserve original configured matches when an inert escape crosses their edge.
// Match all policies against original or normalized input before replacement;
// otherwise replacing a known value can hide a credential-bearing field name.
func normalizeConfiguredDiagnostics(data []byte, configured *valueReplacer, truncated bool) ([]byte, []redaction) {
	var raw []redaction
	if configured != nil {
		raw = configuredValueRedactions(data, configured.variants)
		if truncated {
			raw = append(raw, configuredTailRedactions(data, configured.variants)...)
		}
	}
	escapes := jsonEscapeRe.FindAllIndex(data, -1)
	offset := func(index int, end bool) int {
		removed := 0
		for _, span := range escapes {
			if _, ok := inertEscape(data[span[0]:span[1]]); !ok {
				continue
			}
			if index <= span[0] {
				break
			}
			if index < span[1] {
				if end {
					return span[0] - removed + 1
				}
				return span[0] - removed
			}
			removed += span[1] - span[0] - 1
		}
		return index - removed
	}
	for i := range raw {
		raw[i].start = offset(raw[i].start, false)
		raw[i].end = offset(raw[i].end, true)
	}
	return decodeInertEscapes(data), raw
}
