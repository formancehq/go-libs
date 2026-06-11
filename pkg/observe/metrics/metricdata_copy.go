package metrics

import (
	"slices"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// copyResourceMetrics returns a deep copy of rm.
//
// The OTel SDK readers (notably sdkmetric.PeriodicReader) pool the
// *metricdata.ResourceMetrics passed to Exporter.Export and reuse it for
// subsequent collections, so every slice reachable from it must be copied
// before the data can be retained past the Export call.
//
// attribute.Set, *resource.Resource and instrumentation.Scope are immutable
// once built and are safe to share.
func copyResourceMetrics(rm *metricdata.ResourceMetrics) *metricdata.ResourceMetrics {
	if rm == nil {
		return nil
	}

	out := &metricdata.ResourceMetrics{
		Resource:     rm.Resource,
		ScopeMetrics: make([]metricdata.ScopeMetrics, len(rm.ScopeMetrics)),
	}
	for i, sm := range rm.ScopeMetrics {
		out.ScopeMetrics[i] = metricdata.ScopeMetrics{
			Scope:   sm.Scope,
			Metrics: make([]metricdata.Metrics, len(sm.Metrics)),
		}
		for j, m := range sm.Metrics {
			m.Data = copyAggregation(m.Data)
			out.ScopeMetrics[i].Metrics[j] = m
		}
	}

	return out
}

// copyAggregation deep copies any aggregation type the SDK can produce.
// Unknown types are returned as-is.
func copyAggregation(agg metricdata.Aggregation) metricdata.Aggregation {
	switch data := agg.(type) {
	case metricdata.Gauge[int64]:
		data.DataPoints = copyDataPoints(data.DataPoints)
		return data
	case metricdata.Gauge[float64]:
		data.DataPoints = copyDataPoints(data.DataPoints)
		return data
	case metricdata.Sum[int64]:
		data.DataPoints = copyDataPoints(data.DataPoints)
		return data
	case metricdata.Sum[float64]:
		data.DataPoints = copyDataPoints(data.DataPoints)
		return data
	case metricdata.Histogram[int64]:
		data.DataPoints = copyHistogramDataPoints(data.DataPoints)
		return data
	case metricdata.Histogram[float64]:
		data.DataPoints = copyHistogramDataPoints(data.DataPoints)
		return data
	case metricdata.ExponentialHistogram[int64]:
		data.DataPoints = copyExponentialHistogramDataPoints(data.DataPoints)
		return data
	case metricdata.ExponentialHistogram[float64]:
		data.DataPoints = copyExponentialHistogramDataPoints(data.DataPoints)
		return data
	case metricdata.Summary:
		data.DataPoints = copySummaryDataPoints(data.DataPoints)
		return data
	default:
		return agg
	}
}

func copyDataPoints[N int64 | float64](dps []metricdata.DataPoint[N]) []metricdata.DataPoint[N] {
	out := slices.Clone(dps)
	for i := range out {
		out[i].Exemplars = copyExemplars(out[i].Exemplars)
	}

	return out
}

func copyHistogramDataPoints[N int64 | float64](dps []metricdata.HistogramDataPoint[N]) []metricdata.HistogramDataPoint[N] {
	out := slices.Clone(dps)
	for i := range out {
		out[i].Bounds = slices.Clone(out[i].Bounds)
		out[i].BucketCounts = slices.Clone(out[i].BucketCounts)
		out[i].Exemplars = copyExemplars(out[i].Exemplars)
	}

	return out
}

func copyExponentialHistogramDataPoints[N int64 | float64](dps []metricdata.ExponentialHistogramDataPoint[N]) []metricdata.ExponentialHistogramDataPoint[N] {
	out := slices.Clone(dps)
	for i := range out {
		out[i].PositiveBucket.Counts = slices.Clone(out[i].PositiveBucket.Counts)
		out[i].NegativeBucket.Counts = slices.Clone(out[i].NegativeBucket.Counts)
		out[i].Exemplars = copyExemplars(out[i].Exemplars)
	}

	return out
}

func copySummaryDataPoints(dps []metricdata.SummaryDataPoint) []metricdata.SummaryDataPoint {
	out := slices.Clone(dps)
	for i := range out {
		out[i].QuantileValues = slices.Clone(out[i].QuantileValues)
	}

	return out
}

func copyExemplars[N int64 | float64](exemplars []metricdata.Exemplar[N]) []metricdata.Exemplar[N] {
	out := slices.Clone(exemplars)
	for i := range out {
		out[i].FilteredAttributes = slices.Clone(out[i].FilteredAttributes)
		out[i].SpanID = slices.Clone(out[i].SpanID)
		out[i].TraceID = slices.Clone(out[i].TraceID)
	}

	return out
}
