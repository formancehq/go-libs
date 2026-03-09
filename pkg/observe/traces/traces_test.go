package traces_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/observe"
	"github.com/formancehq/go-libs/v5/pkg/observe/traces"
)

func TestModuleConfig(t *testing.T) {
	t.Parallel()

	t.Run("otlp-exporter-with-grpc-config", func(t *testing.T) {
		cfg := traces.ModuleConfig{
			Exporter: traces.OTLPExporter,
			OTLPConfig: &traces.OTLPConfig{
				Mode:     observe.ModeGRPC,
				Endpoint: "remote:8080",
				Insecure: true,
			},
		}
		require.Equal(t, "otlp", cfg.Exporter)
		require.Equal(t, "grpc", cfg.OTLPConfig.Mode)
	})

	t.Run("otlp-exporter-with-http-config", func(t *testing.T) {
		cfg := traces.ModuleConfig{
			Exporter: traces.OTLPExporter,
			OTLPConfig: &traces.OTLPConfig{
				Mode:     observe.ModeHTTP,
				Endpoint: "remote:8080",
				Insecure: true,
			},
		}
		require.Equal(t, "http", cfg.OTLPConfig.Mode)
	})

	t.Run("stdout-exporter", func(t *testing.T) {
		cfg := traces.ModuleConfig{
			Exporter: traces.StdoutExporter,
		}
		require.Equal(t, "stdout", cfg.Exporter)
	})
}
