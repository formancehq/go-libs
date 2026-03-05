package transportfx

import (
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v5/pkg/transport/grpcserver"
)

// GRPCFXHook converts a grpcserver.Hook into an fx.Hook for use with fx.Lifecycle.
func GRPCFXHook(h grpcserver.Hook) fx.Hook {
	return fx.Hook{OnStart: h.OnStart, OnStop: h.OnStop}
}
