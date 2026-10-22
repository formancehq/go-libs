package observefx

import (
	"github.com/formancehq/go-libs/v5/pkg/fx/otelfx"
	"github.com/formancehq/go-libs/v5/pkg/observe/metrics"
	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

const OTLPMetricsGRPCOptionsKey = otelfx.OTLPMetricsGRPCOptionsKey

const OTLPMetricsHTTPOptionsKey = otelfx.OTLPMetricsHTTPOptionsKey

const OTLPMetricsPeriodicReaderOptionsKey = otelfx.OTLPMetricsPeriodicReaderOptionsKey

// ProvideMetricsProviderOption delegates to the SDK-independent otelfx package.
func ProvideMetricsProviderOption(v any, annotations ...fx.Annotation) fx.Option {
	return otelfx.ProvideMetricsProviderOption(v, annotations...)
}

// ProvideRuntimeMetricsOption delegates to the SDK-independent otelfx package.
func ProvideRuntimeMetricsOption(v any, annotations ...fx.Annotation) fx.Option {
	return otelfx.ProvideRuntimeMetricsOption(v, annotations...)
}

// MetricsModule delegates to the SDK-independent otelfx package.
func MetricsModule(cfg metrics.ModuleConfig) fx.Option {
	return otelfx.MetricsModule(cfg)
}

// MetricsModuleFromFlags delegates to the SDK-independent otelfx package.
func MetricsModuleFromFlags(cmd *cobra.Command) fx.Option {
	return otelfx.MetricsModuleFromFlags(cmd)
}

// StdoutMetricsModule delegates to the SDK-independent otelfx package.
func StdoutMetricsModule() fx.Option {
	return otelfx.StdoutMetricsModule()
}

// ProvideOTLPMetricsGRPCOption delegates to the SDK-independent otelfx package.
func ProvideOTLPMetricsGRPCOption(provider any) fx.Option {
	return otelfx.ProvideOTLPMetricsGRPCOption(provider)
}

// ProvideOTLPMetricsHTTPOption delegates to the SDK-independent otelfx package.
func ProvideOTLPMetricsHTTPOption(provider any) fx.Option {
	return otelfx.ProvideOTLPMetricsHTTPOption(provider)
}

// ProvideOTLPMetricsPeriodicReaderOption delegates to the SDK-independent otelfx package.
func ProvideOTLPMetricsPeriodicReaderOption(provider any) fx.Option {
	return otelfx.ProvideOTLPMetricsPeriodicReaderOption(provider)
}

// ProvideOTLPMetricsGRPCExporter delegates to the SDK-independent otelfx package.
func ProvideOTLPMetricsGRPCExporter() fx.Option {
	return otelfx.ProvideOTLPMetricsGRPCExporter()
}

// ProvideOTLPMetricsHTTPExporter delegates to the SDK-independent otelfx package.
func ProvideOTLPMetricsHTTPExporter() fx.Option {
	return otelfx.ProvideOTLPMetricsHTTPExporter()
}
