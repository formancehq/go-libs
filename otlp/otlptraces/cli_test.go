package otlptraces

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/formancehq/go-libs/v3/otlp"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/fx"
)

func TestOTLPTracesModule(t *testing.T) {
	t.Parallel()
	type testCase struct {
		name                 string
		args                 []string
		expectedSpanExporter tracesdk.SpanExporter
	}

	for _, testCase := range []testCase{
		{
			name: "otlp",
			args: []string{
				fmt.Sprintf("--%s=%s", OtelTracesExporterFlag, "otlp"),
			},
			expectedSpanExporter: &otlptrace.Exporter{},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			cmd := &cobra.Command{
				RunE: func(cmd *cobra.Command, args []string) error {
					app := fx.New(
						fx.NopLogger,
						otlp.NewFxModule(otlp.Config{}),
						FXModuleFromFlags(cmd),
						fx.Invoke(func(lc fx.Lifecycle, spanExporter tracesdk.SpanExporter) {
							lc.Append(fx.Hook{
								OnStart: func(ctx context.Context) error {
									if !reflect.TypeOf(otel.GetTracerProvider()).
										AssignableTo(reflect.TypeOf(&tracesdk.TracerProvider{})) {
										return errors.New("otel.GetTracerProvider() should return a *tracesdk.TracerProvider instance")
									}
									if !reflect.TypeOf(spanExporter).
										AssignableTo(reflect.TypeOf(testCase.expectedSpanExporter)) {
										return fmt.Errorf("span exporter should be of type %t", testCase.expectedSpanExporter)
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
			AddFlags(cmd.Flags())

			cmd.SetArgs(testCase.args)

			require.NoError(t, cmd.Execute())
		})
	}
}
