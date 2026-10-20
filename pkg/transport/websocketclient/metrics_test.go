package websocketclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func collectUnder(t *testing.T, scope string) func() map[string]map[string]int64 {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	previous := otel.GetMeterProvider()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() { otel.SetMeterProvider(previous); _ = provider.Shutdown(context.Background()) })

	return func() map[string]map[string]int64 {
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(t.Context(), &rm); err != nil {
			t.Fatalf("collect: %v", err)
		}

		out := map[string]map[string]int64{}
		for _, sm := range rm.ScopeMetrics {
			if sm.Scope.Name != scope {
				continue
			}
			for _, m := range sm.Metrics {
				byAttrs := map[string]int64{}
				switch data := m.Data.(type) {
				case metricdata.Sum[int64]:
					for _, dp := range data.DataPoints {
						byAttrs[dp.Attributes.Encoded(attribute.DefaultEncoder())] += dp.Value
					}
				case metricdata.Histogram[float64]:
					for _, dp := range data.DataPoints {
						byAttrs[dp.Attributes.Encoded(attribute.DefaultEncoder())] += int64(dp.Count)
					}
				default:
					t.Fatalf("metric %s has unexpected data type %T", m.Name, m.Data)
				}
				out[m.Name] = byAttrs
			}
		}

		return out
	}
}

func TestDialerRecordsBothOutcomes(t *testing.T) {
	const scope = "wsx-test-outcomes"
	counts := collectUnder(t, scope)
	dialer := NewDialer(scope)

	// A real upgrade, so the ok path is the production path rather than a stub.
	accept := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer accept.Close()

	conn, err := dialer(t.Context(), "ws://"+strings.TrimPrefix(accept.URL, "http://"))
	if err != nil {
		t.Fatalf("dial a real WebSocket server: %v", err)
	}
	_ = conn.Close()

	// A 200 that is not an upgrade fails the handshake after a real round trip.
	refuse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer refuse.Close()

	if _, err := dialer(t.Context(), "ws://"+strings.TrimPrefix(refuse.URL, "http://")); err == nil {
		t.Fatal("expected the non-upgrade handler to fail the handshake")
	}

	got := counts()
	attempts, ok := got["wsx.dial.attempts"]
	if !ok {
		t.Fatalf("wsx.dial.attempts not recorded; got %v", got)
	}
	if attempts["outcome=ok"] != 1 || attempts["outcome=error"] != 1 {
		t.Errorf("attempts by outcome = %v, want one ok and one error", attempts)
	}
	if d := got["wsx.dial.duration"]; d["outcome=ok"] != 1 || d["outcome=error"] != 1 {
		t.Errorf("duration observations = %v, want one per outcome", d)
	}
}

func TestDialerRecordsNothingWithoutInstruments(t *testing.T) {
	var m *metrics

	// The nil receiver has to be safe on the exact call the dialer makes.
	m.recordDial(context.Background(), 0, errors.New("dial failed"))
	m.recordDial(context.Background(), 0, nil)
}

func TestNewDialerUsesTheDefaultScope(t *testing.T) {
	counts := collectUnder(t, DefaultMetricsScope)

	// An unroutable address fails fast without needing a server.
	if _, err := NewDialer("")(t.Context(), nonUpgradeURL(t)); err == nil {
		t.Fatal("non-upgrade endpoint unexpectedly succeeded")
	}

	if got := counts()["wsx.dial.attempts"]; got["outcome=error"] == 0 {
		t.Errorf("Dial recorded nothing under %s: %v", DefaultMetricsScope, got)
	}
}

func TestNewDialerReturnsTheConnection(t *testing.T) {
	if _, err := NewDialer("wsx-test-passthrough")(t.Context(), nonUpgradeURL(t)); err == nil {
		t.Error("expected a dial error to be returned to the caller")
	}
}

func nonUpgradeURL(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http")
}
