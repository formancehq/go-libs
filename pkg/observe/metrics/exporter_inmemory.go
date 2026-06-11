package metrics

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type InMemoryExporter struct {
	exp sdkmetric.Exporter

	mu      sync.RWMutex
	metrics *metricdata.ResourceMetrics
}

func (e *InMemoryExporter) Temporality(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	if e.exp != nil {
		return e.exp.Temporality(kind)
	}

	return sdkmetric.DefaultTemporalitySelector(kind)
}

func (e *InMemoryExporter) Aggregation(kind sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	if e.exp != nil {
		return e.exp.Aggregation(kind)
	}

	return sdkmetric.DefaultAggregationSelector(kind)
}

func (e *InMemoryExporter) ForceFlush(ctx context.Context) error {
	if e.exp != nil {
		return e.exp.ForceFlush(ctx)
	}

	return nil
}

func (e *InMemoryExporter) Shutdown(ctx context.Context) error {
	if e.exp != nil {
		return e.exp.Shutdown(ctx)
	}

	return nil
}

func (e *InMemoryExporter) Export(ctx context.Context, data *metricdata.ResourceMetrics) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// The SDK reader owns *data and recycles it (via a sync.Pool) once Export
	// returns, so we must keep a deep copy of it.
	snapshot := copyResourceMetrics(data)

	e.mu.Lock()
	e.metrics = snapshot
	e.mu.Unlock()

	if e.exp != nil {
		return e.exp.Export(ctx, data)
	}

	return nil
}

func (e *InMemoryExporter) GetMetrics() *metricdata.ResourceMetrics {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.metrics
}

func NewInMemoryExporterDecorator(exp sdkmetric.Exporter) *InMemoryExporter {
	return &InMemoryExporter{
		exp: exp,
	}
}

var _ sdkmetric.Exporter = (*InMemoryExporter)(nil)

func NewInMemoryExporterHandler(meterProvider *sdkmetric.MeterProvider, e *InMemoryExporter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := meterProvider.ForceFlush(r.Context()); err != nil {
			panic(err)
		}

		_ = json.NewEncoder(w).Encode(e.GetMetrics())
	}
}
