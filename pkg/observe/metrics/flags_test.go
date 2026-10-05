package metrics_test

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/observe/metrics"
)

func TestConfigFromFlagsNamingDefaultsToIdentity(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	metrics.AddFlags(flags)

	cfg := metrics.ConfigFromFlags(flags)
	require.Equal(t, metrics.NamingOTel, cfg.Naming)
	require.Empty(t, cfg.Prefix)
}

func TestConfigFromFlagsNaming(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	metrics.AddFlags(flags)

	require.NoError(t, flags.Set(metrics.OtelMetricsNamingFlag, "prom"))
	require.NoError(t, flags.Set(metrics.OtelMetricsPrefixFlag, "acme.svc"))

	cfg := metrics.ConfigFromFlags(flags)
	require.Equal(t, metrics.NamingProm, cfg.Naming)
	require.Equal(t, "acme.svc", cfg.Prefix)
}
