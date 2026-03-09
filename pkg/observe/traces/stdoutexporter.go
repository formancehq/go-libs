package traces

import (
	"os"

	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
)

func NewStdoutExporter() (*stdouttrace.Exporter, error) {
	return stdouttrace.New(
		stdouttrace.WithWriter(os.Stdout),
	)
}
