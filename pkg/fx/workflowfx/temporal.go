package workflowfx

import (
	"context"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.uber.org/fx"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/workflow/temporal"
)

func TemporalClientModuleFromFlags(cmd *cobra.Command, tracer trace.Tracer, searchAttributes temporal.SearchAttributes) fx.Option {
	address, _ := cmd.Flags().GetString(temporal.TemporalAddressFlag)
	namespace, _ := cmd.Flags().GetString(temporal.TemporalNamespaceFlag)
	certStr, _ := cmd.Flags().GetString(temporal.TemporalSSLClientCertFlag)
	key, _ := cmd.Flags().GetString(temporal.TemporalSSLClientKeyFlag)
	initSearchAttributes, _ := cmd.Flags().GetBool(temporal.TemporalInitSearchAttributesFlag)
	encryptionEnabled, _ := cmd.Flags().GetBool(temporal.TemporalEncryptionEnabledFlag)
	encryptionKey, _ := cmd.Flags().GetString(temporal.TemporalEncryptionAESKeyFlag)

	cfg := temporal.ClientConfig{
		Address:           address,
		Namespace:         namespace,
		TLSCertPEM:        certStr,
		TLSKeyPEM:         key,
		EncryptionEnabled: encryptionEnabled,
		EncryptionKey:     encryptionKey,
	}

	return TemporalClientModule(cfg, tracer, searchAttributes, initSearchAttributes)
}

func TemporalClientModule(cfg temporal.ClientConfig, tracer trace.Tracer, searchAttributes temporal.SearchAttributes, initSearchAttributes bool) fx.Option {
	return fx.Options(
		fx.Provide(func(logger logging.Logger, meterProvider metric.MeterProvider) (client.Options, error) {
			return temporal.NewClientOptions(cfg, tracer, logger, meterProvider)
		}),
		fx.Provide(client.Dial),
		fx.Invoke(func(lifecycle fx.Lifecycle, c client.Client) {
			lifecycle.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					if initSearchAttributes {
						return temporal.CreateSearchAttributes(ctx, c, cfg.Namespace, searchAttributes.SearchAttributes)
					}
					return nil
				},
				OnStop: func(ctx context.Context) error {
					c.Close()
					return nil
				},
			})
		}),
	)
}

func TemporalWorkerModule(ctx context.Context, taskQueue string, options worker.Options) fx.Option {
	return fx.Options(
		fx.Provide(
			fx.Annotate(func(logger logging.Logger, c client.Client, workflows, activities []temporal.DefinitionSet) worker.Worker {
				return temporal.New(ctx, logger, c, taskQueue, workflows, activities, options)
			}, fx.ParamTags(``, ``, `group:"workflows"`, `group:"activities"`)),
		),
		fx.Invoke(func(lc fx.Lifecycle, w worker.Worker) {
			willStop := false
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					go func() {
						err := w.Run(worker.InterruptCh())
						if err != nil {
							if !willStop {
								panic(err)
							}
						}
					}()
					return nil
				},
				OnStop: func(ctx context.Context) error {
					willStop = true
					w.Stop()
					return nil
				},
			})
		}),
	)
}
