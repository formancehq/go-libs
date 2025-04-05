package otlpmetrics

import (
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestLoadStdoutMetricsProvider(t *testing.T) {
	exporter, err := LoadStdoutMetricsProvider()
	require.NoError(t, err, "La création de l'exportateur ne devrait pas échouer")
	require.NotNil(t, exporter, "L'exportateur ne devrait pas être nil")
	require.Implements(t, (*sdkmetric.Exporter)(nil), exporter, "L'exportateur devrait implémenter l'interface sdkmetric.Exporter")
}

func TestStdoutMetricsModule(t *testing.T) {
	module := StdoutMetricsModule()
	require.NotNil(t, module, "Le module ne devrait pas être nil")
}
