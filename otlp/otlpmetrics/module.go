package otlpmetrics

import (
	"context"
	"time"

	"github.com/formancehq/go-libs/v3/logging"
	"go.opentelemetry.io/contrib/instrumentation/host"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/formancehq/go-libs/v3/otlp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.uber.org/fx"
)

const (
	metricsProviderOptionKey = `group:"_metricsProviderOption"`
	metricsRuntimeOptionKey  = `group:"_metricsRuntimeOption"`

	StdoutExporter = "stdout"
	OTLPExporter   = "otlp"
)

type ModuleConfig struct {
	RuntimeMetrics              bool
	MinimumReadMemStatsInterval time.Duration

	Exporter           string
	OTLPConfig         *OTLPConfig
	PushInterval       time.Duration
	ResourceAttributes []string
	KeepInMemory       bool
}

type OTLPConfig struct {
	Mode     string
	Endpoint string
	Insecure bool
}

func ProvideMetricsProviderOption(v any, annotations ...fx.Annotation) fx.Option {
	annotations = append(annotations, fx.ResultTags(metricsProviderOptionKey))
	return fx.Provide(fx.Annotate(v, annotations...))
}

func ProvideRuntimeMetricsOption(v any, annotations ...fx.Annotation) fx.Option {
	annotations = append(annotations, fx.ResultTags(metricsRuntimeOptionKey))
	return fx.Provide(fx.Annotate(v, annotations...))

}

func MetricsModule(cfg ModuleConfig) fx.Option {
	if cfg.Exporter == "" && !cfg.KeepInMemory {
		return fx.Provide(fx.Annotate(noop.NewMeterProvider, fx.As(new(sdkmetric.MeterProvider))))
	}

	options := make([]fx.Option, 0)
	if cfg.KeepInMemory {
		options = append(options,
			fx.Provide(NewInMemoryExporterDecorator),
			fx.Decorate(func(exporter *InMemoryExporter) sdkmetric.Exporter {
				return exporter
			}),
		)
	}

	options = append(options,
		fx.Supply(cfg),
		fx.Provide(func(mp *sdkmetric.MeterProvider) metric.MeterProvider { return mp }),
		fx.Provide(fx.Annotate(func(options ...sdkmetric.Option) *sdkmetric.MeterProvider {
			ret := sdkmetric.NewMeterProvider(options...)
			otel.SetMeterProvider(ret)

			return ret
		}, fx.ParamTags(metricsProviderOptionKey))),
		fx.Invoke(func(lc fx.Lifecycle, metricProvider *sdkmetric.MeterProvider, options ...runtime.Option) {
			// set global propagator to tracecontext (the default is no-op).
			otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
				b3.New(), propagation.TraceContext{})) // B3 format is common and used by zipkin. Always enabled right now.
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					if cfg.RuntimeMetrics {
						if err := runtime.Start(options...); err != nil {
							return err
						}
						if err := host.Start(); err != nil {
							return err
						}
					}
					return nil
				},
				OnStop: func(ctx context.Context) error {
					logging.FromContext(ctx).Infof("Flush metrics")
					if err := metricProvider.ForceFlush(ctx); err != nil {
						logging.FromContext(ctx).Errorf("unable to flush metrics: %s", err)
					}
					logging.FromContext(ctx).Infof("Shutting down metrics provider")
					if err := metricProvider.Shutdown(ctx); err != nil {
						logging.FromContext(ctx).Errorf("unable to shutdown metrics provider: %s", err)
					}
					logging.FromContext(ctx).Infof("Metrics provider stopped")
					return nil
				},
			})
		}),
		ProvideMetricsProviderOption(sdkmetric.WithResource),
		ProvideMetricsProviderOption(sdkmetric.WithReader),
		fx.Provide(
			fx.Annotate(sdkmetric.NewPeriodicReader, fx.ParamTags(``, OTLPMetricsPeriodicReaderOptionsKey), fx.As(new(sdkmetric.Reader))),
		),
		ProvideOTLPMetricsPeriodicReaderOption(func() sdkmetric.PeriodicReaderOption {
			return sdkmetric.WithInterval(cfg.PushInterval)
		}),
		ProvideRuntimeMetricsOption(func() runtime.Option {
			return runtime.WithMinimumReadMemStatsInterval(cfg.MinimumReadMemStatsInterval)
		}),
	)

	switch cfg.Exporter {
	case StdoutExporter:
		options = append(options, StdoutMetricsModule())
	case OTLPExporter:
		mode := otlp.ModeGRPC
		if cfg.OTLPConfig != nil {
			if cfg.OTLPConfig.Mode != "" {
				mode = cfg.OTLPConfig.Mode
			}
		}
		switch mode {
		case otlp.ModeGRPC:
			if cfg.OTLPConfig != nil {
				if cfg.OTLPConfig.Endpoint != "" {
					options = append(options, ProvideOTLPMetricsGRPCOption(func() otlpmetricgrpc.Option {
						return otlpmetricgrpc.WithEndpoint(cfg.OTLPConfig.Endpoint)
					}))
				}
				if cfg.OTLPConfig.Insecure {
					options = append(options, ProvideOTLPMetricsGRPCOption(func() otlpmetricgrpc.Option {
						return otlpmetricgrpc.WithInsecure()
					}))
				}
			}

			options = append(options, ProvideOTLPMetricsGRPCExporter())
		case otlp.ModeHTTP:
			if cfg.OTLPConfig != nil {
				if cfg.OTLPConfig.Endpoint != "" {
					options = append(options, ProvideOTLPMetricsHTTPOption(func() otlpmetrichttp.Option {
						return otlpmetrichttp.WithEndpoint(cfg.OTLPConfig.Endpoint)
					}))
				}
				if cfg.OTLPConfig.Insecure {
					options = append(options, ProvideOTLPMetricsHTTPOption(func() otlpmetrichttp.Option {
						return otlpmetrichttp.WithInsecure()
					}))
				}
			}

			options = append(options, ProvideOTLPMetricsHTTPExporter())
		}
	default:
		options = append(options, fx.Provide(fx.Annotate(NewNoOpExporter, fx.As(new(sdkmetric.Exporter)))))
	}

	return fx.Options(options...)
}
