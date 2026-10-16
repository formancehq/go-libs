package websocketclient

import (
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestSupervisorEmitsOnApplicationProviderWithoutRecorder(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	monitor := NewSupervisorMonitor(WithSupervisorMeterProvider(provider), WithSupervisorName("feed"))
	attempt := monitor.attempt()
	attempt.frameRead(12, t.Context())
	attempt.handlingStarted(t.Context())
	attempt.handlingFinished(time.Now().Add(-time.Second), 12, t.Context())
	attempt.frameRead(3, t.Context())
	attempt.attemptEnded(t.Context())
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &rm); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			seen[m.Name] = true
			if m.Name == "wsx.frames.handling.duration" && m.Unit != "s" {
				t.Fatal("duration unit missing")
			}
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
				for _, point := range sum.DataPoints {
					want := int64(1)
					switch m.Name {
					case "wsx.frames.read":
						want = 2
					case "wsx.frames.read.bytes":
						want = 15
					}
					if point.Value != want {
						t.Fatalf("%s=%d want%d", m.Name, point.Value, want)
					}
				}
			}
			if gauge, ok := m.Data.(metricdata.Gauge[int64]); ok {
				for _, point := range gauge.DataPoints {
					want := int64(0)
					if m.Name == "wsx.frames.queue.high_water" || m.Name == "wsx.frames.handling.high_water" {
						want = 1
					}
					if point.Value != want {
						t.Fatalf("%s=%d", m.Name, point.Value)
					}
				}
			}
		}
	}
	for _, name := range []string{"wsx.frames.read", "wsx.frames.handled", "wsx.frames.abandoned", "wsx.frames.read.bytes", "wsx.frames.handling.duration", "wsx.frames.queue.depth", "wsx.frames.handling.inflight", "wsx.frames.queue.high_water", "wsx.frames.handling.high_water"} {
		if !seen[name] {
			t.Fatalf("missing %s", name)
		}
	}
}
