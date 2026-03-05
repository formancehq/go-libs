package storagefx

import (
	"context"

	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/storage/bun/connect"
	"github.com/formancehq/go-libs/v5/pkg/storage/bun/debug"
)

func BunConnectModule(connectionOptions connect.ConnectionOptions, debugMode bool) fx.Option {
	return fx.Options(
		fx.Provide(func(logger log.Logger) (*bun.DB, error) {
			hooks := make([]bun.QueryHook, 0)
			debugHook := debug.NewQueryHook()
			debugHook.Debug = debugMode
			hooks = append(hooks, debugHook)

			logger.
				WithFields(map[string]any{
					"max-idle-conns":         connectionOptions.MaxIdleConns,
					"max-open-conns":         connectionOptions.MaxOpenConns,
					"max-conn-max-idle-time": connectionOptions.ConnMaxIdleTime,
					"conn-max-lifetime":      connectionOptions.ConnMaxLifetime,
				}).
				Infof("opening database connection")

			return connect.OpenSQLDB(log.ContextWithLogger(context.Background(), logger), connectionOptions, hooks...)
		}),
		fx.Invoke(func(lc fx.Lifecycle, db *bun.DB, logger log.Logger) {
			lc.Append(fx.Hook{
				OnStop: func(ctx context.Context) error {
					logger.Info("closing database connection...")
					return db.Close()
				},
			})
		}),
	)
}
