package sharedotlptraces

import (
	"context"
	"testing"

	sharedotlp "github.com/formancehq/go-libs/sharedotlp/pkg"
	"github.com/stretchr/testify/assert"
	"go.uber.org/fx"
)

func TestTracesModule(t *testing.T) {
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
					Mode:     sharedotlp.ModeGRPC,
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
					Mode:     sharedotlp.ModeHTTP,
					Endpoint: "remote:8080",
					Insecure: true,
				},
			},
		},
		{
			name: "jaeger-exporter",
			config: ModuleConfig{
				Exporter: JaegerExporter,
			},
		},
		{
			name: "jaeger-exporter-with-config",
			config: ModuleConfig{
				Exporter:     JaegerExporter,
				JaegerConfig: &JaegerConfig{},
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
			options := []fx.Option{TracesModule(test.config)}
			if !testing.Verbose() {
				options = append(options, fx.NopLogger)
			}
			options = append(options, fx.Provide(func() *testing.T {
				return t
			}))
			assert.NoError(t, fx.ValidateApp(options...))

			app := fx.New(options...)
			assert.NoError(t, app.Start(context.Background()))
			defer func(app *fx.App, ctx context.Context) {
				err := app.Stop(ctx)
				if err != nil {
					panic(err)
				}
			}(app, context.Background())
		})
	}

}
