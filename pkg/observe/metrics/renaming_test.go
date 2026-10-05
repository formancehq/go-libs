package metrics_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/formancehq/go-libs/v5/pkg/observe/metrics"
)

// collectInstrumentNames wraps a fresh SDK provider with the given policy,
// lets register create instruments through it and returns the names the SDK
// exports.
func collectInstrumentNames(t *testing.T, naming metrics.Naming, prefix string, register func(otelmetric.MeterProvider)) []string {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})

	mp, err := metrics.NewRenamingMeterProvider(provider, naming, prefix)
	require.NoError(t, err)
	register(mp)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	var names []string
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			names = append(names, m.Name)
		}
	}

	return names
}

func registerSampleInstruments(t *testing.T) func(otelmetric.MeterProvider) {
	t.Helper()

	return func(mp otelmetric.MeterProvider) {
		register := func(meter, instrument string) {
			c, err := mp.Meter(meter).Int64Counter(instrument)
			require.NoError(t, err)
			c.Add(context.Background(), 1)
		}
		register("admission", "admission.preload.total")
		register("wal", "wal.append.save.duration")
		register("raft.node", "raft.fsm.logs_appended")
	}
}

func TestNewRenamingMeterProviderIdentityReturnsInner(t *testing.T) {
	t.Parallel()

	inner := sdkmetric.NewMeterProvider()
	t.Cleanup(func() {
		_ = inner.Shutdown(context.Background())
	})

	for _, prefix := range []string{"", metrics.NoPrefix} {
		for _, naming := range []metrics.Naming{"", metrics.NamingOTel} {
			mp, err := metrics.NewRenamingMeterProvider(inner, naming, prefix)
			require.NoError(t, err)
			require.Same(t, inner, mp)
		}
	}
}

func TestNewRenamingMeterProviderRejectsInvalidPolicy(t *testing.T) {
	t.Parallel()

	inner := sdkmetric.NewMeterProvider()
	t.Cleanup(func() {
		_ = inner.Shutdown(context.Background())
	})

	_, err := metrics.NewRenamingMeterProvider(inner, "prometheus", "")
	require.Error(t, err)

	_, err = metrics.NewRenamingMeterProvider(inner, metrics.NamingOTel, "acme..payments")
	require.Error(t, err)
}

func TestRenamingOTelNamingWithoutPrefixPreservesNames(t *testing.T) {
	t.Parallel()

	names := collectInstrumentNames(t, metrics.NamingOTel, "", registerSampleInstruments(t))

	require.ElementsMatch(t, []string{
		"admission.preload.total",
		"wal.append.save.duration",
		"raft.fsm.logs_appended",
	}, names)
}

func TestRenamingOTelNamingPrefixesEveryInstrument(t *testing.T) {
	t.Parallel()

	names := collectInstrumentNames(t, metrics.NamingOTel, "formance.ledger", registerSampleInstruments(t))

	require.ElementsMatch(t, []string{
		"formance.ledger.admission.preload.total",
		"formance.ledger.wal.append.save.duration",
		"formance.ledger.raft.fsm.logs_appended",
	}, names)
}

func TestRenamingPromNamingPrefixesEveryInstrument(t *testing.T) {
	t.Parallel()

	names := collectInstrumentNames(t, metrics.NamingProm, "formance.ledger", registerSampleInstruments(t))

	require.ElementsMatch(t, []string{
		"formance_ledger_admission_preload_total",
		"formance_ledger_wal_append_save_duration",
		"formance_ledger_raft_fsm_logs_appended",
	}, names)
}

func TestRenamingPromNamingWithoutPrefixOnlyReplacesDots(t *testing.T) {
	t.Parallel()

	names := collectInstrumentNames(t, metrics.NamingProm, metrics.NoPrefix, registerSampleInstruments(t))

	require.ElementsMatch(t, []string{
		"admission_preload_total",
		"wal_append_save_duration",
		"raft_fsm_logs_appended",
	}, names)
}

func TestRenamingCoversEveryInstrumentKind(t *testing.T) {
	t.Parallel()

	names := collectInstrumentNames(t, metrics.NamingProm, "acme.payments", func(mp otelmetric.MeterProvider) {
		m := mp.Meter("kinds")
		ctx := context.Background()

		c, err := m.Int64Counter("int.counter")
		require.NoError(t, err)
		c.Add(ctx, 1)

		ud, err := m.Int64UpDownCounter("int.updown")
		require.NoError(t, err)
		ud.Add(ctx, 1)

		h, err := m.Int64Histogram("int.hist")
		require.NoError(t, err)
		h.Record(ctx, 1)

		g, err := m.Int64Gauge("int.gauge")
		require.NoError(t, err)
		g.Record(ctx, 1)

		fc, err := m.Float64Counter("float.counter")
		require.NoError(t, err)
		fc.Add(ctx, 1)

		fud, err := m.Float64UpDownCounter("float.updown")
		require.NoError(t, err)
		fud.Add(ctx, 1)

		fh, err := m.Float64Histogram("float.hist")
		require.NoError(t, err)
		fh.Record(ctx, 1)

		fg, err := m.Float64Gauge("float.gauge")
		require.NoError(t, err)
		fg.Record(ctx, 1)

		oc, err := m.Int64ObservableCounter("int.obs_counter")
		require.NoError(t, err)
		oud, err := m.Int64ObservableUpDownCounter("int.obs_updown")
		require.NoError(t, err)
		og, err := m.Int64ObservableGauge("int.obs_gauge")
		require.NoError(t, err)
		foc, err := m.Float64ObservableCounter("float.obs_counter")
		require.NoError(t, err)
		foud, err := m.Float64ObservableUpDownCounter("float.obs_updown")
		require.NoError(t, err)
		fog, err := m.Float64ObservableGauge("float.obs_gauge")
		require.NoError(t, err)

		_, err = m.RegisterCallback(func(_ context.Context, o otelmetric.Observer) error {
			o.ObserveInt64(oc, 1)
			o.ObserveInt64(oud, 1)
			o.ObserveInt64(og, 1)
			o.ObserveFloat64(foc, 1)
			o.ObserveFloat64(foud, 1)
			o.ObserveFloat64(fog, 1)
			return nil
		}, oc, oud, og, foc, foud, fog)
		require.NoError(t, err)
	})

	require.Len(t, names, 14)
	for _, n := range names {
		require.True(t, strings.HasPrefix(n, "acme_payments_"), "instrument %q is not prefixed", n)
		require.NotContains(t, n, ".", "instrument %q still contains a dot", n)
	}
}

func TestTransformName(t *testing.T) {
	t.Parallel()

	require.Equal(t, "cache.size", metrics.TransformName("cache.size", metrics.NamingOTel, ""))
	require.Equal(t, "cache.size", metrics.TransformName("cache.size", metrics.NamingOTel, metrics.NoPrefix))
	require.Equal(t, "acme.cache.size", metrics.TransformName("cache.size", metrics.NamingOTel, "acme"))
	require.Equal(t, "cache_size", metrics.TransformName("cache.size", metrics.NamingProm, ""))
	require.Equal(t, "acme_svc_cache_size", metrics.TransformName("cache.size", metrics.NamingProm, "acme.svc"))
}

func TestParseNaming(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want metrics.Naming
		err  bool
	}{
		{in: "otel", want: metrics.NamingOTel},
		{in: "prom", want: metrics.NamingProm},
		{in: "", want: metrics.DefaultNaming},
		{in: "prometheus", err: true},
		{in: "OTEL", err: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := metrics.ParseNaming(tc.in)
			if tc.err {
				require.Error(t, err)

				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParsePrefix(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", metrics.MaxPrefixLength)
	tests := []struct {
		in   string
		want string
		err  bool
	}{
		{in: metrics.NoPrefix, want: ""},
		{in: "", want: ""},
		{in: "formance.ledger", want: "formance.ledger"},
		{in: "ledger", want: "ledger"},
		{in: "acme.payments_ledger2", want: "acme.payments_ledger2"},
		{in: long, want: long},
		{in: long + "a", err: true},
		{in: "formance.ledger.", err: true},
		{in: "formance..ledger", err: true},
		{in: "formance_.ledger", err: true},
		{in: "formance._ledger", err: true},
		{in: ".formance", err: true},
		{in: "formance_", err: true},
		{in: "1formance", err: true},
		{in: "formance-ledger", err: true}, // "-" is invalid in Prometheus names
		{in: "formance ledger", err: true},
		{in: "formance/ledger", err: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := metrics.ParsePrefix(tc.in)
			if tc.err {
				require.Error(t, err)

				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
