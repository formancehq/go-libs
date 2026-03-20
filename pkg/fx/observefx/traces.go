package observefx

import (
	"context"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v5/pkg/observe"
	"github.com/formancehq/go-libs/v5/pkg/observe/traces"
)

const TracerProviderOptionKey = `group:"_tracerProviderOption"`

func ProvideTracerProviderOption(v any, annotations ...fx.Annotation) fx.Option {
	annotations = append(annotations, fx.ResultTags(TracerProviderOptionKey))
	return fx.Provide(fx.Annotate(v, annotations...))
}

func TracesModule(cfg traces.ModuleConfig) fx.Option {
	if cfg.Exporter == "" {
		return fx.Provide(fx.Annotate(noop.NewTracerProvider, fx.As(new(trace.TracerProvider))))
	}

	options := make([]fx.Option, 0)
	options = append(options,
		fx.Supply(cfg),
		fx.Provide(fx.Annotate(func(options ...tracesdk.TracerProviderOption) *tracesdk.TracerProvider {
			return tracesdk.NewTracerProvider(options...)
		}, fx.ParamTags(TracerProviderOptionKey))),
		fx.Provide(func(defaultTracerProvider *tracesdk.TracerProvider) trace.TracerProvider {
			return defaultTracerProvider
		}),
		fx.Invoke(func(tp trace.TracerProvider) trace.TracerProvider {
			otel.SetTracerProvider(tp)
			otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
				b3.New(), propagation.TraceContext{}))
			return tp
		}),
		fx.Invoke(func(lc fx.Lifecycle, tracerProvider *tracesdk.TracerProvider) {
			lc.Append(fx.Hook{
				OnStop: func(ctx context.Context) error {
					return tracerProvider.Shutdown(ctx)
				},
			})
		}),
		ProvideTracerProviderOption(tracesdk.WithResource),
	)
	if cfg.Batch {
		options = append(options, ProvideTracerProviderOption(tracesdk.WithBatcher, fx.ParamTags(``, `group:"_batchOptions"`)))
	} else {
		options = append(options, ProvideTracerProviderOption(tracesdk.WithSyncer))
	}

	switch cfg.Exporter {
	case traces.StdoutExporter:
		options = append(options, StdoutTracerModule())
	case traces.OTLPExporter:
		options = append(options, OTLPTracerModule())
		mode := observe.ModeGRPC
		if cfg.OTLPConfig != nil {
			if cfg.OTLPConfig.Mode != "" {
				mode = cfg.OTLPConfig.Mode
			}
			switch mode {
			case observe.ModeGRPC:
				if cfg.OTLPConfig.Endpoint != "" {
					options = append(options, ProvideOTLPTracerGRPCClientOption(func() otlptracegrpc.Option {
						return otlptracegrpc.WithEndpoint(cfg.OTLPConfig.Endpoint)
					}))
				}
				if cfg.OTLPConfig.Insecure {
					options = append(options, ProvideOTLPTracerGRPCClientOption(func() otlptracegrpc.Option {
						return otlptracegrpc.WithInsecure()
					}))
				}
			case observe.ModeHTTP:
				if cfg.OTLPConfig.Endpoint != "" {
					options = append(options, ProvideOTLPTracerHTTPClientOption(func() otlptracehttp.Option {
						return otlptracehttp.WithEndpoint(cfg.OTLPConfig.Endpoint)
					}))
				}
				if cfg.OTLPConfig.Insecure {
					options = append(options, ProvideOTLPTracerHTTPClientOption(func() otlptracehttp.Option {
						return otlptracehttp.WithInsecure()
					}))
				}
			}
		}
		switch mode {
		case observe.ModeGRPC:
			options = append(options, OTLPTracerGRPCClientModule())
		case observe.ModeHTTP:
			options = append(options, OTLPTracerHTTPClientModule())
		}
	}

	return fx.Options(options...)
}

func TracesModuleFromFlags(cmd *cobra.Command) fx.Option {
	return TracesModule(traces.ConfigFromFlags(cmd.Flags()))
}

func StdoutTracerModule() fx.Option {
	return fx.Options(
		fx.Provide(
			fx.Annotate(traces.NewStdoutExporter, fx.As(new(tracesdk.SpanExporter))),
		),
	)
}

func OTLPTracerModule() fx.Option {
	return fx.Options(
		fx.Provide(
			fx.Annotate(traces.NewOTLPExporter, fx.As(new(tracesdk.SpanExporter))),
		),
	)
}

const OTLPTracerGRPCOptionsKey = `group:"_otlpTracerGrpcOptions"`

func ProvideOTLPTracerGRPCClientOption(provider any) fx.Option {
	return fx.Provide(
		fx.Annotate(provider, fx.ResultTags(OTLPTracerGRPCOptionsKey), fx.As(new(otlptracegrpc.Option))),
	)
}

func OTLPTracerGRPCClientModule() fx.Option {
	return fx.Options(
		fx.Provide(
			fx.Annotate(traces.NewOTLPGRPCClient, fx.ParamTags(OTLPTracerGRPCOptionsKey)),
		),
	)
}

const OTLPTracerHTTPOptionsKey = `group:"_otlpTracerHTTPOptions"`

func ProvideOTLPTracerHTTPClientOption(provider any) fx.Option {
	return fx.Provide(
		fx.Annotate(provider, fx.ResultTags(OTLPTracerHTTPOptionsKey), fx.As(new(otlptracehttp.Option))),
	)
}

func OTLPTracerHTTPClientModule() fx.Option {
	return fx.Options(
		fx.Provide(
			fx.Annotate(traces.NewOTLPHTTPClient, fx.ParamTags(OTLPTracerHTTPOptionsKey)),
		),
	)
}
