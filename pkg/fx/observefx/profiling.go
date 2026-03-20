package observefx

import (
	"github.com/go-chi/chi/v5"
	"github.com/spf13/cobra"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v5/pkg/observe/profiling"
	"github.com/formancehq/go-libs/v5/pkg/transport/httpserver"
)

func ProfilingModule(bindPort string, enable bool) fx.Option {
	if !enable {
		return fx.Options()
	}
	return fx.Options(
		fx.Invoke(fx.Annotate(func(m *chi.Mux, lc fx.Lifecycle) {
			h := httpserver.NewHook(m, httpserver.WithAddress(bindPort))
			lc.Append(fx.Hook{OnStart: h.OnStart, OnStop: h.OnStop})
		}, fx.ParamTags(`name:"profilingRouter"`, ``))),
		fx.Provide(fx.Annotate(profiling.NewRouter, fx.ResultTags(`name:"profilingRouter"`))),
	)
}

func ProfilingModuleFromFlags(cmd *cobra.Command) fx.Option {
	enabled, _ := cmd.Flags().GetBool(profiling.ProfilerEnableFlag)
	port, _ := cmd.Flags().GetString(profiling.ProfilerListenFlag)
	return ProfilingModule(port, enabled)
}
