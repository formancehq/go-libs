package traces

import (
	flag "github.com/spf13/pflag"

	"github.com/formancehq/go-libs/v5/pkg/observe"
)

const (
	OtelTracesBatchFlag                  = "otel-traces-batch"
	OtelTracesExporterFlag               = "otel-traces-exporter"
	OtelTracesExporterJaegerEndpointFlag = "otel-traces-exporter-jaeger-endpoint"
	OtelTracesExporterJaegerUserFlag     = "otel-traces-exporter-jaeger-user"
	OtelTracesExporterJaegerPasswordFlag = "otel-traces-exporter-jaeger-password"
	OtelTracesExporterOTLPModeFlag       = "otel-traces-exporter-otlp-mode"
	OtelTracesExporterOTLPEndpointFlag   = "otel-traces-exporter-otlp-endpoint"
	OtelTracesExporterOTLPInsecureFlag   = "otel-traces-exporter-otlp-insecure"
)

func AddFlags(flags *flag.FlagSet) {
	observe.AddFlags(flags)

	flags.Bool(OtelTracesBatchFlag, false, "Use OpenTelemetry batching")
	flags.String(OtelTracesExporterFlag, "", "OpenTelemetry traces exporter")
	flags.String(OtelTracesExporterJaegerEndpointFlag, "", "OpenTelemetry traces Jaeger exporter endpoint")
	flags.String(OtelTracesExporterJaegerUserFlag, "", "OpenTelemetry traces Jaeger exporter user")
	flags.String(OtelTracesExporterJaegerPasswordFlag, "", "OpenTelemetry traces Jaeger exporter password")
	flags.String(OtelTracesExporterOTLPModeFlag, "grpc", "OpenTelemetry traces OTLP exporter mode (grpc|http)")
	flags.String(OtelTracesExporterOTLPEndpointFlag, "", "OpenTelemetry traces grpc endpoint")
	flags.Bool(OtelTracesExporterOTLPInsecureFlag, false, "OpenTelemetry traces grpc insecure")
}

// Enabled reports whether a traces exporter is configured.
//
// It is the condition the logging stacks take for trace correlation: stamping
// trace_id/span_id for a trace no backend will receive correlates a record with
// nothing. It lives here, beside the flag it reads, so every service asks the
// question the same way instead of each copying the two lines.
func Enabled(flags *flag.FlagSet) bool {
	exporter, _ := flags.GetString(OtelTracesExporterFlag)

	return exporter != ""
}

func ConfigFromFlags(flags *flag.FlagSet) ModuleConfig {
	batch, _ := flags.GetBool(OtelTracesBatchFlag)
	exporter, _ := flags.GetString(OtelTracesExporterFlag)
	serviceName, _ := flags.GetString(observe.OtelServiceNameFlag)
	resourceAttributes, _ := flags.GetStringSlice(observe.OtelResourceAttributesFlag)

	cfg := ModuleConfig{
		Batch:              batch,
		Exporter:           exporter,
		ServiceName:        serviceName,
		ResourceAttributes: resourceAttributes,
	}

	if exporter == OTLPExporter {
		mode, _ := flags.GetString(OtelTracesExporterOTLPModeFlag)
		endpoint, _ := flags.GetString(OtelTracesExporterOTLPEndpointFlag)
		insecure, _ := flags.GetBool(OtelTracesExporterOTLPInsecureFlag)
		cfg.OTLPConfig = &OTLPConfig{
			Mode:     mode,
			Endpoint: endpoint,
			Insecure: insecure,
		}
	}

	return cfg
}
