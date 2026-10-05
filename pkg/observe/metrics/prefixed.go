package metrics

import (
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/embedded"
)

// NewPrefixedMeterProvider wraps inner so that every instrument created
// through the meters it hands out is exported under [PrefixedName](name,
// prefix). The prefix is validated with [ParsePrefix]; when it is "" or
// [NoPrefix], inner is returned unchanged.
//
// Only the instrument constructors are intercepted: once an instrument is
// created, values are recorded directly on the underlying SDK instrument.
//
// Install the result as the service's injected provider, not as the global
// one: OpenTelemetry instrumentation libraries (Go runtime, host, otelhttp,
// otelgrpc) read the global provider by default, and their
// semantic-convention names must stay as upstream defines them. A library
// explicitly handed the injected provider (a Temporal client, an httpclient
// RetryConfig.MeterProvider) is prefixed like the service's own instruments.
//
// SDK views see the prefixed names: a view matching on instrument name must
// use [PrefixedName]. When a prefix applies, the result is no longer a
// *sdkmetric.MeterProvider.
func NewPrefixedMeterProvider(inner metric.MeterProvider, prefix string) (metric.MeterProvider, error) {
	prefix, err := ParsePrefix(prefix)
	if err != nil {
		return nil, err
	}
	if prefix == "" {
		return inner, nil
	}

	return &prefixedMeterProvider{inner: inner, prefix: prefix}, nil
}

type prefixedMeterProvider struct {
	embedded.MeterProvider

	inner  metric.MeterProvider
	prefix string
}

func (p *prefixedMeterProvider) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	return &prefixedMeter{Meter: p.inner.Meter(name, opts...), prefix: p.prefix}
}

// prefixedMeter prefixes the name passed to every instrument constructor and
// forwards the rest (RegisterCallback, the embedded marker) to the wrapped
// meter. Because metric.Meter is embedded, a constructor added by a future
// OpenTelemetry release would pass through unprefixed;
// TestPrefixedMeterOverridesEveryConstructor fails when that happens.
type prefixedMeter struct {
	metric.Meter

	prefix string
}

func (m *prefixedMeter) rewrite(name string) string {
	return PrefixedName(name, m.prefix)
}

func (m *prefixedMeter) Int64Counter(name string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	return m.Meter.Int64Counter(m.rewrite(name), opts...)
}

func (m *prefixedMeter) Int64UpDownCounter(name string, opts ...metric.Int64UpDownCounterOption) (metric.Int64UpDownCounter, error) {
	return m.Meter.Int64UpDownCounter(m.rewrite(name), opts...)
}

func (m *prefixedMeter) Int64Histogram(name string, opts ...metric.Int64HistogramOption) (metric.Int64Histogram, error) {
	return m.Meter.Int64Histogram(m.rewrite(name), opts...)
}

func (m *prefixedMeter) Int64Gauge(name string, opts ...metric.Int64GaugeOption) (metric.Int64Gauge, error) {
	return m.Meter.Int64Gauge(m.rewrite(name), opts...)
}

func (m *prefixedMeter) Int64ObservableCounter(name string, opts ...metric.Int64ObservableCounterOption) (metric.Int64ObservableCounter, error) {
	return m.Meter.Int64ObservableCounter(m.rewrite(name), opts...)
}

func (m *prefixedMeter) Int64ObservableUpDownCounter(name string, opts ...metric.Int64ObservableUpDownCounterOption) (metric.Int64ObservableUpDownCounter, error) {
	return m.Meter.Int64ObservableUpDownCounter(m.rewrite(name), opts...)
}

func (m *prefixedMeter) Int64ObservableGauge(name string, opts ...metric.Int64ObservableGaugeOption) (metric.Int64ObservableGauge, error) {
	return m.Meter.Int64ObservableGauge(m.rewrite(name), opts...)
}

func (m *prefixedMeter) Float64Counter(name string, opts ...metric.Float64CounterOption) (metric.Float64Counter, error) {
	return m.Meter.Float64Counter(m.rewrite(name), opts...)
}

func (m *prefixedMeter) Float64UpDownCounter(name string, opts ...metric.Float64UpDownCounterOption) (metric.Float64UpDownCounter, error) {
	return m.Meter.Float64UpDownCounter(m.rewrite(name), opts...)
}

func (m *prefixedMeter) Float64Histogram(name string, opts ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	return m.Meter.Float64Histogram(m.rewrite(name), opts...)
}

func (m *prefixedMeter) Float64Gauge(name string, opts ...metric.Float64GaugeOption) (metric.Float64Gauge, error) {
	return m.Meter.Float64Gauge(m.rewrite(name), opts...)
}

func (m *prefixedMeter) Float64ObservableCounter(name string, opts ...metric.Float64ObservableCounterOption) (metric.Float64ObservableCounter, error) {
	return m.Meter.Float64ObservableCounter(m.rewrite(name), opts...)
}

func (m *prefixedMeter) Float64ObservableUpDownCounter(name string, opts ...metric.Float64ObservableUpDownCounterOption) (metric.Float64ObservableUpDownCounter, error) {
	return m.Meter.Float64ObservableUpDownCounter(m.rewrite(name), opts...)
}

func (m *prefixedMeter) Float64ObservableGauge(name string, opts ...metric.Float64ObservableGaugeOption) (metric.Float64ObservableGauge, error) {
	return m.Meter.Float64ObservableGauge(m.rewrite(name), opts...)
}
