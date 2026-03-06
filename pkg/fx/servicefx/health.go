package servicefx

import (
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v5/pkg/service/health"
)

const HealthCheckKey = `group:"_healthCheck"`

func ProvideHealthCheck(provider any) fx.Option {
	return fx.Provide(
		fx.Annotate(provider, fx.ResultTags(HealthCheckKey)),
	)
}

func HealthModule() fx.Option {
	return fx.Provide(fx.Annotate(health.NewHealthController, fx.ParamTags(HealthCheckKey)))
}
