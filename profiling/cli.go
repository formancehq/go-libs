package profiling

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.uber.org/fx"
)

const (
	ProfilerListenFlag = "profiler-listen"
	ProfilerEnableFlag = "profiler-enable"
)

func AddFlags(flags *pflag.FlagSet) {
	flags.Bool(ProfilerEnableFlag, true, "Whether or not to enable pprof debug endpoints in service")
	flags.String(ProfilerListenFlag, ":9090", "Listen endpoint for pprof")
}

func FXModuleFromFlags(cmd *cobra.Command) fx.Option {
	enabled, _ := cmd.Flags().GetBool(ProfilerEnableFlag)
	port, _ := cmd.Flags().GetString(ProfilerListenFlag)
	return NewModule(port, enabled)
}
