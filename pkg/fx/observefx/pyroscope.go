//go:build pyroscope

package observefx

import (
	"go.uber.org/fx"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/observe/pyroscope"
)

// PyroscopeModule starts native profiling only when Enabled. Trace correlation
// follows the same gate so profile IDs cannot point to a disabled profiler.
func PyroscopeModule(cfg pyroscope.Config) fx.Option {
	if !cfg.Enabled {
		return fx.Options()
	}
	return fx.Options(
		fx.Invoke(func(lc fx.Lifecycle, logger logging.Logger) {
			cfg.SetupRuntimeProfiling()
			hook := pyroscope.NewHook(cfg, logger)
			lc.Append(fx.Hook{OnStart: hook.OnStart, OnStop: hook.OnStop})
		}),
		PyroscopeTracesModule(cfg.TagSpans, cfg.TagAllSpans),
	)
}
