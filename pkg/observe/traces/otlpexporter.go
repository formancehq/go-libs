package traces

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
)

func NewOTLPExporter(client otlptrace.Client) (*otlptrace.Exporter, error) {
	return otlptrace.New(context.Background(), client)
}

func NewOTLPGRPCClient(options ...otlptracegrpc.Option) otlptrace.Client {
	return otlptracegrpc.NewClient(options...)
}

func NewOTLPHTTPClient(options ...otlptracehttp.Option) otlptrace.Client {
	return otlptracehttp.NewClient(options...)
}
