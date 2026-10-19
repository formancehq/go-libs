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

func redactDiagnosticValues(data []byte, maskBare bool, configured *valueReplacer, additional ...redaction) []byte {
	ranges := append(namedValueRedactions(data, maskBare), additional...)
	if configured != nil {
		value := string(data)
		for _, variant := range configured.variants {
			for offset := 0; offset < len(value); {
				relative := strings.Index(value[offset:], variant)
				if relative < 0 {
					break
				}
				start := offset + relative
				if len(variant) >= minUnboundedSecretValue || valueBounded(value, start, len(variant)) {
					ranges = append(ranges, redaction{start: start, end: start + len(variant)})
				}
				offset = start + 1
			}
		}
	}
	return applyRedactions(data, ranges)
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

func applyRedactions(data []byte, ranges []redaction) []byte {
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
		out = append(out, Marker...)
		last = match.end
	}
	return append(out, data[last:]...)
}

// A detection window can end partway through any configured representation.
// A matching prefix at that edge is ambiguous; mask it without reading ahead.
func configuredTailRedactions(data []byte, configured *valueReplacer) []redaction {
	if configured == nil {
		return nil
	}
	value := string(data)
	start := len(data)
	end := incompleteEscapeStart(value)
	for _, variant := range configured.variants {
		for index := max(0, end-len(variant)+1); index < start; index++ {
			if len(variant) < minUnboundedSecretValue && index > 0 && credentialTokenByte(value[index-1]) {
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
