package otlpmetrics

import (
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestAddFlags(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)

	AddFlags(flags)

	// Vérifier que les drapeaux sont définis avec les valeurs par défaut
	pushInterval, err := flags.GetDuration(OtelMetricsExporterPushIntervalFlag)
	require.NoError(t, err)
	require.Equal(t, 10*time.Second, pushInterval, "L'intervalle de push par défaut devrait être de 10 secondes")

	runtime, err := flags.GetBool(OtelMetricsRuntimeFlag)
	require.NoError(t, err)
	require.False(t, runtime, "Les métriques runtime devraient être désactivées par défaut")

	runtimeInterval, err := flags.GetDuration(OtelMetricsRuntimeMinimumReadMemStatsIntervalFlag)
	require.NoError(t, err)
	require.Equal(t, 15*time.Second, runtimeInterval, "L'intervalle de lecture des stats mémoire devrait être de 15 secondes")

	exporter, err := flags.GetString(OtelMetricsExporterFlag)
	require.NoError(t, err)
	require.Equal(t, "", exporter, "L'exportateur par défaut devrait être vide")

	otlpMode, err := flags.GetString(OtelMetricsExporterOTLPModeFlag)
	require.NoError(t, err)
	require.Equal(t, "grpc", otlpMode, "Le mode OTLP par défaut devrait être grpc")

	otlpEndpoint, err := flags.GetString(OtelMetricsExporterOTLPEndpointFlag)
	require.NoError(t, err)
	require.Equal(t, "", otlpEndpoint, "L'endpoint OTLP par défaut devrait être vide")

	otlpInsecure, err := flags.GetBool(OtelMetricsExporterOTLPInsecureFlag)
	require.NoError(t, err)
	require.False(t, otlpInsecure, "Le mode insecure OTLP devrait être désactivé par défaut")

	keepInMemory, err := flags.GetBool(OtelMetricsKeepInMemoryFlag)
	require.NoError(t, err)
	require.False(t, keepInMemory, "Le mode de conservation en mémoire devrait être désactivé par défaut")
}

func TestFXModuleFromFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Duration(OtelMetricsExporterPushIntervalFlag, 10*time.Second, "")
	cmd.Flags().Bool(OtelMetricsRuntimeFlag, false, "")
	cmd.Flags().Duration(OtelMetricsRuntimeMinimumReadMemStatsIntervalFlag, 15*time.Second, "")
	cmd.Flags().String(OtelMetricsExporterFlag, "", "")
	cmd.Flags().String(OtelMetricsExporterOTLPModeFlag, "grpc", "")
	cmd.Flags().String(OtelMetricsExporterOTLPEndpointFlag, "", "")
	cmd.Flags().Bool(OtelMetricsExporterOTLPInsecureFlag, false, "")
	cmd.Flags().Bool(OtelMetricsKeepInMemoryFlag, false, "")

	// Test avec les valeurs par défaut
	module := FXModuleFromFlags(cmd)
	require.NotNil(t, module, "Le module ne devrait pas être nil")

	// Test avec des valeurs personnalisées
	cmd.Flags().Set(OtelMetricsExporterFlag, "otlp")
	cmd.Flags().Set(OtelMetricsExporterOTLPModeFlag, "http")
	cmd.Flags().Set(OtelMetricsExporterOTLPEndpointFlag, "localhost:4317")
	cmd.Flags().Set(OtelMetricsExporterOTLPInsecureFlag, "true")
	cmd.Flags().Set(OtelMetricsRuntimeFlag, "true")
	cmd.Flags().Set(OtelMetricsRuntimeMinimumReadMemStatsIntervalFlag, "30s")
	cmd.Flags().Set(OtelMetricsExporterPushIntervalFlag, "20s")
	cmd.Flags().Set(OtelMetricsKeepInMemoryFlag, "true")

	module = FXModuleFromFlags(cmd)
	require.NotNil(t, module, "Le module ne devrait pas être nil")
}
