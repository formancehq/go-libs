package testservice

import (
	"context"
	"strings"

	"github.com/formancehq/go-libs/v4/otlp"
	"github.com/formancehq/go-libs/v4/otlp/otlpmetrics"
	"github.com/formancehq/go-libs/v4/testing/deferred"
)

func OTLPInstrumentation(otlpConfiguration *deferred.Deferred[OTLPConfig]) Instrumentation {
	return InstrumentationFunc(func(ctx context.Context, cfg *RunConfiguration) error {
		if otlpConfiguration.GetValue().Metrics != nil {
			cfg.AppendArgs("--"+otlpmetrics.OtelMetricsExporterFlag, otlpConfiguration.GetValue().Metrics.Exporter)
			if otlpConfiguration.GetValue().Metrics.KeepInMemory {
				cfg.AppendArgs("--" + otlpmetrics.OtelMetricsKeepInMemoryFlag)
			}
			if otlpConfiguration.GetValue().Metrics.OTLPConfig != nil {
				cfg.AppendArgs(
					"--"+otlpmetrics.OtelMetricsExporterOTLPEndpointFlag, otlpConfiguration.GetValue().Metrics.OTLPConfig.Endpoint,
					"--"+otlpmetrics.OtelMetricsExporterOTLPModeFlag, otlpConfiguration.GetValue().Metrics.OTLPConfig.Mode,
				)
				if otlpConfiguration.GetValue().Metrics.OTLPConfig.Insecure {
					cfg.AppendArgs("--" + otlpmetrics.OtelMetricsExporterOTLPInsecureFlag)
				}
			}
			if otlpConfiguration.GetValue().Metrics.RuntimeMetrics {
				cfg.AppendArgs("--" + otlpmetrics.OtelMetricsRuntimeFlag)
			}
			if otlpConfiguration.GetValue().Metrics.MinimumReadMemStatsInterval != 0 {
				cfg.AppendArgs(
					"--"+otlpmetrics.OtelMetricsRuntimeMinimumReadMemStatsIntervalFlag,
					otlpConfiguration.GetValue().Metrics.MinimumReadMemStatsInterval.String(),
				)
			}
			if otlpConfiguration.GetValue().Metrics.PushInterval != 0 {
				cfg.AppendArgs(
					"--"+otlpmetrics.OtelMetricsExporterPushIntervalFlag,
					otlpConfiguration.GetValue().Metrics.PushInterval.String(),
				)
			}
			if len(otlpConfiguration.GetValue().Metrics.ResourceAttributes) > 0 {
				cfg.AppendArgs(
					"--"+otlp.OtelResourceAttributesFlag,
					strings.Join(otlpConfiguration.GetValue().Metrics.ResourceAttributes, ","),
				)
			}
		}
		if otlpConfiguration.GetValue().BaseConfig.ServiceName != "" {
			cfg.AppendArgs("--"+otlp.OtelServiceNameFlag, otlpConfiguration.GetValue().BaseConfig.ServiceName)
		}

		return nil
	})
}

type OTLPConfig struct {
	BaseConfig otlp.Config
	Metrics    *otlpmetrics.ModuleConfig
}
