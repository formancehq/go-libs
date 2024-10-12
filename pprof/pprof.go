package pprof

import (
	"context"
	"net/http"

	otelpyroscope "github.com/grafana/otel-profiling-go"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
)

func NewFXModule() fx.Option {
	return fx.Options(
		fx.Decorate(func(provider trace.TracerProvider) trace.TracerProvider {
			return otelpyroscope.NewTracerProvider(provider)
		}),
		fx.Invoke(func(lc fx.Lifecycle) {
			// todo: attach on application routers
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					go func() {
						if err := http.ListenAndServe(":3000", nil); err != nil && !errors.Is(err, http.ErrServerClosed) {
							panic(err)
						}
					}()
					return nil
				},
			})
		}),
	)
}
