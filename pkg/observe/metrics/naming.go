package metrics

import (
	"fmt"
	"regexp"
	"strings"
)

// Naming selects the convention used to format the names of the instruments
// a service creates through the injected metric.MeterProvider. The
// semantic-convention metrics emitted by OpenTelemetry instrumentation
// libraries (Go runtime, host, otelhttp, otelgrpc) use the global provider and
// are not affected; see [NewRenamingMeterProvider].
type Naming string

const (
	// NamingOTel keeps the OpenTelemetry dot notation, joined to the prefix
	// with a "." (e.g. "acme.payments.admission.duration").
	NamingOTel Naming = "otel"

	// NamingProm rewrites names to the Prometheus convention: the prefixed
	// name with every "." replaced by "_" (e.g.
	// "acme_payments_admission_duration").
	NamingProm Naming = "prom"
)

// DefaultNaming is the policy applied when no naming is configured.
const DefaultNaming = NamingOTel

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
// OpenTelemetry instrument-name prefix and, once dots are replaced by
// underscores, as a Prometheus metric-name prefix. Each segment starts with a
// letter and ends with a letter or digit, so the joined name never contains
// "..", "._" or "_.".
var prefixPattern = regexp.MustCompile(`^[A-Za-z]([A-Za-z0-9_]*[A-Za-z0-9])?(\.[A-Za-z]([A-Za-z0-9_]*[A-Za-z0-9])?)*$`)

// ParseNaming validates a naming value. The empty string yields
// [DefaultNaming], so a zero [ModuleConfig] keeps the upstream names.
func ParseNaming(s string) (Naming, error) {
	switch Naming(s) {
	case "":
		return DefaultNaming, nil
	case NamingOTel, NamingProm:
		return Naming(s), nil
	default:
		return "", fmt.Errorf("invalid metrics naming %q: expected %q or %q", s, NamingOTel, NamingProm)
	}
}

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

// TransformName returns the name an instrument registered as instrumentName
// is exported under, given a naming policy and a prefix validated by
// [ParsePrefix] ("" and [NoPrefix] both mean no namespace). It lets tooling
// such as dashboard generators, and SDK views matching on instrument name,
// predict exported names without registering instruments.
func TransformName(instrumentName string, naming Naming, prefix string) string {
	name := instrumentName
	if prefix != "" && prefix != NoPrefix {
		name = prefix + "." + instrumentName
	}
	if naming == NamingProm {
		return strings.ReplaceAll(name, ".", "_")
	}

	return name
}
