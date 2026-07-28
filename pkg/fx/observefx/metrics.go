package observefx

import (
	"context"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/host"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v5/pkg/observe"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/observe/metrics"
)

const (
	metricsProviderOptionKey            = `group:"_metricsProviderOption"`
	metricsRuntimeOptionKey             = `group:"_metricsRuntimeOption"`
	OTLPMetricsGRPCOptionsKey           = `group:"_otlpMetricsGrpcOptions"`
	OTLPMetricsHTTPOptionsKey           = `group:"_otlpMetricsHTTPOptions"`
	OTLPMetricsPeriodicReaderOptionsKey = `group:"_otlpMetricsPeriodicReaderOptions"`
)

// secondsHistogramBoundaries are the explicit bucket boundaries (in seconds)
// applied to seconds-unit histograms under ClassicHistograms. Dense
// resolution from 5ms to a few seconds resolves the fast, page/call-bounded
// happy path; boundaries out to 240s cover the retry/backoff tail (a single
// HTTP call in a connectivity plugin can legitimately take minutes under
// sustained upstream rate-limiting).
var secondsHistogramBoundaries = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 120, 240,
}

func ProvideMetricsProviderOption(v any, annotations ...fx.Annotation) fx.Option {
	annotations = append(annotations, fx.ResultTags(metricsProviderOptionKey))
	return fx.Provide(fx.Annotate(v, annotations...))
}

func ProvideRuntimeMetricsOption(v any, annotations ...fx.Annotation) fx.Option {
	annotations = append(annotations, fx.ResultTags(metricsRuntimeOptionKey))
	return fx.Provide(fx.Annotate(v, annotations...))
}

func MetricsModule(cfg metrics.ModuleConfig) fx.Option {
	options := make([]fx.Option, 0)
	options = append(options,
		fx.Supply(cfg),
		fx.Provide(func(mp *sdkmetric.MeterProvider) metric.MeterProvider { return mp }),
		fx.Provide(fx.Annotate(func(options ...sdkmetric.Option) *sdkmetric.MeterProvider {
			os := options
			if !cfg.ClassicHistograms {
				var view sdkmetric.View = func(i sdkmetric.Instrument) (sdkmetric.Stream, bool) {
					s := sdkmetric.Stream{Name: i.Name, Description: i.Description, Unit: i.Unit}
					if i.Kind == sdkmetric.InstrumentKindHistogram {
						s.Aggregation = sdkmetric.AggregationBase2ExponentialHistogram{
							MaxSize:  160,
							MaxScale: 20,
						}
						return s, true
					}
					return s, false
				}
				os = append(os, sdkmetric.WithView(view))
			} else {
				// The SDK's own default explicit-bucket boundaries are
				// unitless numbers tuned for millisecond-scale values (first
				// non-zero boundary: 5). A seconds-unit histogram under that
				// default has its first bucket cover 0-5s, which swallows
				// nearly every sample from this codebase's job/request
				// duration histograms -- those measure page- or call-bounded
				// units of work that normally finish in well under a second.
				// Give seconds-unit histograms boundaries shaped for that
				// distribution instead of the SDK default.
				var view sdkmetric.View = func(i sdkmetric.Instrument) (sdkmetric.Stream, bool) {
					s := sdkmetric.Stream{Name: i.Name, Description: i.Description, Unit: i.Unit}
					if i.Kind == sdkmetric.InstrumentKindHistogram && i.Unit == "s" {
						s.Aggregation = sdkmetric.AggregationExplicitBucketHistogram{
							Boundaries: secondsHistogramBoundaries,
						}
						return s, true
					}
					return s, false
				}
				os = append(os, sdkmetric.WithView(view))
			}

			ret := sdkmetric.NewMeterProvider(os...)
			otel.SetMeterProvider(ret)

			return ret
		}, fx.ParamTags(metricsProviderOptionKey))),
		fx.Invoke(fx.Annotate(
			func(lc fx.Lifecycle, metricProvider *sdkmetric.MeterProvider, options ...runtime.Option) {
				otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
					b3.New(), propagation.TraceContext{}))
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
			},
			fx.ParamTags(``, ``, metricsRuntimeOptionKey),
		)),
		ProvideMetricsProviderOption(sdkmetric.WithResource),
		ProvideMetricsProviderOption(sdkmetric.WithReader),
		fx.Provide(
			fx.Annotate(sdkmetric.NewPeriodicReader, fx.ParamTags(``, OTLPMetricsPeriodicReaderOptionsKey), fx.As(new(sdkmetric.Reader))),
		),
		ProvideOTLPMetricsPeriodicReaderOption(func() sdkmetric.PeriodicReaderOption {
			return sdkmetric.WithInterval(cfg.PushInterval)
		}),
	)
	if cfg.MinimumReadMemStatsInterval > 0 {
		options = append(options, ProvideRuntimeMetricsOption(func() runtime.Option {
			return runtime.WithMinimumReadMemStatsInterval(cfg.MinimumReadMemStatsInterval)
		}))
	}

	switch cfg.Exporter {
	case metrics.StdoutExporter:
		options = append(options, StdoutMetricsModule())
	case metrics.OTLPExporter:
		mode := observe.ModeGRPC
		if cfg.OTLPConfig != nil {
			if cfg.OTLPConfig.Mode != "" {
				mode = cfg.OTLPConfig.Mode
			}
		}
		switch mode {
		case observe.ModeGRPC:
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
		case observe.ModeHTTP:
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
		options = append(options, fx.Provide(fx.Annotate(metrics.NewNoOpExporter, fx.As(new(sdkmetric.Exporter)))))
	}

	if cfg.KeepInMemory {
		options = append(options,
			fx.Provide(metrics.NewInMemoryExporterDecorator),
			fx.Decorate(func(exporter *metrics.InMemoryExporter) sdkmetric.Exporter {
				return exporter
			}),
		)
	}

	return fx.Options(options...)
}

func MetricsModuleFromFlags(cmd *cobra.Command) fx.Option {
	return MetricsModule(metrics.ConfigFromFlags(cmd.Flags()))
}

func StdoutMetricsModule() fx.Option {
	return fx.Provide(
		fx.Annotate(metrics.NewStdoutExporter, fx.As(new(sdkmetric.Exporter))),
	)
}

func ProvideOTLPMetricsGRPCOption(provider any) fx.Option {
	return fx.Provide(
		fx.Annotate(provider, fx.ResultTags(OTLPMetricsGRPCOptionsKey), fx.As(new(otlpmetricgrpc.Option))),
	)
}

func ProvideOTLPMetricsHTTPOption(provider any) fx.Option {
	return fx.Provide(
		fx.Annotate(provider, fx.ResultTags(OTLPMetricsHTTPOptionsKey), fx.As(new(otlpmetrichttp.Option))),
	)
}

func ProvideOTLPMetricsPeriodicReaderOption(provider any) fx.Option {
	return fx.Provide(
		fx.Annotate(provider, fx.ResultTags(OTLPMetricsPeriodicReaderOptionsKey), fx.As(new(sdkmetric.PeriodicReaderOption))),
	)
}

func ProvideOTLPMetricsGRPCExporter() fx.Option {
	return fx.Provide(
		fx.Annotate(metrics.NewOTLPGRPCExporter, fx.ParamTags(OTLPMetricsGRPCOptionsKey), fx.As(new(sdkmetric.Exporter))),
	)
}

func ProvideOTLPMetricsHTTPExporter() fx.Option {
	return fx.Provide(
		fx.Annotate(metrics.NewOTLPHTTPExporter, fx.ParamTags(OTLPMetricsHTTPOptionsKey), fx.As(new(sdkmetric.Exporter))),
	)
}
