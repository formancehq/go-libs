package metrics

import (
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/embedded"
)

// NewRenamingMeterProvider wraps inner so that every instrument created
// through the meters it hands out is exported under
// [TransformName](name, naming, prefix). The naming and prefix are validated
// with [ParseNaming] and [ParsePrefix], so "" and [NoPrefix] both disable the
// namespace. When the policy is the identity (otel naming, no prefix), inner
// is returned unchanged.
//
// Only the instrument constructors are intercepted: once an instrument is
// created, values are recorded directly on the underlying SDK instrument.
//
// Install the result as the service's injected provider, not as the global
// one: OpenTelemetry instrumentation libraries (Go runtime, host, otelhttp,
// otelgrpc) read the global provider by default, and their
// semantic-convention names must stay as upstream defines them. A library
// explicitly handed the injected provider (a Temporal client, an httpclient
// RetryConfig.MeterProvider) is renamed like the service's own instruments.
//
// SDK views see the renamed names: a view matching on instrument name must
// use [TransformName]. When renaming applies, the result is no longer a
// *sdkmetric.MeterProvider.
func NewRenamingMeterProvider(inner metric.MeterProvider, naming Naming, prefix string) (metric.MeterProvider, error) {
	naming, err := ParseNaming(string(naming))
	if err != nil {
		return nil, err
	}
	prefix, err = ParsePrefix(prefix)
	if err != nil {
		return nil, err
	}
	if naming == NamingOTel && prefix == "" {
		return inner, nil
	}

	return &renamingMeterProvider{inner: inner, naming: naming, prefix: prefix}, nil
}

type renamingMeterProvider struct {
	embedded.MeterProvider

	inner  metric.MeterProvider
	naming Naming
	prefix string
}

func (p *renamingMeterProvider) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	return &renamingMeter{Meter: p.inner.Meter(name, opts...), naming: p.naming, prefix: p.prefix}
}

// renamingMeter rewrites the name passed to every instrument constructor and
// forwards the rest (RegisterCallback, the embedded marker) to the wrapped
// meter. Because metric.Meter is embedded, a constructor added by a future
// OpenTelemetry release would pass through unrenamed;
// TestRenamingMeterOverridesEveryConstructor fails when that happens.
type renamingMeter struct {
	metric.Meter

	naming Naming
	prefix string
}

func (m *renamingMeter) rewrite(name string) string {
	return TransformName(name, m.naming, m.prefix)
}

func (m *renamingMeter) Int64Counter(name string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	return m.Meter.Int64Counter(m.rewrite(name), opts...)
}

func (m *renamingMeter) Int64UpDownCounter(name string, opts ...metric.Int64UpDownCounterOption) (metric.Int64UpDownCounter, error) {
	return m.Meter.Int64UpDownCounter(m.rewrite(name), opts...)
}

func (m *renamingMeter) Int64Histogram(name string, opts ...metric.Int64HistogramOption) (metric.Int64Histogram, error) {
	return m.Meter.Int64Histogram(m.rewrite(name), opts...)
}

func (m *renamingMeter) Int64Gauge(name string, opts ...metric.Int64GaugeOption) (metric.Int64Gauge, error) {
	return m.Meter.Int64Gauge(m.rewrite(name), opts...)
}

func (m *renamingMeter) Int64ObservableCounter(name string, opts ...metric.Int64ObservableCounterOption) (metric.Int64ObservableCounter, error) {
	return m.Meter.Int64ObservableCounter(m.rewrite(name), opts...)
}

func (m *renamingMeter) Int64ObservableUpDownCounter(name string, opts ...metric.Int64ObservableUpDownCounterOption) (metric.Int64ObservableUpDownCounter, error) {
	return m.Meter.Int64ObservableUpDownCounter(m.rewrite(name), opts...)
}

func (m *renamingMeter) Int64ObservableGauge(name string, opts ...metric.Int64ObservableGaugeOption) (metric.Int64ObservableGauge, error) {
	return m.Meter.Int64ObservableGauge(m.rewrite(name), opts...)
}

func (m *renamingMeter) Float64Counter(name string, opts ...metric.Float64CounterOption) (metric.Float64Counter, error) {
	return m.Meter.Float64Counter(m.rewrite(name), opts...)
}

func (m *renamingMeter) Float64UpDownCounter(name string, opts ...metric.Float64UpDownCounterOption) (metric.Float64UpDownCounter, error) {
	return m.Meter.Float64UpDownCounter(m.rewrite(name), opts...)
}

func (m *renamingMeter) Float64Histogram(name string, opts ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	return m.Meter.Float64Histogram(m.rewrite(name), opts...)
}

func (m *renamingMeter) Float64Gauge(name string, opts ...metric.Float64GaugeOption) (metric.Float64Gauge, error) {
	return m.Meter.Float64Gauge(m.rewrite(name), opts...)
}

func (m *renamingMeter) Float64ObservableCounter(name string, opts ...metric.Float64ObservableCounterOption) (metric.Float64ObservableCounter, error) {
	return m.Meter.Float64ObservableCounter(m.rewrite(name), opts...)
}

func (m *renamingMeter) Float64ObservableUpDownCounter(name string, opts ...metric.Float64ObservableUpDownCounterOption) (metric.Float64ObservableUpDownCounter, error) {
	return m.Meter.Float64ObservableUpDownCounter(m.rewrite(name), opts...)
}

func (m *renamingMeter) Float64ObservableGauge(name string, opts ...metric.Float64ObservableGaugeOption) (metric.Float64ObservableGauge, error) {
	return m.Meter.Float64ObservableGauge(m.rewrite(name), opts...)
}
