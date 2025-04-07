package otlpmetrics

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestProvideOTLPMetricsGRPCExporter(t *testing.T) {
	option := ProvideOTLPMetricsGRPCExporter()
	require.NotNil(t, option, "L'option ne devrait pas être nil")
}

func TestProvideOTLPMetricsHTTPExporter(t *testing.T) {
	option := ProvideOTLPMetricsHTTPExporter()
	require.NotNil(t, option, "L'option ne devrait pas être nil")
}

func TestProvideOTLPMetricsGRPCOption(t *testing.T) {
	// Créer une fonction qui retourne une option GRPC
	provider := func() otlpmetricgrpc.Option {
		return otlpmetricgrpc.WithEndpoint("localhost:4317")
	}

	option := ProvideOTLPMetricsGRPCOption(provider)
	require.NotNil(t, option, "L'option ne devrait pas être nil")
}

func TestProvideOTLPMetricsHTTPOption(t *testing.T) {
	// Créer une fonction qui retourne une option HTTP
	provider := func() otlpmetrichttp.Option {
		return otlpmetrichttp.WithEndpoint("localhost:4318")
	}

	option := ProvideOTLPMetricsHTTPOption(provider)
	require.NotNil(t, option, "L'option ne devrait pas être nil")
}

func TestProvideOTLPMetricsPeriodicReaderOption(t *testing.T) {
	// Créer une fonction qui retourne une option PeriodicReader
	provider := func() sdkmetric.PeriodicReaderOption {
		return sdkmetric.WithInterval(0)
	}

	option := ProvideOTLPMetricsPeriodicReaderOption(provider)
	require.NotNil(t, option, "L'option ne devrait pas être nil")
}
