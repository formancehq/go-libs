package otlptraces

import (
	"context"
	"testing"

	"github.com/formancehq/go-libs/v2/otlp"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

func TestTracesModule(t *testing.T) {
	t.Parallel()
	type testCase struct {
		name   string
		config ModuleConfig
	}

	tests := []testCase{
		{
			name: "otlp-exporter",
			config: ModuleConfig{
				Exporter: OTLPExporter,
			},
		},
		{
			name: "otlp-exporter-with-grpc-config",
			config: ModuleConfig{
				Exporter: OTLPExporter,
				OTLPConfig: &OTLPConfig{
					Mode:     otlp.ModeGRPC,
					Endpoint: "remote:8080",
					Insecure: true,
				},
			},
		},
		{
			name: "otlp-exporter-with-http-config",
			config: ModuleConfig{
				Exporter: OTLPExporter,
				OTLPConfig: &OTLPConfig{
					Mode:     otlp.ModeHTTP,
					Endpoint: "remote:8080",
					Insecure: true,
				},
			},
		},
		{
			name: "stdout-exporter",
			config: ModuleConfig{
				Exporter: StdoutExporter,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			options := []fx.Option{otlp.NewFxModule(otlp.Config{}), TracesModule(test.config)}
			if !testing.Verbose() {
				options = append(options, fx.NopLogger)
			}
			options = append(options, fx.Provide(func() *testing.T {
				return t
			}))
			require.NoError(t, fx.ValidateApp(options...))

			app := fx.New(options...)
			require.NoError(t, app.Start(context.Background()))
			defer func(app *fx.App, ctx context.Context) {
				err := app.Stop(ctx)
				if err != nil {
					panic(err)
				}
			}(app, context.Background())
		})
	}

}
