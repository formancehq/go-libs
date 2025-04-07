package otlpmetrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
)

func TestInMemoryExporter_Temporality(t *testing.T) {
	// Test with nil exporter
	exporter := &InMemoryExporter{}

	temporality := exporter.Temporality(sdkmetric.InstrumentKindCounter)
	require.Equal(t, metricdata.CumulativeTemporality, temporality, "La temporalité par défaut devrait être cumulative")

	// Test with mock exporter
	mockExporter := &mockExporter{
		temporality: metricdata.DeltaTemporality,
	}
	exporterWithMock := &InMemoryExporter{
		exp: mockExporter,
	}

	temporalityWithMock := exporterWithMock.Temporality(sdkmetric.InstrumentKindCounter)
	require.Equal(t, metricdata.DeltaTemporality, temporalityWithMock, "La temporalité devrait être celle du mock")
}

func TestInMemoryExporter_Aggregation(t *testing.T) {
	// Test with nil exporter
	exporter := &InMemoryExporter{}

	aggregation := exporter.Aggregation(sdkmetric.InstrumentKindCounter)
	require.NotNil(t, aggregation, "L'agrégation par défaut ne devrait pas être nil")

	// Test with mock exporter
	mockExporter := &mockExporter{}
	exporterWithMock := &InMemoryExporter{
		exp: mockExporter,
	}

	aggregationWithMock := exporterWithMock.Aggregation(sdkmetric.InstrumentKindCounter)
	require.NotNil(t, aggregationWithMock, "L'agrégation ne devrait pas être nil")
}

func TestInMemoryExporter_ForceFlush(t *testing.T) {
	// Test with nil exporter
	exporter := &InMemoryExporter{}

	err := exporter.ForceFlush(context.Background())
	require.NoError(t, err, "ForceFlush ne devrait pas échouer avec un exportateur nil")

	// Test with mock exporter
	mockExporter := &mockExporter{}
	exporterWithMock := &InMemoryExporter{
		exp: mockExporter,
	}

	err = exporterWithMock.ForceFlush(context.Background())
	require.NoError(t, err, "ForceFlush ne devrait pas échouer avec un mock")
	require.True(t, mockExporter.forceFlushCalled, "ForceFlush devrait être appelé sur le mock")
}

func TestInMemoryExporter_Shutdown(t *testing.T) {
	// Test with nil exporter
	exporter := &InMemoryExporter{}

	err := exporter.Shutdown(context.Background())
	require.NoError(t, err, "Shutdown ne devrait pas échouer avec un exportateur nil")

	// Test with mock exporter
	mockExporter := &mockExporter{}
	exporterWithMock := &InMemoryExporter{
		exp: mockExporter,
	}

	err = exporterWithMock.Shutdown(context.Background())
	require.NoError(t, err, "Shutdown ne devrait pas échouer avec un mock")
	require.True(t, mockExporter.shutdownCalled, "Shutdown devrait être appelé sur le mock")
}

func TestInMemoryExporter_Export(t *testing.T) {
	// Test with canceled context
	exporter := &InMemoryExporter{
		exp: &mockExporter{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := exporter.Export(ctx, &metricdata.ResourceMetrics{})
	require.Error(t, err, "Export devrait échouer avec un contexte annulé")

	// Test with valid context
	mockExporter := &mockExporter{}
	exporterWithMock := &InMemoryExporter{
		exp: mockExporter,
	}

	metrics := &metricdata.ResourceMetrics{
		Resource: resource.NewSchemaless(),
	}

	err = exporterWithMock.Export(context.Background(), metrics)
	require.NoError(t, err, "Export ne devrait pas échouer")
	require.True(t, mockExporter.exportCalled, "Export devrait être appelé sur le mock")
	require.NotNil(t, exporterWithMock.metrics, "Les métriques devraient être stockées")
}

func TestInMemoryExporter_GetMetrics(t *testing.T) {
	exporter := &InMemoryExporter{}

	// Test with nil metrics
	metrics := exporter.GetMetrics()
	require.Nil(t, metrics, "GetMetrics devrait retourner nil si aucune métrique n'est stockée")

	// Test with metrics
	testMetrics := &metricdata.ResourceMetrics{
		Resource: resource.NewSchemaless(),
	}
	exporter.metrics = testMetrics

	metrics = exporter.GetMetrics()
	require.Same(t, testMetrics, metrics, "GetMetrics devrait retourner les métriques stockées")
}

func TestNewInMemoryExporterDecorator(t *testing.T) {
	mockExporter := &mockExporter{}

	exporter := NewInMemoryExporterDecorator(mockExporter)
	require.NotNil(t, exporter, "L'exportateur ne devrait pas être nil")
	require.Same(t, mockExporter, exporter.exp, "L'exportateur sous-jacent devrait être correctement défini")
}

func TestNewInMemoryExporterHandler(t *testing.T) {
	mockExporter := &mockExporter{}
	exporter := &InMemoryExporter{
		exp: mockExporter,
		metrics: &metricdata.ResourceMetrics{
			Resource: resource.NewSchemaless(),
		},
	}

	meterProvider := sdkmetric.NewMeterProvider()

	handler := NewInMemoryExporterHandler(meterProvider, exporter)
	require.NotNil(t, handler, "Le gestionnaire ne devrait pas être nil")

	// Test the handler
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	// This should not panic
	handler(w, req)

	require.Equal(t, http.StatusOK, w.Code, "Le code de statut devrait être 200")

	// Vérifier que la réponse est du JSON valide
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err, "La réponse devrait être du JSON valide")
}

// Mock exporter pour les tests
type mockExporter struct {
	temporality      metricdata.Temporality
	aggregation      sdkmetric.Aggregation
	forceFlushCalled bool
	shutdownCalled   bool
	exportCalled     bool
}

func (m *mockExporter) Temporality(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	return m.temporality
}

func (m *mockExporter) Aggregation(kind sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	if m.aggregation != nil {
		return m.aggregation
	}
	return sdkmetric.DefaultAggregationSelector(kind)
}

func (m *mockExporter) ForceFlush(ctx context.Context) error {
	m.forceFlushCalled = true
	return nil
}

func (m *mockExporter) Shutdown(ctx context.Context) error {
	m.shutdownCalled = true
	return nil
}

func (m *mockExporter) Export(ctx context.Context, data *metricdata.ResourceMetrics) error {
	m.exportCalled = true
	return nil
}
