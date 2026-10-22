package otelfx_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v5/pkg/fx/observefx"
	"github.com/formancehq/go-libs/v5/pkg/fx/otelfx"
	"github.com/formancehq/go-libs/v5/pkg/observe"
	"github.com/formancehq/go-libs/v5/pkg/observe/metrics"
	"github.com/formancehq/go-libs/v5/pkg/observe/traces"
)

func TestTelemetryDependencyBoundary(t *testing.T) {
	for _, tags := range []string{"", "pyroscope"} {
		t.Run("tags="+tags, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), "go", "list", "-mod=readonly", "-deps", "-tags=", "github.com/formancehq/go-libs/v5/pkg/fx/otelfx")
			if tags == "pyroscope" {
				cmd = exec.CommandContext(t.Context(), "go", "list", "-mod=readonly", "-deps", "-tags=pyroscope", "github.com/formancehq/go-libs/v5/pkg/fx/otelfx")
			}
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			for _, path := range strings.Fields(string(output)) {
				require.False(t, strings.HasPrefix(path, "github.com/grafana/"), "telemetry composition imports %s", path)
				require.NotEqual(t, "github.com/formancehq/go-libs/v5/pkg/observe/pyroscope", path)
			}
		})
	}
}

// Both APIs share option groups and provider types. Existing callers may pass
// legacy providers to a new module, or new providers to a legacy module.
func TestTelemetryModulesInteroperate(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "otelfx", true: "observefx"}[legacy], func(t *testing.T) {
			var mp *metric.MeterProvider
			var exporter *metrics.InMemoryExporter
			var tp trace.TracerProvider
			optionApplied := false
			resourceModule := otelfx.ResourceModule(observe.Config{ServiceName: "boundary-test"})
			metricsModule := otelfx.MetricsModule(metrics.ModuleConfig{KeepInMemory: true})
			tracesModule := otelfx.TracesModule(traces.ModuleConfig{})
			option := observefx.ProvideMetricsProviderOption(func() metric.Option {
				optionApplied = true
				return metric.WithView(func(metric.Instrument) (metric.Stream, bool) { return metric.Stream{}, false })
			})
			if legacy {
				resourceModule = observefx.ResourceModule(observe.Config{ServiceName: "boundary-test"})
				metricsModule = observefx.MetricsModule(metrics.ModuleConfig{KeepInMemory: true})
				tracesModule = observefx.TracesModule(traces.ModuleConfig{})
				option = otelfx.ProvideMetricsProviderOption(func() metric.Option {
					optionApplied = true
					return metric.WithView(func(metric.Instrument) (metric.Stream, bool) { return metric.Stream{}, false })
				})
			}
			app := fx.New(fx.NopLogger, resourceModule, metricsModule, tracesModule, option, fx.Populate(&mp, &exporter, &tp))
			require.NoError(t, app.Err())
			require.NoError(t, app.Start(t.Context()))
			require.True(t, optionApplied)
			counter, err := mp.Meter("boundary-test").Int64Counter("calls")
			require.NoError(t, err)
			counter.Add(t.Context(), 1)
			require.NoError(t, mp.ForceFlush(t.Context()))
			require.NotEmpty(t, exporter.GetMetrics().ScopeMetrics)
			require.NotNil(t, tp)
			require.NoError(t, app.Stop(t.Context()))
		})
	}
}
