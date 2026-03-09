package profiling

import (
	"github.com/spf13/pflag"
)

const (
	ProfilerListenFlag = "profiler-listen"
	ProfilerEnableFlag = "profiler-enable"
)

func AddFlags(flags *pflag.FlagSet) {
	flags.Bool(ProfilerEnableFlag, true, "Whether or not to enable pprof debug endpoints in service")
	flags.String(ProfilerListenFlag, ":9090", "Listen endpoint for pprof")
}
