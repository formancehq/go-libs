package observefx

import (
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v5/pkg/observe"
)

func ResourceModule(cfg observe.Config) fx.Option {
	return fx.Options(
		fx.Provide(func() (*resource.Resource, error) {
			return observe.BuildResource(cfg.ServiceName, cfg.ResourceAttributes, cfg.ServiceVersion)
		}),
		fx.Invoke(func(res *resource.Resource) {
			// Lets any instrumentation using observe.ResourceAttributes
			// (pkg/observe/job, internal/httpx) attach the configured
			// resource's attributes to its own spans/metrics, matching what
			// it already does for OTEL_RESOURCE_ATTRIBUTES -- necessary
			// since resource attributes configured this way (e.g. via
			// observe.Config.ResourceAttributes) never touch that env var.
			observe.SetResourceAttributes(res.Attributes()...)
		}),
	)
}

func ResourceModuleFromFlags(cmd *cobra.Command, opts ...observe.Option) fx.Option {
	otelServiceName, _ := cmd.Flags().GetString(observe.OtelServiceNameFlag)
	otelResourceAttributes, _ := cmd.Flags().GetStringSlice(observe.OtelResourceAttributesFlag)

	cfg := observe.NewConfig(opts...)
	cfg.ServiceName = otelServiceName
	cfg.ResourceAttributes = otelResourceAttributes
	return ResourceModule(cfg)
}
