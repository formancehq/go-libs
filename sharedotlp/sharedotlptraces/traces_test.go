package sharedotlptraces

import (
	"context"
	"fmt"
	sharedotlp "github.com/numary/go-libs/sharedotlp"
	"github.com/stretchr/testify/assert"
	"go.uber.org/fx"
	"testing"
)

func TestTracesModule(t *testing.T) {

	type testCase struct {
		name   string
		config ModuleConfig
	}

	tests := []testCase{
		{
			name: fmt.Sprintf("otlp-exporter"),
			config: ModuleConfig{
				Exporter: OTLPExporter,
			},
		},
		{
			name: fmt.Sprintf("otlp-exporter-with-grpc-config"),
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
			name: fmt.Sprintf("otlp-exporter-with-http-config"),
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
			name: fmt.Sprintf("jaeger-exporter"),
			config: ModuleConfig{
				Exporter: JaegerExporter,
			},
		},
		{
			name: fmt.Sprintf("jaeger-exporter-with-config"),
			config: ModuleConfig{
				Exporter:     JaegerExporter,
				JaegerConfig: &JaegerConfig{},
			},
		},
		{
			name: fmt.Sprintf("stdout-exporter"),
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
			defer app.Stop(context.Background())
		})
	}

}
