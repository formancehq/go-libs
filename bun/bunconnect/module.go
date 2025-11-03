package bunconnect

import (
	"context"

	"github.com/formancehq/go-libs/v3/bun/bundebug"

	"github.com/formancehq/go-libs/v3/logging"
	"github.com/uptrace/bun"
	"go.uber.org/fx"
)

func Module(connectionOptions ConnectionOptions, debug bool) fx.Option {
	return fx.Options(
		fx.Provide(func(logger logging.Logger) (*bun.DB, error) {
			hooks := make([]bun.QueryHook, 0)
			debugHook := bundebug.NewQueryHook()
			debugHook.Debug = debug
			hooks = append(hooks, debugHook)

			logger.
				WithFields(map[string]any{
					"max-idle-conns":         connectionOptions.MaxIdleConns,
					"max-open-conns":         connectionOptions.MaxOpenConns,
					"max-conn-max-idle-time": connectionOptions.ConnMaxIdleTime,
					"conn-max-lifetime":      connectionOptions.ConnMaxLifetime,
				}).
				Infof("opening database connection")

			return OpenSQLDB(logging.ContextWithLogger(context.Background(), logger), connectionOptions, hooks...)
		}),
		fx.Invoke(func(lc fx.Lifecycle, db *bun.DB, logger logging.Logger) {
			lc.Append(fx.Hook{
				OnStop: func(ctx context.Context) error {
					logger.Info("closing database connection...")
					return db.Close()
				},
			})
		}),
	)
}
