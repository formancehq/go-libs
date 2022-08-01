package sharedhealth

import "go.uber.org/fx"

const HealthCheckKey = `group:"_healthCheck"`

func ProvideHealthCheck(provider interface{}) fx.Option {
	return fx.Provide(
		fx.Annotate(provider, fx.ResultTags(HealthCheckKey)),
	)
}

func Module() fx.Option {
	return fx.Provide(fx.Annotate(NewHealthController, fx.ParamTags(HealthCheckKey)))
}
