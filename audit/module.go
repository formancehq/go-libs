package audit

import (
	"context"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

// ModuleConfig holds the configuration for the audit module
type ModuleConfig struct {
	Config Config
}

// Module creates an Fx module for audit
func Module(cfg ModuleConfig) fx.Option {
	options := make([]fx.Option, 0)

	if cfg.Config.Enabled {
		options = append(options,
			fx.Supply(cfg.Config),
			fx.Provide(NewClient),
			fx.Invoke(func(lc fx.Lifecycle, client *Client) {
				lc.Append(fx.Hook{
					OnStop: func(ctx context.Context) error {
						return client.Close()
					},
				})
			}),
		)
	} else {
		// Provide a disabled client when audit is disabled
		options = append(options,
			fx.Provide(func(logger *zap.Logger) (*Client, error) {
				disabledCfg := cfg.Config
				disabledCfg.Enabled = false
				return NewClient(disabledCfg, logger)
			}),
		)
	}

	return fx.Module("audit", options...)
}
