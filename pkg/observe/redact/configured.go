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
	plain    *strings.Replacer
	url      *strings.Replacer
	bounded  []string
	variants []string
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
	var plainReplacements []string
	var urlReplacements []string
	bounded := make([]string, 0, len(ordered))
	for _, variant := range ordered {
		if len(variant) < minUnboundedSecretValue {
			bounded = append(bounded, variant)

			continue
		}
		plainReplacements = append(plainReplacements, variant, Marker)
		urlReplacements = append(urlReplacements, variant, url.QueryEscape(Marker))
	}
	replacer := &valueReplacer{bounded: bounded, variants: ordered}
	if len(plainReplacements) > 0 {
		replacer.plain = strings.NewReplacer(plainReplacements...)
		replacer.url = strings.NewReplacer(urlReplacements...)
	}
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

	return configured.replace(value, Marker, configured.plain)
}

func redactConfiguredURLString(value string) string {
	configured := configuredValues.Load()
	if configured == nil {
		return value
	}

	return configured.replace(value, url.QueryEscape(Marker), configured.url)
}

func (r *valueReplacer) replace(value, marker string, unbounded *strings.Replacer) string {
	if unbounded != nil {
		value = unbounded.Replace(value)
	}

	return replaceBoundedValues(value, r.bounded, marker)
}

func replaceBoundedValues(value string, variants []string, marker string) string {
	if len(variants) == 0 {
		return value
	}

	var out strings.Builder
	out.Grow(len(value))
	for index := 0; index < len(value); {
		matched := ""
		for _, variant := range variants {
			if strings.HasPrefix(value[index:], variant) && valueBounded(value, index, len(variant)) {
				matched = variant

				break
			}
		}
		if matched == "" {
			out.WriteByte(value[index])
			index++

			continue
		}
		out.WriteString(marker)
		index += len(matched)
	}

	return out.String()
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
