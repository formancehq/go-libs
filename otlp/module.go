package otlp

import (
	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

type Config struct {
	ServiceName        string
	ResourceAttributes []string
}

func NewFxModule(cfg Config) fx.Option {
	return fx.Options(
		LoadResource(cfg.ServiceName, cfg.ResourceAttributes),
	)
}

func FXModuleFromFlags(cmd *cobra.Command) fx.Option {
	otelServiceName, _ := cmd.Flags().GetString(OtelServiceNameFlag)
	otelResourceAttributes, _ := cmd.Flags().GetStringSlice(OtelResourceAttributesFlag)

	return NewFxModule(Config{
		ServiceName:        otelServiceName,
		ResourceAttributes: otelResourceAttributes,
	})
}
