//go:build pyroscope

package observefx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/grafana/pyroscope-go"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v5/pkg/fx/observefx"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/observe/pyroscope"
)

func TestNativeProfilingModuleGatesTraceCorrelation(t *testing.T) {
	for _, tc := range []struct {
		name                                             string
		enabled, tagSpans, allSpans, wantRoot, wantChild bool
	}{
		{"disabled", false, true, true, false, false},
		{"no-tagging", true, false, true, false, false},
		{"root", true, true, false, true, false},
		{"all", true, true, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			base := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { require.NoError(t, base.Shutdown(context.WithoutCancel(t.Context()))) })
			var tp trace.TracerProvider
			cfg := pyroscope.DefaultConfig()
			cfg.Enabled = tc.enabled
			cfg.TagSpans = tc.tagSpans
			cfg.TagAllSpans = tc.allSpans
			// Avoid changing process-wide sampling in a graph-only correlation test.
			cfg.MutexProfileFraction = 0
			cfg.BlockProfileRate = 0
			app := fx.New(fx.NopLogger, fx.Provide(func() trace.TracerProvider { return base }),
				fx.Supply(fx.Annotate(logging.NopZap(), fx.As(new(logging.Logger)))),
				observefx.PyroscopeModule(cfg), fx.Populate(&tp))
			require.NoError(t, app.Err())
			ctx, root := tp.Tracer("test").Start(t.Context(), "root")
			_, child := tp.Tracer("test").Start(ctx, "child")
			child.End()
			root.End()
			ended := recorder.Ended()
			require.Len(t, ended, 2)
			require.Equal(t, tc.wantChild, hasProfileIDAttribute(ended[0]))
			require.Equal(t, tc.wantRoot, hasProfileIDAttribute(ended[1]))
		})
	}
}

func TestNativeProfilingModuleDisabledNeedsNoLogger(t *testing.T) {
	old := runtime.SetMutexProfileFraction(-1)
	app := fx.New(fx.NopLogger, observefx.PyroscopeModule(pyroscope.DefaultConfig()))
	require.NoError(t, app.Err())
	require.Equal(t, old, runtime.SetMutexProfileFraction(-1))
	require.NoError(t, app.Start(t.Context()))
	require.NoError(t, app.Stop(t.Context()))
}

func TestNativeProfilingModuleStartsAndStopsSDK(t *testing.T) {
	var uploads atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { uploads.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	t.Setenv("PYROSCOPE_ADHOC_SERVER_ADDRESS", server.URL)
	cfg := pyroscope.DefaultConfig()
	cfg.Enabled = true
	cfg.ApplicationName = "test-service"
	cfg.ServerAddress = server.URL
	cfg.ProfileTypes = []sdk.ProfileType{sdk.ProfileGoroutines}
	cfg.UploadRate = 100 * time.Millisecond
	cfg.DisableGCRuns = true
	cfg.MutexProfileFraction = 0
	cfg.BlockProfileRate = 0
	app := fx.New(fx.NopLogger, fx.Supply(fx.Annotate(logging.NopZap(), fx.As(new(logging.Logger)))), observefx.PyroscopeModule(cfg))
	require.NoError(t, app.Err())
	require.NoError(t, app.Start(t.Context()))
	defer func() { require.NoError(t, app.Stop(context.WithoutCancel(t.Context()))) }()
	require.Eventually(t, func() bool { return uploads.Load() > 0 }, 3*time.Second, 10*time.Millisecond)
}
