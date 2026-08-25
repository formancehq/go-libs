package pyroscopetraces

import (
	flag "github.com/spf13/pflag"
)

const (
	OtelTracesProviderPyroscopeEnabledFlag  = "otel-traces-provider-pyroscope-enabled"
	OtelTracesProviderPyroscopeAllSpansFlag = "otel-traces-provider-pyroscope-all-spans"
)

func AddFlags(flags *flag.FlagSet) {
	flags.Bool(OtelTracesProviderPyroscopeEnabledFlag, false, "Tag spans with a pyroscope.profile.id attribute so Grafana can correlate a trace span with the profile "+
		"samples captured while it ran. This only tags spans; it does not itself start or configure Pyroscope continuous profiling, which "+
		"must already be running on the service for the correlation to resolve to real profile data")
	flags.Bool(OtelTracesProviderPyroscopeAllSpansFlag, false, "Tag every span in a trace, not just its local root span (has no effect unless "+OtelTracesProviderPyroscopeEnabledFlag+" is also set). "+
		"Costs one extra attribute write per span; only enable it if you need to correlate profiles from a specific child span, not just "+
		"the local root")
}
