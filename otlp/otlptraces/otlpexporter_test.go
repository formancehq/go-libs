package otlptraces

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func TestLoadOTLPTracerGRPCClient(t *testing.T) {
	client := LoadOTLPTracerGRPCClient()
	require.NotNil(t, client, "Le client GRPC ne devrait pas être nil")
}

func TestLoadOTLPTracerHTTPClient(t *testing.T) {
	client := LoadOTLPTracerHTTPClient()
	require.NotNil(t, client, "Le client HTTP ne devrait pas être nil")
}

func TestOTLPTracerModule(t *testing.T) {
	module := OTLPTracerModule()
	require.NotNil(t, module, "Le module ne devrait pas être nil")
}

func TestProvideOTLPTracerGRPCClientOption(t *testing.T) {
	provider := func() otlptracegrpc.Option {
		return otlptracegrpc.WithEndpoint("localhost:4317")
	}
	
	option := ProvideOTLPTracerGRPCClientOption(provider)
	require.NotNil(t, option, "L'option ne devrait pas être nil")
}

func TestOTLPTracerGRPCClientModule(t *testing.T) {
	module := OTLPTracerGRPCClientModule()
	require.NotNil(t, module, "Le module ne devrait pas être nil")
}

func TestProvideOTLPTracerHTTPClientOption(t *testing.T) {
	provider := func() otlptracehttp.Option {
		return otlptracehttp.WithEndpoint("localhost:4318")
	}
	
	option := ProvideOTLPTracerHTTPClientOption(provider)
	require.NotNil(t, option, "L'option ne devrait pas être nil")
}

func TestOTLPTracerHTTPClientModule(t *testing.T) {
	module := OTLPTracerHTTPClientModule()
	require.NotNil(t, module, "Le module ne devrait pas être nil")
}

type mockClient struct{}

func (m *mockClient) Start(ctx context.Context) error {
	return nil
}

func (m *mockClient) Stop(ctx context.Context) error {
	return nil
}

func (m *mockClient) UploadTraces(ctx context.Context, protoSpans []*tracepb.ResourceSpans) error {
	return nil
}

func TestLoadOTLPTracerProvider(t *testing.T) {
	client := &mockClient{}
	exporter, err := LoadOTLPTracerProvider(client)
	require.NoError(t, err, "La création de l'exportateur ne devrait pas échouer")
	require.NotNil(t, exporter, "L'exportateur ne devrait pas être nil")
}
