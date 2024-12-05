package otlp

import (
	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

type Config struct {
	ServiceName        string
	ResourceAttributes []string
	serviceVersion     string
}

type Option func(*Config)

func WithServiceVersion(version string) Option {
	return func(cfg *Config) {
		cfg.serviceVersion = version
	}
}

func NewFxModule(cfg Config) fx.Option {
	return fx.Options(
		LoadResource(cfg.ServiceName, cfg.ResourceAttributes, cfg.serviceVersion),
	)
}

func FXModuleFromFlags(cmd *cobra.Command, opts ...Option) fx.Option {
	otelServiceName, _ := cmd.Flags().GetString(OtelServiceNameFlag)
	otelResourceAttributes, _ := cmd.Flags().GetStringSlice(OtelResourceAttributesFlag)

	return NewFxModule(Config{
		ServiceName:        otelServiceName,
		ResourceAttributes: otelResourceAttributes,
	})
}
