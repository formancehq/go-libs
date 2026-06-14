package observefx_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
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
