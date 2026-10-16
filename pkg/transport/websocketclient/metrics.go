package websocketclient

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

// DefaultMetricsScope is the instrumentation scope used when a Dialer
// is built without one.
const DefaultMetricsScope = "github.com/formancehq/go-libs/v5/pkg/transport/websocketclient"

// Dial outcomes, recorded as an attribute. A closed set, so the attribute stays
// low-cardinality: a dial error carries a provider hostname and a transport reason,
// neither of which belongs in a metric dimension.
const (
	outcomeOK    = "ok"
	outcomeError = "error"
)

// metrics holds the dial instruments. A nil *metrics records nothing, which
// recordDial's first line relies on: instrument construction can fail, and a
// caller must not lose its connection over a telemetry problem.
type metrics struct {
	duration otelmetric.Float64Histogram
	attempts otelmetric.Int64Counter
}

// newMetrics builds the instruments, returning nil if either fails so a dialer
// records all of it or none rather than a confusing half.
func newMetrics(scope string) *metrics {
	if scope == "" {
		scope = DefaultMetricsScope
	}
	meter := otel.Meter(scope)

	duration, err := meter.Float64Histogram("wsx.dial.duration",
		otelmetric.WithDescription("Duration of a WebSocket dial, including the handshake"),
		otelmetric.WithUnit("s"))
	if err != nil {
		return nil
	}
	attempts, err := meter.Int64Counter("wsx.dial.attempts",
		otelmetric.WithDescription("Completed WebSocket dials, by outcome"))
	if err != nil {
		return nil
	}

	return &metrics{duration: duration, attempts: attempts}
}

// recordDial records one completed dial. Reconnect counting stays with the
// caller, which owns the connection lifetime and retry loop.
func (m *metrics) recordDial(ctx context.Context, took time.Duration, err error) {
	if m == nil {
		return
	}
	outcome := outcomeOK
	if err != nil {
		outcome = outcomeError
	}
	attrs := otelmetric.WithAttributes(attribute.String("outcome", outcome))
	m.attempts.Add(ctx, 1, attrs)
	m.duration.Record(ctx, took.Seconds(), attrs)
}
