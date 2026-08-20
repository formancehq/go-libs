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

// TestMetricsModuleUsesExponentialHistograms pins that every histogram
// instrument gets Base2ExponentialHistogram aggregation unconditionally --
// there is no backend-specific opt-out, matching every other package in this
// module (none of them special-case aggregation for a particular exporter).
func TestMetricsModuleUsesExponentialHistograms(t *testing.T) {
	var (
		exporter      *metrics.InMemoryExporter
		meterProvider *sdkmetric.MeterProvider
	)

	app := fxtest.New(t,
		observefx.ResourceModule(observe.Config{ServiceName: "histogram-aggregation-test"}),
		observefx.MetricsModule(metrics.ModuleConfig{KeepInMemory: true}),
		fx.Populate(&exporter, &meterProvider),
		fx.NopLogger,
	)
	app.RequireStart()
	defer app.RequireStop()

	hist, err := otel.Meter("histogram-aggregation-test").Float64Histogram("test.histogram", otelmetric.WithUnit("s"))
	require.NoError(t, err)
	hist.Record(context.Background(), 1.5, otelmetric.WithAttributes())

	require.NoError(t, meterProvider.ForceFlush(context.Background()))

	rm := exporter.GetMetrics()
	require.NotNil(t, rm)

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "test.histogram" {
				_, ok := m.Data.(metricdata.ExponentialHistogram[float64])
				require.True(t, ok, "every histogram must use exponential aggregation")
				return
			}
		}
	}

	t.Fatal("test.histogram not found in exported metrics")
}
