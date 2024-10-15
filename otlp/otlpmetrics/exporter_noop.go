package otlpmetrics

import (
	"context"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type NoOpExporter struct{}

func (e *NoOpExporter) Temporality(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	return sdkmetric.DefaultTemporalitySelector(kind)
}

func (e *NoOpExporter) Aggregation(kind sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(kind)
}

func (e *NoOpExporter) ForceFlush(ctx context.Context) error {
	return nil
}

func (e *NoOpExporter) Shutdown(ctx context.Context) error {
	return nil
}

func (e *NoOpExporter) Export(ctx context.Context, data *metricdata.ResourceMetrics) error {
	return nil
}

func NewNoOpExporter() *NoOpExporter {
	return &NoOpExporter{}
}

var _ sdkmetric.Exporter = (*NoOpExporter)(nil)
