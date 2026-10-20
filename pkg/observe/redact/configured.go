package redact

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
)

const minUnboundedSecretValue = 16

var configuredValues atomic.Pointer[valueReplacer]

type valueReplacer struct {
	variants   []configuredVariant
	diagnostic []configuredVariant
}

// A normalized spelling retains the boundary policy of its original known value.
type configuredVariant struct {
	value     string
	unbounded bool
}

// ConfigureValues atomically replaces the process-wide credential set. Empty
// values and literal Marker matches are ignored; nil clears it. Resolve
// credentials before startup and configure once before logging. Values expand to JSON, URL and
// base64 spellings; short variants require token boundaries.
func ConfigureValues(values []string) {
	variants := map[string]struct{}{}
	for _, value := range values {
		if value == "" {
			continue
		}
		for _, variant := range valueVariants(value) {
			if variant != "" && variant != Marker {
				variants[variant] = struct{}{}
			}
		}
	}
	if len(variants) == 0 {
		configuredValues.Store(nil)

		return
	}

	ordered := make([]string, 0, len(variants))
	for variant := range variants {
		ordered = append(ordered, variant)
	}
	slices.SortFunc(ordered, func(left, right string) int {
		if len(left) != len(right) {
			return len(right) - len(left)
		}

		return strings.Compare(left, right)
	})
	replacer := &valueReplacer{}
	diagnostic := make(map[string]bool, len(ordered))
	for _, value := range ordered {
		unbounded := len(value) >= minUnboundedSecretValue
		replacer.variants = append(replacer.variants, configuredVariant{value: value, unbounded: unbounded})
		// Only diagnostic input receives this single inert-escape normalization.
		// Keep both spellings; direct masking and structured capture use variants.
		for _, spelling := range []string{value, string(decodeInertEscapes([]byte(value)))} {
			diagnostic[spelling] = diagnostic[spelling] || unbounded
		}
	}
	for value, unbounded := range diagnostic {
		replacer.diagnostic = append(replacer.diagnostic, configuredVariant{value: value, unbounded: unbounded})
	}
	slices.SortFunc(replacer.diagnostic, func(left, right configuredVariant) int {
		if len(left.value) != len(right.value) {
			return len(right.value) - len(left.value)
		}
		return strings.Compare(left.value, right.value)
	})
	configuredValues.Store(replacer)
}

func valueVariants(value string) []string {
	jsonEscaped := value
	if jsonValue, err := json.Marshal(value); err == nil {
		jsonEscaped = strings.TrimSuffix(strings.TrimPrefix(string(jsonValue), `"`), `"`)
	}

	return []string{
		value,
		url.QueryEscape(value),
		url.PathEscape(value),
		jsonEscaped,
		base64.StdEncoding.EncodeToString([]byte(value)),
		base64.RawStdEncoding.EncodeToString([]byte(value)),
		base64.URLEncoding.EncodeToString([]byte(value)),
		base64.RawURLEncoding.EncodeToString([]byte(value)),
	}
}

// MaskValues removes configured values without applying field-name heuristics.
// Short values match only at token boundaries; text outside matches is retained.
func MaskValues(value string) string {
	configured := configuredValues.Load()
	if configured == nil {
		return value
	}

	return configured.replace(value, Marker)
}

func redactConfiguredURLString(value string) string {
	configured := configuredValues.Load()
	if configured == nil {
		return value
	}

	return configured.replace(value, url.QueryEscape(Marker))
}

func (r *valueReplacer) replace(value, marker string) string {
	return string(applyRedactions([]byte(value), configuredValueRedactions([]byte(value), r.variants), marker))
}

func valueBounded(value string, start, length int) bool {
	return (start == 0 || !credentialTokenByte(value[start-1])) &&
		(start+length == len(value) || !credentialTokenByte(value[start+length]))
}

func credentialTokenByte(value byte) bool {
	return value >= 0x80 ||
		value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		strings.ContainsRune("_-.~%+", rune(value))
}
