package metrics

import (
	"time"

	flag "github.com/spf13/pflag"

	"github.com/formancehq/go-libs/v5/pkg/observe"
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
	observe.AddFlags(flags)

	flags.Duration(OtelMetricsExporterPushIntervalFlag, 10*time.Second, "OpenTelemetry metrics exporter push interval")
	flags.Bool(OtelMetricsRuntimeFlag, false, "Enable OpenTelemetry runtime metrics")
	flags.Duration(OtelMetricsRuntimeMinimumReadMemStatsIntervalFlag, 15*time.Second, "OpenTelemetry runtime metrics minimum read mem stats interval")
	flags.String(OtelMetricsExporterFlag, "", "OpenTelemetry metrics exporter")
	flags.String(OtelMetricsExporterOTLPModeFlag, "grpc", "OpenTelemetry metrics OTLP exporter mode (grpc|http)")
	flags.String(OtelMetricsExporterOTLPEndpointFlag, "", "OpenTelemetry metrics grpc endpoint")
	flags.Bool(OtelMetricsExporterOTLPInsecureFlag, false, "OpenTelemetry metrics grpc insecure")
	flags.Bool(OtelMetricsKeepInMemoryFlag, false, "Allow to keep metrics in memory")
}

func ConfigFromFlags(flags *flag.FlagSet) ModuleConfig {
	exporter, _ := flags.GetString(OtelMetricsExporterFlag)
	runtimeMetrics, _ := flags.GetBool(OtelMetricsRuntimeFlag)
	minReadMemStats, _ := flags.GetDuration(OtelMetricsRuntimeMinimumReadMemStatsIntervalFlag)
	pushInterval, _ := flags.GetDuration(OtelMetricsExporterPushIntervalFlag)
	keepInMemory, _ := flags.GetBool(OtelMetricsKeepInMemoryFlag)
	otlpMode, _ := flags.GetString(OtelMetricsExporterOTLPModeFlag)
	otlpEndpoint, _ := flags.GetString(OtelMetricsExporterOTLPEndpointFlag)
	otlpInsecure, _ := flags.GetBool(OtelMetricsExporterOTLPInsecureFlag)

	return ModuleConfig{
		Exporter: exporter,
		OTLPConfig: &OTLPConfig{
			Mode:     otlpMode,
			Endpoint: otlpEndpoint,
			Insecure: otlpInsecure,
		},
		RuntimeMetrics:              runtimeMetrics,
		MinimumReadMemStatsInterval: minReadMemStats,
		PushInterval:                pushInterval,
		KeepInMemory:                keepInMemory,
	}
}
