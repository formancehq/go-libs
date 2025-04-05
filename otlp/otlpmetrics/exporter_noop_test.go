package otlpmetrics

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
)

func TestNoOpExporter_Temporality(t *testing.T) {
	exporter := NewNoOpExporter()
	
	temporality := exporter.Temporality(sdkmetric.InstrumentKindCounter)
	require.Equal(t, metricdata.CumulativeTemporality, temporality, "La temporalité par défaut devrait être cumulative")
	
	temporality = exporter.Temporality(sdkmetric.InstrumentKindHistogram)
	require.Equal(t, metricdata.CumulativeTemporality, temporality, "La temporalité par défaut devrait être cumulative")
}

func TestNoOpExporter_Aggregation(t *testing.T) {
	exporter := NewNoOpExporter()
	
	aggregation := exporter.Aggregation(sdkmetric.InstrumentKindCounter)
	require.NotNil(t, aggregation, "L'agrégation par défaut ne devrait pas être nil")
	
	aggregation = exporter.Aggregation(sdkmetric.InstrumentKindHistogram)
	require.NotNil(t, aggregation, "L'agrégation par défaut ne devrait pas être nil")
}

func TestNoOpExporter_ForceFlush(t *testing.T) {
	exporter := NewNoOpExporter()
	
	err := exporter.ForceFlush(context.Background())
	require.NoError(t, err, "ForceFlush ne devrait pas échouer")
	
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	
	err = exporter.ForceFlush(ctx)
	require.NoError(t, err, "ForceFlush ne devrait pas échouer même avec un contexte annulé")
}

func TestNoOpExporter_Shutdown(t *testing.T) {
	exporter := NewNoOpExporter()
	
	err := exporter.Shutdown(context.Background())
	require.NoError(t, err, "Shutdown ne devrait pas échouer")
	
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	
	err = exporter.Shutdown(ctx)
	require.NoError(t, err, "Shutdown ne devrait pas échouer même avec un contexte annulé")
}

func TestNoOpExporter_Export(t *testing.T) {
	exporter := NewNoOpExporter()
	
	metrics := &metricdata.ResourceMetrics{
		Resource: resource.NewSchemaless(),
	}
	
	err := exporter.Export(context.Background(), metrics)
	require.NoError(t, err, "Export ne devrait pas échouer")
	
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	
	err = exporter.Export(ctx, metrics)
	require.NoError(t, err, "Export ne devrait pas échouer même avec un contexte annulé")
}

func TestNewNoOpExporter(t *testing.T) {
	exporter := NewNoOpExporter()
	require.NotNil(t, exporter, "L'exportateur ne devrait pas être nil")
	require.IsType(t, &NoOpExporter{}, exporter, "L'exportateur devrait être du type NoOpExporter")
}
