package metrics

import (
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func NewStdoutExporter() (sdkmetric.Exporter, error) {
	return stdoutmetric.New()
}
