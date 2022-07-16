package sharedotlpmetrics

import (
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/nonrecording"
	"go.uber.org/fx"
)

func LoadNoOpMeterProvider() metric.MeterProvider {
	return nonrecording.NewNoopMeterProvider()
}

func NoOpMeterModule() fx.Option {
	return fx.Options(
		fx.Provide(LoadNoOpMeterProvider),
		metricsSdkExportModule(),
	)
}
