package observefx

import (
	otelpyroscope "github.com/grafana/otel-profiling-go"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v5/pkg/observe/pyroscopetraces"
)

// PyroscopeTracesModule decorates whichever trace.TracerProvider is already
// registered in the fx graph (see TracesModule) so that spans are tagged
// with a pyroscope.profile.id attribute, letting Grafana correlate a trace
// span with the Pyroscope profile samples captured while it executed.
//
// By default (allSpans: false) only each trace's local root span — the
// first span created locally, which may still have a remote parent from an
// incoming request — is tagged, matching otelpyroscope's own default and
// keeping the per-span overhead to a minimum. Pass allSpans: true to tag
// every span instead, at the cost of one extra locked, allocating
// span.SetAttributes call per span.
//
// It must be composed alongside TracesModule (or any other module providing
// trace.TracerProvider) in the same fx.New call; on its own it has no effect.
// Kept as its own module, rather than a flag on TracesModule, so that
// services that don't use Pyroscope don't need to reason about a
// Grafana-specific vendor library in the generic tracing bootstrap.
func PyroscopeTracesModule(enable bool, allSpans bool) fx.Option {
	if !enable {
		return fx.Options()
	}
	scope := otelpyroscope.ScopeRootSpan
	if allSpans {
		scope = otelpyroscope.ScopeAllSpans
	}
	return fx.Decorate(func(tp trace.TracerProvider) trace.TracerProvider {
		return otelpyroscope.NewTracerProvider(tp, otelpyroscope.WithSpanIDLabelScope(scope))
	})
}

func PyroscopeTracesModuleFromFlags(cmd *cobra.Command) fx.Option {
	enabled, _ := cmd.Flags().GetBool(pyroscopetraces.OtelTracesProviderPyroscopeEnabledFlag)
	allSpans, _ := cmd.Flags().GetBool(pyroscopetraces.OtelTracesProviderPyroscopeAllSpansFlag)
	return PyroscopeTracesModule(enabled, allSpans)
}
