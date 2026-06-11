package metrics

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
	"go.opentelemetry.io/otel/sdk/resource"
)

func makeResourceMetrics(value int64) metricdata.ResourceMetrics {
	now := time.Unix(1700000000, 0).UTC()
	attrs := attribute.NewSet(attribute.String("host", "test"))

	return metricdata.ResourceMetrics{
		Resource: resource.NewSchemaless(attribute.String("service.name", "test")),
		ScopeMetrics: []metricdata.ScopeMetrics{
			{
				Scope: instrumentation.Scope{Name: "test-scope"},
				Metrics: []metricdata.Metrics{
					{
						Name: "gauge",
						Data: metricdata.Gauge[int64]{
							DataPoints: []metricdata.DataPoint[int64]{
								{Attributes: attrs, Time: now, Value: value},
							},
						},
					},
					{
						Name: "sum",
						Data: metricdata.Sum[float64]{
							Temporality: metricdata.CumulativeTemporality,
							IsMonotonic: true,
							DataPoints: []metricdata.DataPoint[float64]{
								{
									Attributes: attrs,
									StartTime:  now,
									Time:       now,
									Value:      float64(value),
									Exemplars: []metricdata.Exemplar[float64]{
										{
											FilteredAttributes: []attribute.KeyValue{attribute.String("k", "v")},
											Time:               now,
											Value:              float64(value),
											SpanID:             []byte{1, 2, 3, 4, 5, 6, 7, 8},
											TraceID:            []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
										},
									},
								},
							},
						},
					},
					{
						Name: "histogram",
						Data: metricdata.Histogram[float64]{
							Temporality: metricdata.CumulativeTemporality,
							DataPoints: []metricdata.HistogramDataPoint[float64]{
								{
									Attributes:   attrs,
									StartTime:    now,
									Time:         now,
									Count:        uint64(value),
									Bounds:       []float64{1, 10, 100},
									BucketCounts: []uint64{uint64(value), 0, 0, 0},
									Min:          metricdata.NewExtrema(float64(value)),
									Max:          metricdata.NewExtrema(float64(value)),
									Sum:          float64(value),
								},
							},
						},
					},
					{
						Name: "exponential-histogram",
						Data: metricdata.ExponentialHistogram[float64]{
							Temporality: metricdata.CumulativeTemporality,
							DataPoints: []metricdata.ExponentialHistogramDataPoint[float64]{
								{
									Attributes:     attrs,
									StartTime:      now,
									Time:           now,
									Count:          uint64(value),
									Sum:            float64(value),
									Scale:          1,
									ZeroCount:      0,
									PositiveBucket: metricdata.ExponentialBucket{Offset: 0, Counts: []uint64{uint64(value)}},
									NegativeBucket: metricdata.ExponentialBucket{Offset: 0, Counts: []uint64{0}},
								},
							},
						},
					},
					{
						Name: "summary",
						Data: metricdata.Summary{
							DataPoints: []metricdata.SummaryDataPoint{
								{
									Attributes: attrs,
									StartTime:  now,
									Time:       now,
									Count:      uint64(value),
									Sum:        float64(value),
									QuantileValues: []metricdata.QuantileValue{
										{Quantile: 0.5, Value: float64(value)},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// mutateResourceMetrics rewrites every slice-backed value reachable from rm
// in place, mimicking the SDK reader recycling the struct for a new
// collection.
func mutateResourceMetrics(rm *metricdata.ResourceMetrics, value int64) {
	for i := range rm.ScopeMetrics {
		metrics := rm.ScopeMetrics[i].Metrics
		for j := range metrics {
			switch data := metrics[j].Data.(type) {
			case metricdata.Gauge[int64]:
				for k := range data.DataPoints {
					data.DataPoints[k].Value = value
				}
			case metricdata.Sum[float64]:
				for k := range data.DataPoints {
					data.DataPoints[k].Value = float64(value)
					for l := range data.DataPoints[k].Exemplars {
						data.DataPoints[k].Exemplars[l].Value = float64(value)
						data.DataPoints[k].Exemplars[l].FilteredAttributes[0] = attribute.Int64("k", value)
						data.DataPoints[k].Exemplars[l].SpanID[0] = byte(value)
						data.DataPoints[k].Exemplars[l].TraceID[0] = byte(value)
					}
				}
			case metricdata.Histogram[float64]:
				for k := range data.DataPoints {
					data.DataPoints[k].Count = uint64(value)
					data.DataPoints[k].Sum = float64(value)
					data.DataPoints[k].Bounds[0] = float64(value)
					data.DataPoints[k].BucketCounts[0] = uint64(value)
				}
			case metricdata.ExponentialHistogram[float64]:
				for k := range data.DataPoints {
					data.DataPoints[k].Count = uint64(value)
					data.DataPoints[k].Sum = float64(value)
					data.DataPoints[k].PositiveBucket.Counts[0] = uint64(value)
					data.DataPoints[k].NegativeBucket.Counts[0] = uint64(value)
				}
			case metricdata.Summary:
				for k := range data.DataPoints {
					data.DataPoints[k].Count = uint64(value)
					data.DataPoints[k].Sum = float64(value)
					data.DataPoints[k].QuantileValues[0].Value = float64(value)
				}
			}
		}
	}
}

func TestInMemoryExporterDeepCopiesExportedData(t *testing.T) {
	t.Parallel()

	exporter := NewInMemoryExporterDecorator(nil)

	exported := makeResourceMetrics(1)
	require.NoError(t, exporter.Export(context.Background(), &exported))

	// Mimic the SDK reader reusing the pooled struct for a new collection.
	mutateResourceMetrics(&exported, 42)

	got := exporter.GetMetrics()
	require.NotNil(t, got)
	require.NotSame(t, &exported, got)

	want := makeResourceMetrics(1)
	metricdatatest.AssertEqual(t, want, *got)
}

func TestInMemoryExporterConcurrentExportAndGetMetrics(t *testing.T) {
	t.Parallel()

	exporter := NewInMemoryExporterDecorator(nil)

	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: reuses the same struct across Export calls, like the SDK
	// PeriodicReader does with its sync.Pool.
	go func() {
		defer wg.Done()

		pooled := makeResourceMetrics(0)
		for i := range int64(iterations) {
			mutateResourceMetrics(&pooled, i)
			_ = exporter.Export(context.Background(), &pooled)
		}
	}()

	// Reader: deeply walks whatever GetMetrics returns. Run with -race, this
	// fails if the stored snapshot aliases memory still mutated by the writer.
	go func() {
		defer wg.Done()

		for range iterations {
			if metrics := exporter.GetMetrics(); metrics != nil {
				_, _ = json.Marshal(metrics)
			}
		}
	}()

	wg.Wait()
}
