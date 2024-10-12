package otlpmetrics

import (
	"context"
	"encoding/json"
	"net/http"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/fx"
)

type InMemoryExporter struct {
	metrics *metricdata.ResourceMetrics
}

func (e *InMemoryExporter) Temporality(k sdkmetric.InstrumentKind) metricdata.Temporality {
	return sdkmetric.DefaultTemporalitySelector(k)
}

func (e *InMemoryExporter) Aggregation(k sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(k)
}

func (e *InMemoryExporter) Export(ctx context.Context, data *metricdata.ResourceMetrics) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// notes(gfyrag): copy as indicate by interface sdkmetric.Exporter
	// I don't know if that's enough...
	e.metrics = &(*data)

	return nil
}

func (e *InMemoryExporter) ForceFlush(context.Context) error {
	// exporter holds no state, nothing to flush.
	return nil
}

func (e *InMemoryExporter) Shutdown(context.Context) error {
	return nil
}

func (e *InMemoryExporter) GetMetrics() *metricdata.ResourceMetrics {
	return e.metrics
}

func NewInMemoryExporter() *InMemoryExporter {
	return &InMemoryExporter{}
}

var _ sdkmetric.Exporter = (*InMemoryExporter)(nil)

func InMemoryMetricsModule() fx.Option {
	return fx.Options(
		fx.Provide(NewInMemoryExporter),
		fx.Provide(
			fx.Annotate(func(exporter *InMemoryExporter) *InMemoryExporter {
				return exporter
			}, fx.As(new(sdkmetric.Exporter))),
		),
	)
}

func NewInMemoryExporterHandler(meterProvider *sdkmetric.MeterProvider, e *InMemoryExporter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := meterProvider.ForceFlush(r.Context()); err != nil {
			panic(err)
		}

		_ = json.NewEncoder(w).Encode(e.GetMetrics())
	}
}
