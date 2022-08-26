package sharedotlpmetrics

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	controller "go.opentelemetry.io/otel/sdk/metric/controller/basic"
	"go.uber.org/fx"
)

func TestOTLPMetricsModule(t *testing.T) {
	cmd := &cobra.Command{
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return viper.BindPFlags(cmd.Flags())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fx.New(
				fx.NopLogger,
				CLIMetricsModule(viper.GetViper()),
				fx.Invoke(func(lc fx.Lifecycle, provider metric.MeterProvider) {
					lc.Append(fx.Hook{
						OnStart: func(ctx context.Context) error {
							if !reflect.TypeOf(provider).
								AssignableTo(reflect.TypeOf(&controller.Controller{})) {
								return errors.New("global.GetMeterProvider() should return a *controller.Controller instance")
							}
							return nil
						},
					})
				}))
			require.NoError(t, app.Start(cmd.Context()))
			require.NoError(t, app.Err())
			return nil
		},
	}
	InitOTLPMetricsFlags(cmd.Flags())

	cmd.SetArgs([]string{
		fmt.Sprintf("--%s", OtelMetricsFlag),
		fmt.Sprintf("--%s=%s", OtelMetricsExporterFlag, "otlp"),
	})

	require.NoError(t, cmd.Execute())
}
