package websocketclient

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	metric "go.opentelemetry.io/otel/metric"
)

type supervisorMetricConfig struct {
	provider metric.MeterProvider
	name     string
}

// SupervisorMonitorOption configures instruments on the application's provider.
type SupervisorMonitorOption func(*supervisorMetricConfig)

// WithSupervisorMeterProvider uses the injected application provider, including
// its naming/view policy. Nil uses the global OTel provider. It creates no SDK.
func WithSupervisorMeterProvider(provider metric.MeterProvider) SupervisorMonitorOption {
	return func(c *supervisorMetricConfig) { c.provider = provider }
}

// WithSupervisorName distinguishes concurrent session monitors. Use a bounded,
// statically declared role, never a URL, credential, account ID or error text.
// Reusing a name aggregates counters; gauges describe the last observed session.
func WithSupervisorName(name string) SupervisorMonitorOption {
	return func(c *supervisorMetricConfig) { c.name = name }
}

type supervisorMetrics struct {
	readCount, handledCount, abandoned, bytes metric.Int64Counter
	duration                                  metric.Float64Histogram
	depth, inflight, queueHigh, handlingHigh  metric.Int64Gauge
	addOpts                                   []metric.AddOption
	recordOpts                                []metric.RecordOption
}

func newSupervisorMetrics(c supervisorMetricConfig) *supervisorMetrics {
	provider := c.provider
	if provider == nil {
		provider = otel.GetMeterProvider()
	}
	if c.name == "" {
		c.name = "session"
	}
	meter := provider.Meter(DefaultMetricsScope)
	attrs := metric.WithAttributes(attribute.String("stage", "websocket.frames"), attribute.String("channel", "websocket"), attribute.String("name", c.name))
	m := &supervisorMetrics{addOpts: []metric.AddOption{attrs}, recordOpts: []metric.RecordOption{attrs}}
	var err error
	m.readCount, err = meter.Int64Counter("wsx.frames.read", metric.WithUnit("{frame}"))
	if err != nil {
		return nil
	}
	m.handledCount, err = meter.Int64Counter("wsx.frames.handled", metric.WithUnit("{frame}"))
	if err != nil {
		return nil
	}
	m.abandoned, err = meter.Int64Counter("wsx.frames.abandoned", metric.WithUnit("{frame}"))
	if err != nil {
		return nil
	}
	m.bytes, err = meter.Int64Counter("wsx.frames.read.bytes", metric.WithUnit("By"))
	if err != nil {
		return nil
	}
	m.duration, err = meter.Float64Histogram("wsx.frames.handling.duration", metric.WithUnit("s"))
	if err != nil {
		return nil
	}
	m.depth, err = meter.Int64Gauge("wsx.frames.queue.depth", metric.WithUnit("{frame}"))
	if err != nil {
		return nil
	}
	m.inflight, err = meter.Int64Gauge("wsx.frames.handling.inflight", metric.WithUnit("{frame}"))
	if err != nil {
		return nil
	}
	m.queueHigh, err = meter.Int64Gauge("wsx.frames.queue.high_water", metric.WithUnit("{frame}"))
	if err != nil {
		return nil
	}
	m.handlingHigh, err = meter.Int64Gauge("wsx.frames.handling.high_water", metric.WithUnit("{frame}"))
	if err != nil {
		return nil
	}
	return m
}
func (m *supervisorMetrics) read(ctx context.Context, n int) {
	if m != nil {
		m.readCount.Add(ctx, 1, m.addOpts...)
		m.bytes.Add(ctx, int64(n), m.addOpts...)
	}
}
func (m *supervisorMetrics) handled(ctx context.Context, d time.Duration) {
	if m != nil {
		m.handledCount.Add(ctx, 1, m.addOpts...)
		m.duration.Record(ctx, d.Seconds(), m.recordOpts...)
	}
}
func (m *supervisorMetrics) abandon(ctx context.Context, n uint64) {
	if m != nil {
		m.abandoned.Add(ctx, int64(n), m.addOpts...)
	}
}
func (m *supervisorMetrics) gauges(ctx context.Context, s supervisorMonitorState) {
	if m == nil {
		return
	}
	m.depth.Record(ctx, int64(s.frameQueueDepth), m.recordOpts...)
	m.inflight.Record(ctx, int64(s.handlingInFlight), m.recordOpts...)
	m.queueHigh.Record(ctx, int64(s.frameQueueHighWater), m.recordOpts...)
	m.handlingHigh.Record(ctx, int64(s.handlingHighWater), m.recordOpts...)
}
