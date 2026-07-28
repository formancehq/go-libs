package observefx_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/formancehq/go-libs/v5/pkg/fx/observefx"
	"github.com/formancehq/go-libs/v5/pkg/observe"
	"github.com/formancehq/go-libs/v5/pkg/observe/metrics"
)

func TestMetricsModuleProvidesRuntimeOptionsToRuntimeMetricsInvoke(t *testing.T) {
	var runtimeOptionProvided atomic.Bool

	app := fxtest.New(t,
		observefx.ResourceModule(observe.Config{
			ServiceName: "metrics-test",
		}),
		observefx.MetricsModule(metrics.ModuleConfig{
			MinimumReadMemStatsInterval: time.Second,
		}),
		observefx.ProvideRuntimeMetricsOption(func() runtime.Option {
			runtimeOptionProvided.Store(true)
			return runtime.WithMinimumReadMemStatsInterval(100 * time.Millisecond)
		}),
		fx.NopLogger,
	)
	app.RequireStart()
	defer app.RequireStop()

	require.True(t, runtimeOptionProvided.Load())
}

func TestMetricsModuleDoesNotProvideZeroRuntimeMetricsInterval(t *testing.T) {
	var runtimeOptionsCount int

	app := fxtest.New(t,
		observefx.ResourceModule(observe.Config{
			ServiceName: "metrics-test",
		}),
		observefx.MetricsModule(metrics.ModuleConfig{}),
		fx.Invoke(fx.Annotate(
			func(options ...runtime.Option) {
				runtimeOptionsCount = len(options)
			},
			fx.ParamTags(`group:"_metricsRuntimeOption"`),
		)),
		fx.NopLogger,
	)
	app.RequireStart()
	defer app.RequireStop()

	require.Zero(t, runtimeOptionsCount)
}

// histogramAggregationKind starts a MetricsModule with the given
// ClassicHistograms setting, records one observation on a histogram with the
// given unit, force-flushes through the in-memory exporter, and returns the
// concrete metricdata type the SDK actually produced.
func histogramAggregationKind(t *testing.T, classic bool, unit string) any {
	t.Helper()

	var (
		exporter      *metrics.InMemoryExporter
		meterProvider *sdkmetric.MeterProvider
	)

	app := fxtest.New(t,
		observefx.ResourceModule(observe.Config{ServiceName: "histogram-aggregation-test"}),
		observefx.MetricsModule(metrics.ModuleConfig{
			KeepInMemory:      true,
			ClassicHistograms: classic,
		}),
		fx.Populate(&exporter, &meterProvider),
		fx.NopLogger,
	)
	app.RequireStart()
	defer app.RequireStop()

	hist, err := otel.Meter("histogram-aggregation-test").Float64Histogram("test.histogram", otelmetric.WithUnit(unit))
	require.NoError(t, err)
	hist.Record(context.Background(), 1.5, otelmetric.WithAttributes())

	require.NoError(t, meterProvider.ForceFlush(context.Background()))

	rm := exporter.GetMetrics()
	require.NotNil(t, rm)

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "test.histogram" {
				return m.Data
			}
		}
	}

	t.Fatal("test.histogram not found in exported metrics")

	return nil
}

func TestMetricsModuleDefaultsToExponentialHistograms(t *testing.T) {
	_, ok := histogramAggregationKind(t, false, "s").(metricdata.ExponentialHistogram[float64])
	require.True(t, ok, "default (ClassicHistograms: false) must produce an exponential histogram")
}

func TestMetricsModuleClassicHistogramsUsesExplicitBuckets(t *testing.T) {
	_, ok := histogramAggregationKind(t, true, "1").(metricdata.Histogram[float64])
	require.True(t, ok, "ClassicHistograms: true must produce an explicit-bucket histogram")
}

// wantSecondsHistogramBoundaries mirrors observefx's unexported
// secondsHistogramBoundaries -- kept in sync by these tests failing if either
// side drifts from the other.
var wantSecondsHistogramBoundaries = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 120, 240,
}

func TestMetricsModuleClassicHistogramsUsesSecondsBoundariesForSecondsUnit(t *testing.T) {
	data := histogramAggregationKind(t, true, "s")
	hist, ok := data.(metricdata.Histogram[float64])
	require.True(t, ok, "ClassicHistograms: true must produce an explicit-bucket histogram")
	require.NotEmpty(t, hist.DataPoints)
	require.Equal(t, wantSecondsHistogramBoundaries, hist.DataPoints[0].Bounds)
}

func TestMetricsModuleClassicHistogramsLeavesNonSecondsUnitAtSDKDefault(t *testing.T) {
	data := histogramAggregationKind(t, true, "1")
	hist, ok := data.(metricdata.Histogram[float64])
	require.True(t, ok, "ClassicHistograms: true must produce an explicit-bucket histogram")
	require.NotEmpty(t, hist.DataPoints)
	require.NotEqual(t, wantSecondsHistogramBoundaries, hist.DataPoints[0].Bounds,
		"a non-seconds-unit histogram should keep the SDK's default boundaries, not the seconds-tuned ones")
}
