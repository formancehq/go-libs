package otlpmetrics

import (
	"testing"
	"time"

	"github.com/formancehq/go-libs/v2/otlp"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestProvideMetricsProviderOption(t *testing.T) {
	provider := func() sdkmetric.Option {
		return sdkmetric.WithView()
	}

	option := ProvideMetricsProviderOption(provider)
	require.NotNil(t, option, "L'option ne devrait pas être nil")
}

func TestProvideRuntimeMetricsOption(t *testing.T) {
	provider := func() any {
		return "test"
	}

	option := ProvideRuntimeMetricsOption(provider)
	require.NotNil(t, option, "L'option ne devrait pas être nil")
}

func TestMetricsModule(t *testing.T) {
	t.Run("with default config", func(t *testing.T) {
		cfg := ModuleConfig{
			PushInterval:       10 * time.Second,
			RuntimeMetrics:     false,
			ResourceAttributes: []string{},
		}

		module := MetricsModule(cfg)
		require.NotNil(t, module, "Le module ne devrait pas être nil")
	})

	t.Run("with stdout exporter", func(t *testing.T) {
		cfg := ModuleConfig{
			PushInterval:       10 * time.Second,
			RuntimeMetrics:     true,
			ResourceAttributes: []string{},
			Exporter:           StdoutExporter,
		}

		module := MetricsModule(cfg)
		require.NotNil(t, module, "Le module ne devrait pas être nil")
	})

	t.Run("with OTLP GRPC exporter", func(t *testing.T) {
		cfg := ModuleConfig{
			PushInterval:       10 * time.Second,
			RuntimeMetrics:     true,
			ResourceAttributes: []string{},
			Exporter:           OTLPExporter,
			OTLPConfig: &OTLPConfig{
				Mode:     otlp.ModeGRPC,
				Endpoint: "localhost:4317",
				Insecure: true,
			},
		}

		module := MetricsModule(cfg)
		require.NotNil(t, module, "Le module ne devrait pas être nil")
	})

	t.Run("with OTLP HTTP exporter", func(t *testing.T) {
		cfg := ModuleConfig{
			PushInterval:       10 * time.Second,
			RuntimeMetrics:     true,
			ResourceAttributes: []string{},
			Exporter:           OTLPExporter,
			OTLPConfig: &OTLPConfig{
				Mode:     otlp.ModeHTTP,
				Endpoint: "localhost:4318",
				Insecure: true,
			},
		}

		module := MetricsModule(cfg)
		require.NotNil(t, module, "Le module ne devrait pas être nil")
	})

	t.Run("with OTLP exporter and nil config", func(t *testing.T) {
		cfg := ModuleConfig{
			PushInterval:       10 * time.Second,
			RuntimeMetrics:     true,
			ResourceAttributes: []string{},
			Exporter:           OTLPExporter,
			OTLPConfig:         nil,
		}

		module := MetricsModule(cfg)
		require.NotNil(t, module, "Le module ne devrait pas être nil")
	})

	t.Run("with in-memory exporter", func(t *testing.T) {
		cfg := ModuleConfig{
			PushInterval:       10 * time.Second,
			RuntimeMetrics:     true,
			ResourceAttributes: []string{},
			KeepInMemory:       true,
		}

		module := MetricsModule(cfg)
		require.NotNil(t, module, "Le module ne devrait pas être nil")
	})
}

func TestOTLPConfig(t *testing.T) {
	cfg := OTLPConfig{
		Mode:     otlp.ModeGRPC,
		Endpoint: "localhost:4317",
		Insecure: true,
	}

	require.Equal(t, otlp.ModeGRPC, cfg.Mode, "Le mode devrait être GRPC")
	require.Equal(t, "localhost:4317", cfg.Endpoint, "L'endpoint devrait être localhost:4317")
	require.True(t, cfg.Insecure, "Le mode insecure devrait être activé")
}

func TestModuleConfig(t *testing.T) {
	cfg := ModuleConfig{
		RuntimeMetrics:              true,
		MinimumReadMemStatsInterval: 15 * time.Second,
		Exporter:                    OTLPExporter,
		OTLPConfig: &OTLPConfig{
			Mode:     otlp.ModeGRPC,
			Endpoint: "localhost:4317",
			Insecure: true,
		},
		PushInterval:       10 * time.Second,
		ResourceAttributes: []string{"service.name=test"},
		KeepInMemory:       true,
	}

	require.True(t, cfg.RuntimeMetrics, "Les métriques runtime devraient être activées")
	require.Equal(t, 15*time.Second, cfg.MinimumReadMemStatsInterval, "L'intervalle de lecture des stats mémoire devrait être de 15 secondes")
	require.Equal(t, OTLPExporter, cfg.Exporter, "L'exportateur devrait être OTLP")
	require.NotNil(t, cfg.OTLPConfig, "La configuration OTLP ne devrait pas être nil")
	require.Equal(t, 10*time.Second, cfg.PushInterval, "L'intervalle de push devrait être de 10 secondes")
	require.Len(t, cfg.ResourceAttributes, 1, "Il devrait y avoir un attribut de ressource")
	require.True(t, cfg.KeepInMemory, "Le mode de conservation en mémoire devrait être activé")
}
