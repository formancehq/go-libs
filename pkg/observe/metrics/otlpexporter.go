package metrics

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func NewOTLPGRPCExporter(options ...otlpmetricgrpc.Option) (sdkmetric.Exporter, error) {
	return otlpmetricgrpc.New(context.Background(), options...)
}

func NewOTLPHTTPExporter(options ...otlpmetrichttp.Option) (sdkmetric.Exporter, error) {
	return otlpmetrichttp.New(context.Background(), options...)
}
