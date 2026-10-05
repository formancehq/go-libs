package metrics_test

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/observe/metrics"
	"github.com/formancehq/go-libs/v5/pkg/service"
)

func TestConfigFromFlagsNamingDefaultsToIdentity(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	metrics.AddFlags(flags)

	cfg := metrics.ConfigFromFlags(flags)
	require.Equal(t, metrics.NamingOTel, cfg.Naming)
	require.Empty(t, cfg.Prefix)
}

func TestAddFlagsDefaultPrefix(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	metrics.AddFlags(flags, metrics.WithDefaultPrefix("formance.ledger"))

	require.Equal(t, "formance.ledger", flags.Lookup(metrics.OtelMetricsPrefixFlag).DefValue)
	require.Equal(t, "formance.ledger", metrics.ConfigFromFlags(flags).Prefix)
}

// TestDefaultPrefixPrecedence pins that a service default only applies when
// neither the environment nor the command line sets the prefix.
func TestDefaultPrefixPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  string
		args []string
		want string
	}{
		{name: "default", want: "formance.ledger"},
		{name: "env", env: "acme.payments", want: "acme.payments"},
		{name: "env none", env: metrics.NoPrefix, want: metrics.NoPrefix},
		{name: "cli over env", env: "acme.payments", args: []string{"--" + metrics.OtelMetricsPrefixFlag, "acme.cli"}, want: "acme.cli"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv("OTEL_METRICS_PREFIX", tc.env)
			}
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			metrics.AddFlags(flags, metrics.WithDefaultPrefix("formance.ledger"))

			require.NoError(t, service.BindEnvToFlagSetWithError(flags))
			require.NoError(t, flags.Parse(tc.args))

			prefix := metrics.ConfigFromFlags(flags).Prefix
			require.Equal(t, tc.want, prefix)
			_, err := metrics.ParsePrefix(prefix)
			require.NoError(t, err)
		})
	}
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
