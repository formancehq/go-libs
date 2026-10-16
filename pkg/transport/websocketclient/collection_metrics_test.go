package websocketclient

import (
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	observemetrics "github.com/formancehq/go-libs/v5/pkg/observe/metrics"
)

func TestCollectionsUseApplicationProviderAndLocalExportCollection(t *testing.T) {
	exporter := observemetrics.NewInMemoryExporterDecorator(nil)
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(time.Hour))))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	injected, err := observemetrics.NewPrefixedMeterProvider(provider, "test.collection")
	if err != nil {
		t.Fatal(err)
	}
	q := NewEventQueue[int](1, EventQueueDrop, WithCollectionMeterProvider(injected), WithCollectionName("events"))
	if !q.TryPush(Event[int]{Value: 1}) || q.TryPush(Event[int]{Value: 2}) {
		t.Fatal("admission changed")
	}
	if _, ok := q.Receive(t.Context()); !ok {
		t.Fatal("dequeue changed")
	}
	q.Close()
	q.TryPush(Event[int]{Value: 3})
	b := NewKeyedBuffer[int](1, WithCollectionMeterProvider(injected), WithCollectionName("buffer"))
	b.Push("a", 1)
	b.Push("a", 2)
	b.Push("b", 3)
	b.Drain(t.Context(), 1)
	if err := exporter.ForceFlush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if exporter.GetMetrics() != nil {
		t.Fatal("exporter flush unexpectedly collected SDK instruments")
	}
	if err := provider.ForceFlush(t.Context()); err != nil {
		t.Fatal(err)
	}
	snapshot := exporter.GetMetrics()
	if snapshot == nil {
		t.Fatal("provider did not collect")
	}
	want := map[string]int64{"events.enqueued": 1, "events.dequeued": 1, "events.dropped": 1, "events.rejected_closed": 1, "buffer.pushed": 3, "buffer.inserted": 2, "buffer.replaced": 1, "buffer.evicted": 1, "buffer.drained": 1}
	seen := map[string]bool{}
	depths := map[string]bool{}
	for _, scope := range snapshot.ScopeMetrics {
		for _, m := range scope.Metrics {
			for suffix, value := range want {
				if m.Name == "test.collection.wsx."+suffix {
					sum, ok := m.Data.(metricdata.Sum[int64])
					if !ok || len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != value {
						t.Fatalf("%s=%v", m.Name, m.Data)
					}
					seen[suffix] = true
				}
			}
			if m.Name == "test.collection.wsx.events.depth" || m.Name == "test.collection.wsx.buffer.depth" {
				depths[m.Name] = true
				gauge, ok := m.Data.(metricdata.Gauge[int64])
				if !ok || len(gauge.DataPoints) != 1 || gauge.DataPoints[0].Value != 0 {
					t.Fatalf("observed empty depth missing: %s", m.Name)
				}
			}
		}
	}
	if len(depths) != 2 {
		t.Fatalf("missing observed depth gauges: %v", depths)
	}
	for suffix := range want {
		if !seen[suffix] {
			t.Fatalf("missing %s", suffix)
		}
	}
}
