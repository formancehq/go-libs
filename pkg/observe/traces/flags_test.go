package traces_test

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/observe/traces"
)

func TestAddFlags(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	traces.AddFlags(flags)

	// Verify flags are registered
	f := flags.Lookup(traces.OtelTracesExporterFlag)
	require.NotNil(t, f)
	require.Equal(t, "", f.DefValue)

	f = flags.Lookup(traces.OtelTracesBatchFlag)
	require.NotNil(t, f)
}

func TestConfigFromFlags(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	traces.AddFlags(flags)

	require.NoError(t, flags.Set(traces.OtelTracesExporterFlag, "otlp"))
	require.NoError(t, flags.Set(traces.OtelTracesBatchFlag, "true"))

	cfg := traces.ConfigFromFlags(flags)
	require.Equal(t, "otlp", cfg.Exporter)
	require.True(t, cfg.Batch)
}
