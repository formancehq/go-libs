package sharedotlpmetrics

import (
	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.uber.org/fx"
)

const (
	OtelMetricsFlag                     = "otel-metrics"
	OtelMetricsExporterFlag             = "otel-metrics-exporter"
	OtelMetricsExporterOTLPModeFlag     = "otel-metrics-exporter-otlp-mode"
	OtelMetricsExporterOTLPEndpointFlag = "otel-metrics-exporter-otlp-endpoint"
	OtelMetricsExporterOTLPInsecureFlag = "otel-metrics-exporter-otlp-insecure"
)

func InitOTLPMetricsFlags(flags *flag.FlagSet) {
	flags.Bool(OtelMetricsFlag, false, "Enable OpenTelemetry metrics support")
	flags.String(OtelMetricsExporterFlag, "stdout", "OpenTelemetry metrics exporter")
	flags.String(OtelMetricsExporterOTLPModeFlag, "grpc", "OpenTelemetry metrics OTLP exporter mode (grpc|http)")
	flags.String(OtelMetricsExporterOTLPEndpointFlag, "", "OpenTelemetry metrics grpc endpoint")
	flags.Bool(OtelMetricsExporterOTLPInsecureFlag, false, "OpenTelemetry metrics grpc insecure")
}

func CLIMetricsModule(v *viper.Viper) fx.Option {
	if v.GetBool(OtelMetricsFlag) {
		return MetricsModule(MetricsModuleConfig{
			Exporter: v.GetString(OtelMetricsExporterFlag),
			OTLPConfig: func() *OTLPMetricsConfig {
				if v.GetString(OtelMetricsExporterFlag) != OTLPMetricsExporter {
					return nil
				}
				return &OTLPMetricsConfig{
					Mode:     v.GetString(OtelMetricsExporterOTLPModeFlag),
					Endpoint: v.GetString(OtelMetricsExporterOTLPEndpointFlag),
					Insecure: v.GetBool(OtelMetricsExporterOTLPInsecureFlag),
				}
			}(),
		})
	}
	return nil
}
