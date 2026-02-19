package circuitbreaker

import (
	"context"
	"time"

	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v4/logging"
	"github.com/formancehq/go-libs/v4/publish/circuit_breaker/storage"
	topicmapper "github.com/formancehq/go-libs/v4/publish/topic_mapper"
)

func Module(schema string, openIntervalDuration time.Duration, storageLimit int, debug bool) fx.Option {
	options := make([]fx.Option, 0)

	options = append(options,
		fx.Provide(func(
			logger logging.Logger,
			topicMapper *topicmapper.TopicMapperPublisherDecorator,
			store storage.Store,
			lc fx.Lifecycle,
		) *CircuitBreaker {
			cb := newCircuitBreaker(logger, topicMapper, store, openIntervalDuration)

			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					go cb.loop(context.WithoutCancel(ctx))
					return nil
				},
				OnStop: func(ctx context.Context) error {
					return cb.Close()
				},
			})
			return cb
		}),
	)

	options = append(options, storage.Module(schema, storageLimit, debug))

	return fx.Options(options...)
}
