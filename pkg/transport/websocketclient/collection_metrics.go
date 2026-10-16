package websocketclient

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	metric "go.opentelemetry.io/otel/metric"
)

type collectionMetricConfig struct {
	provider metric.MeterProvider
	name     string
}

// CollectionOption configures bounded collection OTel instruments without owning
// a provider, exporter, reader or alternate instrumentation API.
type CollectionOption func(*collectionMetricConfig)

// WithCollectionMeterProvider uses the application's provider; nil uses global.
func WithCollectionMeterProvider(p metric.MeterProvider) CollectionOption {
	return func(c *collectionMetricConfig) { c.provider = p }
}

// WithCollectionName assigns a bounded static role. Never use provider data or
// account/credential/URL values. Names sharing a stream aggregate counters;
// gauges show the last observed collection for those dimensions.
func WithCollectionName(name string) CollectionOption {
	return func(c *collectionMetricConfig) { c.name = name }
}

type collectionMetrics struct {
	counters   []metric.Int64Counter
	gauges     []metric.Int64Gauge
	last       [8]uint64
	addOpts    []metric.AddOption
	recordOpts []metric.RecordOption
}

func newCollectionMetrics(kind string, options []CollectionOption) *collectionMetrics {
	c := collectionMetricConfig{}
	for _, option := range options {
		option(&c)
	}
	provider := c.provider
	if provider == nil {
		provider = otel.GetMeterProvider()
	}
	if c.name == "" {
		c.name = kind
	}
	meter := provider.Meter(DefaultMetricsScope)
	attrs := metric.WithAttributes(attribute.String("stage", "websocket."+kind), attribute.String("channel", "websocket"), attribute.String("name", c.name))
	m := &collectionMetrics{addOpts: []metric.AddOption{attrs}, recordOpts: []metric.RecordOption{attrs}}
	names := []string{"enqueued", "dequeued", "dropped", "coalesced", "rejected_closed", "discarded"}
	if kind == "buffer" {
		names = []string{"pushed", "inserted", "replaced", "drained", "evicted", "discarded", "rejected"}
	}
	for _, name := range names {
		instrument, err := meter.Int64Counter("wsx."+kind+"."+name, metric.WithUnit("{item}"))
		if err != nil {
			return nil
		}
		m.counters = append(m.counters, instrument)
	}
	for _, desc := range []struct{ name, unit string }{{"depth", "{item}"}, {"capacity", "{item}"}, {"bytes", "By"}, {"byte_capacity", "By"}, {"high_water", "{item}"}, {"oldest_enqueued_unix_nano", "ns"}} {
		instrument, err := meter.Int64Gauge("wsx."+kind+"."+desc.name, metric.WithUnit(desc.unit))
		if err != nil {
			return nil
		}
		m.gauges = append(m.gauges, instrument)
	}
	return m
}

// Mutations call record under the collection's existing lock. Deltas bridge its
// native monotonic operation counters to OTel; there is no retained time series.
func (m *collectionMetrics) record(totals [8]uint64, values [8]int64) {
	ctx := context.Background()
	for i, counter := range m.counters {
		delta := totals[i] - m.last[i]
		counter.Add(ctx, int64(delta), m.addOpts...)
		m.last[i] = totals[i]
	}
	for i, gauge := range m.gauges {
		gauge.Record(ctx, values[i], m.recordOpts...)
	}
}
