package performance

import (
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecorderSnapshotIsSortedAndImmutable(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)
	recorder := New(Config{LatencySampleLimit: 4})
	for _, input := range []struct {
		name    string
		channel string
	}{
		{name: "map", channel: "websocket"},
		{name: "fetch", channel: "poll"},
		{name: "fetch", channel: "generated"},
	} {
		if !recorder.Record(input.name, input.channel, Sample{At: base, Calls: 1}) {
			t.Fatalf("record %s/%s", input.name, input.channel)
		}
	}

	got := recorder.Snapshot(base.Add(time.Second))
	wantOrder := [][2]string{
		{"fetch", "generated"},
		{"fetch", "poll"},
		{"map", "websocket"},
	}
	if len(got.Stages) != len(wantOrder) {
		t.Fatalf("stages = %d, want %d", len(got.Stages), len(wantOrder))
	}
	for i, want := range wantOrder {
		if got.Stages[i].Name != want[0] || got.Stages[i].Channel != want[1] {
			t.Errorf("stage %d = %s/%s, want %s/%s", i,
				got.Stages[i].Name, got.Stages[i].Channel, want[0], want[1])
		}
	}

	got.Stages[0].Name = "changed"
	got.Stages[0].Totals.Calls = 99
	again := recorder.Snapshot(base.Add(time.Second))
	if again.Stages[0].Name != "fetch" || again.Stages[0].Totals.Calls != 1 {
		t.Fatalf("snapshot aliases recorder state: %+v", again.Stages[0])
	}
}

func TestStageTracksExactTotalsWindowsAndInFlight(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)
	stage := New(Config{}).Stage("fetch_batch", "generated")
	if !stage.AddInFlight(2) || !stage.AddInFlight(-1) || !stage.AddInFlight(3) {
		t.Fatal("valid in-flight adjustment rejected")
	}
	if stage.AddInFlight(-5) {
		t.Fatal("in-flight underflow accepted")
	}

	first := Sample{
		At: base, Duration: 10 * time.Millisecond, Calls: 1, Errors: 1,
		ItemsIn: 2, ItemsOut: 3, Transactions: 4,
	}
	second := Sample{
		At: base.Add(2 * time.Second), Duration: 20 * time.Millisecond, Calls: 2,
		ItemsIn: 5, ItemsOut: 7, Transactions: 11,
	}
	if !stage.Record(first) || !stage.Record(second) {
		t.Fatal("valid sample rejected")
	}

	got := snapshotForStage(stage, base.Add(3*time.Second))
	wantCounts := Counts{
		Samples: 2, Calls: 3, Errors: 1, ItemsIn: 7, ItemsOut: 10,
		Transactions: 15, ActiveTime: 30 * time.Millisecond,
	}
	if !got.InFlightAvailable || got.InFlight != 4 || got.HighWater != 4 {
		t.Errorf("in-flight availability/value/high-water = %t/%d/%d, want true/4/4",
			got.InFlightAvailable, got.InFlight, got.HighWater)
	}
	if got.Totals != wantCounts {
		t.Errorf("totals = %+v, want %+v", got.Totals, wantCounts)
	}
	if got.Rolling10s.Counts != wantCounts || got.Rolling60s.Counts != wantCounts {
		t.Errorf("rolling counts = %+v / %+v, want %+v",
			got.Rolling10s.Counts, got.Rolling60s.Counts, wantCounts)
	}
	if !got.Rolling10s.Rates.Available || got.Rolling10s.Rates.Elapsed != 3*time.Second {
		t.Errorf("rates = %+v", got.Rolling10s.Rates)
	}
	assertFloat(t, "calls/s", got.Rolling10s.Rates.CallsPerSecond, 1)
	assertFloat(t, "errors/s", got.Rolling10s.Rates.ErrorsPerSecond, 1.0/3.0)
	assertFloat(t, "items-in/s", got.Rolling10s.Rates.ItemsInPerSecond, 7.0/3.0)
	assertFloat(t, "items-out/s", got.Rolling10s.Rates.ItemsOutPerSecond, 10.0/3.0)
	assertFloat(t, "transactions/s", got.Rolling10s.Rates.TransactionsPerSecond, 5)
	assertFloat(t, "active seconds/s", got.Rolling10s.Rates.ActiveSecondsPerSecond, 0.01)
	if got.Latency.Samples != 2 || got.Latency.Observed != 2 ||
		got.Latency.P50 != 10*time.Millisecond || got.Latency.P95 != 20*time.Millisecond ||
		got.Latency.P99 != 20*time.Millisecond {
		t.Errorf("latency = %+v", got.Latency)
	}
}

func TestRollingWindowsExpireAtExactBoundary(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)
	stage := New(Config{}).Stage("observe", "webhook")
	for _, at := range []time.Time{
		base,
		base.Add(50 * time.Second),
		base.Add(59 * time.Second),
	} {
		if !stage.Record(Sample{At: at, Duration: time.Millisecond, Calls: 1}) {
			t.Fatalf("record at %s", at)
		}
	}

	atBoundary := snapshotForStage(stage, base.Add(60*time.Second))
	if atBoundary.Rolling10s.Counts.Calls != 2 {
		t.Errorf("10s calls at boundary = %d, want 2", atBoundary.Rolling10s.Counts.Calls)
	}
	if atBoundary.Rolling60s.Counts.Calls != 3 {
		t.Errorf("60s calls at boundary = %d, want 3", atBoundary.Rolling60s.Counts.Calls)
	}

	afterBoundary := snapshotForStage(stage, base.Add(60*time.Second+time.Nanosecond))
	if afterBoundary.Rolling60s.Counts.Calls != 3 {
		t.Errorf("60s calls within same complete-second window = %d, want 3",
			afterBoundary.Rolling60s.Counts.Calls)
	}

	afterNextBucket := snapshotForStage(stage, base.Add(61*time.Second+time.Nanosecond))
	if afterNextBucket.Rolling60s.Counts.Calls != 2 {
		t.Errorf("60s calls after next bucket = %d, want 2", afterNextBucket.Rolling60s.Counts.Calls)
	}

	afterExpiry := snapshotForStage(stage, base.Add(120*time.Second+time.Nanosecond))
	if afterExpiry.Rolling10s.Counts.Samples != 0 || afterExpiry.Rolling60s.Counts.Samples != 0 {
		t.Errorf("expired samples retained in windows: %+v / %+v",
			afterExpiry.Rolling10s.Counts, afterExpiry.Rolling60s.Counts)
	}
	if afterExpiry.Totals.Calls != 3 {
		t.Errorf("cumulative calls = %d, want 3", afterExpiry.Totals.Calls)
	}
}

func TestRateRequiresTwoCallsAndPositiveElapsedTime(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)
	stage := New(Config{}).Stage("map", "poll")
	if !stage.Record(Sample{At: base, Calls: 1}) {
		t.Fatal("first record rejected")
	}
	if got := snapshotForStage(stage, base.Add(time.Second)); got.Rolling10s.Rates.Available {
		t.Fatalf("one-call rate reported: %+v", got.Rolling10s.Rates)
	}
	if !stage.Record(Sample{At: base.Add(time.Second), Calls: 1}) {
		t.Fatal("second record rejected")
	}
	got := snapshotForStage(stage, base.Add(2*time.Second))
	if !got.Rolling10s.Rates.Available {
		t.Fatalf("two-call rate unavailable: %+v", got.Rolling10s.Rates)
	}
	assertFloat(t, "two-call calls/s", got.Rolling10s.Rates.CallsPerSecond, 1)

	aggregate := New(Config{}).Stage("apply", "ledger")
	if !aggregate.Record(Sample{At: base, Calls: 2}) {
		t.Fatal("aggregate record rejected")
	}
	if got := snapshotForStage(aggregate, base); got.Rolling10s.Rates.Available {
		t.Fatal("zero-elapsed rate reported")
	}
	if got := snapshotForStage(aggregate, base.Add(time.Second)); !got.Rolling10s.Rates.Available {
		t.Fatal("explicit two-call aggregate rate unavailable after positive elapsed time")
	}
}

func TestLatencyPercentilesUseBoundedMostRecentSamples(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)
	stage := New(Config{LatencySampleLimit: 4}).Stage("fetch", "generated")
	for index, duration := range []time.Duration{
		time.Millisecond,
		2 * time.Millisecond,
		3 * time.Millisecond,
		4 * time.Millisecond,
		5 * time.Millisecond,
	} {
		if !stage.Record(Sample{At: base.Add(time.Duration(index) * time.Second), Duration: duration}) {
			t.Fatalf("record duration %s", duration)
		}
	}

	got := snapshotForStage(stage, base.Add(5*time.Second)).Latency
	if got.Observed != 5 || got.Samples != 4 {
		t.Fatalf("latency counts = observed %d retained %d, want 5/4", got.Observed, got.Samples)
	}
	if got.P50 != 3*time.Millisecond || got.P95 != 5*time.Millisecond || got.P99 != 5*time.Millisecond {
		t.Errorf("latency percentiles = %s/%s/%s, want 3ms/5ms/5ms", got.P50, got.P95, got.P99)
	}
}

func TestRollingHistoryHasFixedStorage(t *testing.T) {
	t.Parallel()

	const samples = 1000
	base := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)
	stage := New(Config{LatencySampleLimit: 4}).Stage("fetch", "generated")
	for index := range samples {
		if !stage.Record(Sample{
			At:       base.Add(time.Duration(index) * time.Second),
			Duration: time.Millisecond,
			Calls:    1,
		}) {
			t.Fatalf("record sample %d", index)
		}
	}

	got := snapshotForStage(stage, base.Add(samples*time.Second+500*time.Millisecond))
	if len(stage.buckets) != rollingBucketCount {
		t.Fatalf("bucket storage = %d, want %d", len(stage.buckets), rollingBucketCount)
	}
	if got.Rolling10s.Counts.Calls != 10 || got.Rolling60s.Counts.Calls != 60 {
		t.Errorf("rolling calls = %d/%d, want 10/60",
			got.Rolling10s.Counts.Calls, got.Rolling60s.Counts.Calls)
	}
	if got.Totals.Calls != samples {
		t.Errorf("cumulative calls = %d, want %d", got.Totals.Calls, samples)
	}
	if got.Latency.Observed != samples || got.Latency.Samples != 4 {
		t.Errorf("latency = %+v, want observed %d retained 4", got.Latency, samples)
	}
}

func TestZeroLatencyIsAnAvailableSample(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)
	recorder := New(Config{})
	empty := recorder.Stage("empty", "poll")
	recorded := recorder.Stage("recorded", "poll")
	if !recorded.Record(Sample{At: base}) {
		t.Fatal("zero latency sample rejected")
	}

	snapshot := recorder.Snapshot(base.Add(time.Second))
	if len(snapshot.Stages) != 2 {
		t.Fatalf("stages = %d, want 2", len(snapshot.Stages))
	}
	if snapshot.Stages[0].Name != empty.name || snapshot.Stages[0].Latency.Samples != 0 {
		t.Errorf("empty stage = %+v", snapshot.Stages[0])
	}
	if snapshot.Stages[1].Name != recorded.name || snapshot.Stages[1].Latency.Samples != 1 ||
		snapshot.Stages[1].Latency.P50 != 0 {
		t.Errorf("recorded stage = %+v", snapshot.Stages[1])
	}
}

func TestRecordRejectsInvalidMeasurements(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)
	stage := New(Config{}).Stage("fetch", "poll")
	if stage.Record(Sample{Calls: 1}) {
		t.Fatal("zero timestamp accepted")
	}
	if stage.Record(Sample{At: base, Duration: -time.Nanosecond, Calls: 1}) {
		t.Fatal("negative duration accepted")
	}
	got := snapshotForStage(stage, base)
	if got.Totals.Samples != 0 || got.Latency.Samples != 0 {
		t.Fatalf("invalid samples changed stage: %+v", got)
	}
}

func TestInFlightAvailabilityRequiresSuccessfulAdjustment(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)
	stage := New(Config{}).Stage("fetch", "poll")
	untouched := snapshotForStage(stage, base)
	if untouched.InFlightAvailable || untouched.InFlight != 0 || untouched.HighWater != 0 {
		t.Fatalf("untouched gauge reported: %+v", untouched)
	}
	if !stage.Record(Sample{At: base, Calls: 1}) {
		t.Fatal("valid sample rejected")
	}
	if afterRecord := snapshotForStage(stage, base.Add(time.Second)); afterRecord.InFlightAvailable {
		t.Fatalf("record made gauge available: %+v", afterRecord)
	}
	if stage.AddInFlight(-1) {
		t.Fatal("in-flight underflow accepted")
	}
	invalid := snapshotForStage(stage, base)
	if invalid.InFlightAvailable {
		t.Fatalf("rejected adjustment made gauge available: %+v", invalid)
	}
	if !stage.AddInFlight(0) {
		t.Fatal("explicit zero adjustment rejected")
	}
	explicitZero := snapshotForStage(stage, base)
	if !explicitZero.InFlightAvailable || explicitZero.InFlight != 0 || explicitZero.HighWater != 0 {
		t.Fatalf("explicit zero gauge = %+v", explicitZero)
	}
}

func TestRecorderIsRaceSafe(t *testing.T) {
	t.Parallel()

	const (
		workers    = 8
		iterations = 1000
	)
	base := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)
	recorder := New(Config{LatencySampleLimit: 64})
	stage := recorder.Stage("fetch_batch", "generated")
	var failed atomic.Bool
	stopSnapshots := make(chan struct{})
	snapshotsDone := make(chan struct{})
	go func() {
		defer close(snapshotsDone)
		for {
			select {
			case <-stopSnapshots:
				return
			default:
				snapshot := recorder.Snapshot(base.Add(time.Minute))
				if len(snapshot.Stages) != 1 {
					failed.Store(true)
					continue
				}
				stageSnapshot := snapshot.Stages[0]
				if stageSnapshot.Totals.Samples != stageSnapshot.Latency.Observed ||
					stageSnapshot.Latency.Samples != min(stageSnapshot.Latency.Observed, uint64(64)) {
					failed.Store(true)
				}
			}
		}
	}()

	var group sync.WaitGroup
	group.Add(workers)
	for worker := range workers {
		go func() {
			defer group.Done()
			for iteration := range iterations {
				if !stage.AddInFlight(1) {
					failed.Store(true)
				}
				at := base.Add(time.Duration(worker*iterations+iteration) * time.Microsecond)
				if !stage.Record(Sample{
					At: at, Duration: time.Microsecond, Calls: 1,
					ItemsIn: 1, ItemsOut: 1, Transactions: 1,
				}) {
					failed.Store(true)
				}
				if !stage.AddInFlight(-1) {
					failed.Store(true)
				}
			}
		}()
	}
	group.Wait()
	close(stopSnapshots)
	<-snapshotsDone
	if failed.Load() {
		t.Fatal("concurrent operation failed")
	}

	got := snapshotForStage(stage, base.Add(time.Second))
	want := uint64(workers * iterations)
	if got.Totals.Calls != want || got.Totals.ItemsIn != want ||
		got.Totals.ItemsOut != want || got.Totals.Transactions != want {
		t.Errorf("totals = %+v, want each counter %d", got.Totals, want)
	}
	if !got.InFlightAvailable || got.InFlight != 0 || got.HighWater == 0 || got.HighWater > workers {
		t.Errorf("in-flight availability/value/high-water = %t/%d/%d, want true/0/1..%d",
			got.InFlightAvailable, got.InFlight, got.HighWater, workers)
	}
	if got.Latency.Observed != want || got.Latency.Samples != 64 {
		t.Errorf("latency = %+v, want observed %d retained 64", got.Latency, want)
	}
}

// snapshotForStage keeps focused tests independent from registry ordering.
func snapshotForStage(stage *Stage, now time.Time) StageSnapshot {
	return stage.snapshot(now)
}

func assertFloat(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-12 {
		t.Errorf("%s = %f, want %f", name, got, want)
	}
}

func TestRecordRejectsAggregateOverflowAtomically(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, time.October, 16, 10, 0, 0, 0, time.UTC)
	for _, dimension := range []string{"calls", "errors", "in", "out", "transactions", "duration"} {
		t.Run(dimension, func(t *testing.T) {
			t.Parallel()
			r := New(Config{LatencySampleLimit: 2})
			s := Sample{At: base, Calls: 2}
			overflow := Sample{At: base.Add(time.Second), Calls: 1, Duration: time.Second}
			switch dimension {
			case "calls":
				s.Calls = ^uint64(0)
			case "errors":
				s.Errors = ^uint64(0)
				overflow.Errors = 1
			case "in":
				s.ItemsIn = ^uint64(0)
				overflow.ItemsIn = 1
			case "out":
				s.ItemsOut = ^uint64(0)
				overflow.ItemsOut = 1
			case "transactions":
				s.Transactions = ^uint64(0)
				overflow.Transactions = 1
			case "duration":
				s.Duration = time.Duration(1<<63 - 1)
			}
			if !r.Record("stage", "channel", s) {
				t.Fatal("maximum representable sample refused")
			}
			before := r.Snapshot(base.Add(3 * time.Second))
			if r.Record("stage", "channel", overflow) {
				t.Fatal("unrepresentable sample accepted")
			}
			after := r.Snapshot(base.Add(3 * time.Second))
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected sample mutated state: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestRecordRejectsSampleCountOverflowAtomically(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, time.October, 16, 10, 0, 0, 0, time.UTC)
	r := New(Config{LatencySampleLimit: 2})
	s := r.Stage("stage", "channel")
	if !s.Record(Sample{At: base}) {
		t.Fatal("valid sample refused")
	}
	// Reaching this boundary through recording would take MaxUint64 calls.
	s.totals.Samples = ^uint64(0)
	s.latencyTotal = ^uint64(0)
	before := r.Snapshot(base.Add(2 * time.Second))
	if s.Record(Sample{At: base.Add(time.Second)}) {
		t.Fatal("sample count overflow accepted")
	}
	if after := r.Snapshot(base.Add(2 * time.Second)); !reflect.DeepEqual(before, after) {
		t.Fatal("rejection changed snapshot")
	}
}
