package observefx

import (
	"github.com/formancehq/go-libs/v5/pkg/fx/otelfx"
	"github.com/formancehq/go-libs/v5/pkg/observe/traces"
	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

const TracerProviderOptionKey = otelfx.TracerProviderOptionKey

const OTLPTracerGRPCOptionsKey = otelfx.OTLPTracerGRPCOptionsKey

const OTLPTracerHTTPOptionsKey = otelfx.OTLPTracerHTTPOptionsKey

// ProvideTracerProviderOption delegates to the SDK-independent otelfx package.
func ProvideTracerProviderOption(v any, annotations ...fx.Annotation) fx.Option {
	return otelfx.ProvideTracerProviderOption(v, annotations...)
}

// TracesModule delegates to the SDK-independent otelfx package.
func TracesModule(cfg traces.ModuleConfig) fx.Option {
	return otelfx.TracesModule(cfg)
}

// TracesModuleFromFlags delegates to the SDK-independent otelfx package.
func TracesModuleFromFlags(cmd *cobra.Command) fx.Option {
	return otelfx.TracesModuleFromFlags(cmd)
}

// StdoutTracerModule delegates to the SDK-independent otelfx package.
func StdoutTracerModule() fx.Option {
	return otelfx.StdoutTracerModule()
}

// OTLPTracerModule delegates to the SDK-independent otelfx package.
func OTLPTracerModule() fx.Option {
	return otelfx.OTLPTracerModule()
}

// ProvideOTLPTracerGRPCClientOption delegates to the SDK-independent otelfx package.
func ProvideOTLPTracerGRPCClientOption(provider any) fx.Option {
	return otelfx.ProvideOTLPTracerGRPCClientOption(provider)
}

// OTLPTracerGRPCClientModule delegates to the SDK-independent otelfx package.
func OTLPTracerGRPCClientModule() fx.Option {
	return otelfx.OTLPTracerGRPCClientModule()
}

// ProvideOTLPTracerHTTPClientOption delegates to the SDK-independent otelfx package.
func ProvideOTLPTracerHTTPClientOption(provider any) fx.Option {
	return otelfx.ProvideOTLPTracerHTTPClientOption(provider)
}

// OTLPTracerHTTPClientModule delegates to the SDK-independent otelfx package.
func OTLPTracerHTTPClientModule() fx.Option {
	return otelfx.OTLPTracerHTTPClientModule()
}
