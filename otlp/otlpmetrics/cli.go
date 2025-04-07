package otlpmetrics

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/formancehq/go-libs/v3/otlp"
	flag "github.com/spf13/pflag"
	"go.uber.org/fx"
)

const (
	OtelMetricsExporterPushIntervalFlag               = "otel-metrics-exporter-push-interval"
	OtelMetricsRuntimeFlag                            = "otel-metrics-runtime"
	OtelMetricsRuntimeMinimumReadMemStatsIntervalFlag = "otel-metrics-runtime-minimum-read-mem-stats-interval"
	OtelMetricsExporterFlag                           = "otel-metrics-exporter"
	OtelMetricsKeepInMemoryFlag                       = "otel-metrics-keep-in-memory"
	OtelMetricsExporterOTLPModeFlag                   = "otel-metrics-exporter-otlp-mode"
	OtelMetricsExporterOTLPEndpointFlag               = "otel-metrics-exporter-otlp-endpoint"
	OtelMetricsExporterOTLPInsecureFlag               = "otel-metrics-exporter-otlp-insecure"
)

func AddFlags(flags *flag.FlagSet) {
	otlp.AddFlags(flags)

	flags.Duration(OtelMetricsExporterPushIntervalFlag, 10*time.Second, "OpenTelemetry metrics exporter push interval")
	flags.Bool(OtelMetricsRuntimeFlag, false, "Enable OpenTelemetry runtime metrics")
	flags.Duration(OtelMetricsRuntimeMinimumReadMemStatsIntervalFlag, 15*time.Second, "OpenTelemetry runtime metrics minimum read mem stats interval")
	flags.String(OtelMetricsExporterFlag, "", "OpenTelemetry metrics exporter")
	flags.String(OtelMetricsExporterOTLPModeFlag, "grpc", "OpenTelemetry metrics OTLP exporter mode (grpc|http)")
	flags.String(OtelMetricsExporterOTLPEndpointFlag, "", "OpenTelemetry metrics grpc endpoint")
	flags.Bool(OtelMetricsExporterOTLPInsecureFlag, false, "OpenTelemetry metrics grpc insecure")

	// notes(gfyrag): apps are in charge of exposing in memory metrics using whatever protocol it wants to
	flags.Bool(OtelMetricsKeepInMemoryFlag, false, "Allow to keep metrics in memory")
}

func FXModuleFromFlags(cmd *cobra.Command) fx.Option {
	otelMetricsExporterOTLPEndpoint, _ := cmd.Flags().GetString(OtelMetricsExporterOTLPEndpointFlag)
	otelMetricsExporterOTLPMode, _ := cmd.Flags().GetString(OtelMetricsExporterOTLPModeFlag)
	otelMetricsExporterOTLPInsecure, _ := cmd.Flags().GetBool(OtelMetricsExporterOTLPInsecureFlag)
	otelMetricsExporter, _ := cmd.Flags().GetString(OtelMetricsExporterFlag)
	otelMetricsRuntime, _ := cmd.Flags().GetBool(OtelMetricsRuntimeFlag)
	otelMetricsRuntimeMinimumReadMemStatsInterval, _ := cmd.Flags().GetDuration(OtelMetricsRuntimeMinimumReadMemStatsIntervalFlag)
	otelMetricsExporterPushInterval, _ := cmd.Flags().GetDuration(OtelMetricsExporterPushIntervalFlag)
	otelMetricsKeepInMemory, _ := cmd.Flags().GetBool(OtelMetricsKeepInMemoryFlag)

	return MetricsModule(ModuleConfig{
		OTLPConfig: &OTLPConfig{
			Mode:     otelMetricsExporterOTLPMode,
			Endpoint: otelMetricsExporterOTLPEndpoint,
			Insecure: otelMetricsExporterOTLPInsecure,
		},
		Exporter:                    otelMetricsExporter,
		RuntimeMetrics:              otelMetricsRuntime,
		MinimumReadMemStatsInterval: otelMetricsRuntimeMinimumReadMemStatsInterval,
		PushInterval:                otelMetricsExporterPushInterval,
		KeepInMemory:                otelMetricsKeepInMemory,
	})
}
