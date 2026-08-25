package observefx_test

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/formancehq/go-libs/v5/pkg/fx/observefx"
	"github.com/formancehq/go-libs/v5/pkg/observe/pyroscopetraces"
)

func hasProfileIDAttribute(span sdktrace.ReadOnlySpan) bool {
	for _, attr := range span.Attributes() {
		if attr.Key == "pyroscope.profile.id" {
			return true
		}
	}
	return false
}

// This test exists specifically to guard against the bug the code review
// caught: the pyroscope decoration must reach every consumer of
// trace.TracerProvider, whether it gets it from the fx container (DI) or
// from the process-global otel package.
func TestPyroscopeTracesModuleDecoratesBothDIAndGlobalTracerProvider(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	base := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	var diTracerProvider trace.TracerProvider

	app := fxtest.New(t,
		fx.Provide(func() trace.TracerProvider { return base }),
		observefx.PyroscopeTracesModule(true, true),
		fx.Populate(&diTracerProvider),
		fx.Invoke(func(tp trace.TracerProvider) {
			// Mirrors what TracesModule's own fx.Invoke does: install the
			// (hopefully already-decorated) provider globally.
			otel.SetTracerProvider(tp)
		}),
		fx.NopLogger,
	)
	app.RequireStart()
	defer app.RequireStop()

	assertTaggedWithProfileID := func(t *testing.T, tp trace.TracerProvider, label string) {
		t.Helper()
		_, span := tp.Tracer("test").Start(context.Background(), "op")
		span.End()

		spans := recorder.Ended()
		require.NotEmpty(t, spans, "%s: expected at least one recorded span", label)
		require.True(t, hasProfileIDAttribute(spans[len(spans)-1]),
			"%s: span is missing the pyroscope.profile.id attribute added by otelpyroscope; consumer got the unwrapped TracerProvider", label)
	}

	assertTaggedWithProfileID(t, diTracerProvider, "DI-injected trace.TracerProvider")
	assertTaggedWithProfileID(t, otel.GetTracerProvider(), "global otel.GetTracerProvider()")
}

func TestPyroscopeTracesModuleDisabledLeavesProviderUntouched(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	base := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	var diTracerProvider trace.TracerProvider

	app := fxtest.New(t,
		fx.Provide(func() trace.TracerProvider { return base }),
		observefx.PyroscopeTracesModule(false, false),
		fx.Populate(&diTracerProvider),
		fx.NopLogger,
	)
	app.RequireStart()
	defer app.RequireStop()

	_, span := diTracerProvider.Tracer("test").Start(context.Background(), "op")
	span.End()

	spans := recorder.Ended()
	require.NotEmpty(t, spans)
	require.False(t, hasProfileIDAttribute(spans[len(spans)-1]), "flag disabled: span should not carry the pyroscope attribute")
}

// Covers the configurable scope added after the initial review: by default
// (allSpans: false) only the trace's root span should be tagged, matching
// otelpyroscope's own cheaper default; allSpans: true must tag children too.
func TestPyroscopeTracesModuleSpanScope(t *testing.T) {
	newRecordedChildSpan := func(t *testing.T, allSpans bool) (root, child sdktrace.ReadOnlySpan) {
		t.Helper()
		recorder := tracetest.NewSpanRecorder()
		base := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

		var tp trace.TracerProvider
		app := fxtest.New(t,
			fx.Provide(func() trace.TracerProvider { return base }),
			observefx.PyroscopeTracesModule(true, allSpans),
			fx.Populate(&tp),
			fx.NopLogger,
		)
		app.RequireStart()
		defer app.RequireStop()

		tracer := tp.Tracer("test")
		ctx, rootSpan := tracer.Start(context.Background(), "root")
		_, childSpan := tracer.Start(ctx, "child")
		childSpan.End()
		rootSpan.End()

		spans := recorder.Ended()
		require.Len(t, spans, 2)
		// Ended() records in end order: child ends before root.
		return spans[1], spans[0]
	}

	t.Run("root span only (default)", func(t *testing.T) {
		root, child := newRecordedChildSpan(t, false)
		require.True(t, hasProfileIDAttribute(root), "root span should be tagged even with allSpans=false")
		require.False(t, hasProfileIDAttribute(child), "child span should NOT be tagged when allSpans=false")
	})

	t.Run("all spans", func(t *testing.T) {
		root, child := newRecordedChildSpan(t, true)
		require.True(t, hasProfileIDAttribute(root), "root span should be tagged when allSpans=true")
		require.True(t, hasProfileIDAttribute(child), "child span should be tagged when allSpans=true")
	})
}

// Covers the actual entry point a real command uses: pyroscopetraces.AddFlags
// registering the flags, and PyroscopeTracesModuleFromFlags reading them back
// (previously untested — the tests above all called PyroscopeTracesModule
// directly with literal bools, never through the flag-parsing path).
func TestPyroscopeTracesModuleFromFlags(t *testing.T) {
	newCommand := func() *cobra.Command {
		cmd := &cobra.Command{Use: "test"}
		pyroscopetraces.AddFlags(cmd.Flags())
		return cmd
	}

	buildApp := func(t *testing.T, cmd *cobra.Command) (tp trace.TracerProvider, recorder *tracetest.SpanRecorder) {
		t.Helper()
		recorder = tracetest.NewSpanRecorder()
		base := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

		app := fxtest.New(t,
			fx.Provide(func() trace.TracerProvider { return base }),
			observefx.PyroscopeTracesModuleFromFlags(cmd),
			fx.Populate(&tp),
			fx.NopLogger,
		)
		app.RequireStart()
		t.Cleanup(app.RequireStop)
		return tp, recorder
	}

	t.Run("flags not set defaults to disabled", func(t *testing.T) {
		tp, recorder := buildApp(t, newCommand())

		_, span := tp.Tracer("test").Start(context.Background(), "op")
		span.End()

		spans := recorder.Ended()
		require.NotEmpty(t, spans)
		require.False(t, hasProfileIDAttribute(spans[len(spans)-1]), "flags left at their default (false) should not enable tagging")
	})

	t.Run("enabled flag turns on root-span tagging", func(t *testing.T) {
		cmd := newCommand()
		require.NoError(t, cmd.Flags().Set(pyroscopetraces.OtelTracesProviderPyroscopeEnabledFlag, "true"))

		tp, recorder := buildApp(t, cmd)

		_, span := tp.Tracer("test").Start(context.Background(), "op")
		span.End()

		spans := recorder.Ended()
		require.NotEmpty(t, spans)
		require.True(t, hasProfileIDAttribute(spans[len(spans)-1]), "enabled flag should turn on pyroscope.profile.id tagging")
	})

	t.Run("all-spans flag reaches child spans", func(t *testing.T) {
		cmd := newCommand()
		require.NoError(t, cmd.Flags().Set(pyroscopetraces.OtelTracesProviderPyroscopeEnabledFlag, "true"))
		require.NoError(t, cmd.Flags().Set(pyroscopetraces.OtelTracesProviderPyroscopeAllSpansFlag, "true"))

		tp, recorder := buildApp(t, cmd)

		ctx, rootSpan := tp.Tracer("test").Start(context.Background(), "root")
		_, childSpan := tp.Tracer("test").Start(ctx, "child")
		childSpan.End()
		rootSpan.End()

		spans := recorder.Ended()
		require.Len(t, spans, 2)
		require.True(t, hasProfileIDAttribute(spans[0]), "child span should be tagged when the all-spans flag is set")
		require.True(t, hasProfileIDAttribute(spans[1]), "root span should be tagged when the all-spans flag is set")
	})
}
