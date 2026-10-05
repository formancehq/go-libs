package metrics

import (
	"fmt"
	"regexp"
)

// NoPrefix is the prefix value that disables the namespace. A non-empty
// sentinel is needed because the service flag binder ignores empty
// environment variables, so a service that ships a non-empty default prefix
// could not otherwise turn it off through the environment. The empty string
// disables the namespace too.
const NoPrefix = "none"

// MaxPrefixLength bounds the prefix so that prefixed instrument names stay
// well under the 255-character limit the OpenTelemetry SDK enforces at
// instrument creation.
const MaxPrefixLength = 64

// prefixPattern accepts dot-separated segments that stay valid both as an
// OpenTelemetry instrument-name prefix and, once an exporter replaces dots by
// underscores, as a Prometheus metric-name prefix. Each segment starts with a
// letter and ends with a letter or digit, so the joined name never contains
// "..", "._" or "_.".
var prefixPattern = regexp.MustCompile(`^[A-Za-z]([A-Za-z0-9_]*[A-Za-z0-9])?(\.[A-Za-z]([A-Za-z0-9_]*[A-Za-z0-9])?)*$`)

// ParsePrefix validates a metrics prefix and returns the effective one:
// [NoPrefix] and the empty string both yield "", which disables the
// namespace.
func ParsePrefix(s string) (string, error) {
	if s == "" || s == NoPrefix {
		return "", nil
	}
	if len(s) > MaxPrefixLength {
		return "", fmt.Errorf("invalid metrics prefix %q: longer than %d characters", s, MaxPrefixLength)
	}
	if !prefixPattern.MatchString(s) {
		return "", fmt.Errorf("invalid metrics prefix %q: expected %q or dot-separated segments of letters, digits and underscores, each starting with a letter and ending with a letter or digit", s, NoPrefix)
	}

	return s, nil
}

// PrefixedName returns the name an instrument registered as instrumentName is
// exported under, given a prefix validated by [ParsePrefix] ("" and
// [NoPrefix] both mean no namespace). It lets tooling such as dashboard
// generators, and SDK views matching on instrument name, predict exported
// names without registering instruments.
func PrefixedName(instrumentName string, prefix string) string {
	if prefix == "" || prefix == NoPrefix {
		return instrumentName
	}

	return prefix + "." + instrumentName
}
