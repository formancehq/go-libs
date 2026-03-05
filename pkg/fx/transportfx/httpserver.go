package transportfx

import (
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v5/pkg/transport/httpserver"
)

// FXHook converts an httpserver.Hook into an fx.Hook for use with fx.Lifecycle.
func FXHook(h httpserver.Hook) fx.Hook {
	return fx.Hook{OnStart: h.OnStart, OnStop: h.OnStop}
}
