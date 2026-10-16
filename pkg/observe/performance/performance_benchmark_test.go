package performance

import (
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

const benchmarkLatencyLimit = 256

func BenchmarkStageRecord(b *testing.B) {
	recorder := New(Config{LatencySampleLimit: benchmarkLatencyLimit})
	stage := recorder.Stage("fetch_batch", "generated")
	sample := Sample{
		At:           time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC),
		Duration:     250 * time.Microsecond,
		Calls:        1,
		ItemsIn:      500,
		ItemsOut:     500,
		Transactions: 100,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if !stage.Record(sample) {
			b.Fatal("fixed valid sample rejected")
		}
	}
	b.StopTimer()

	validateBenchmarkStorage(b, []*Stage{stage}, benchmarkLatencyLimit)
}

func BenchmarkRecorderSnapshot(b *testing.B) {
	for _, stageCount := range []int{1, 8} {
		b.Run("stages="+strconv.Itoa(stageCount), func(b *testing.B) {
			recorder, stages, now := populatedBenchmarkRecorder(b, stageCount)

			b.ReportAllocs()
			b.ResetTimer()
			var snapshot Snapshot
			for b.Loop() {
				snapshot = recorder.Snapshot(now)
			}
			b.StopTimer()
			runtime.KeepAlive(snapshot)

			validateBenchmarkStorage(b, stages, benchmarkLatencyLimit)
		})
	}
}

func BenchmarkStageRecordAndSnapshotContended(b *testing.B) {
	recorder, stages, now := populatedBenchmarkRecorder(b, 1)
	stage := stages[0]
	sample := Sample{
		At:           now.Add(-time.Second),
		Duration:     250 * time.Microsecond,
		Calls:        1,
		ItemsIn:      500,
		ItemsOut:     500,
		Transactions: 100,
	}
	var workerSequence atomic.Uint64
	var failed atomic.Bool

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		snapshotWorker := workerSequence.Add(1)%2 == 0
		var snapshot Snapshot
		for pb.Next() {
			if snapshotWorker {
				snapshot = recorder.Snapshot(now)
				continue
			}
			if !stage.Record(sample) {
				failed.Store(true)
			}
		}
		runtime.KeepAlive(snapshot)
	})
	b.StopTimer()
	if failed.Load() {
		b.Fatal("fixed valid sample rejected")
	}

	validateBenchmarkStorage(b, stages, benchmarkLatencyLimit)
}

func populatedBenchmarkRecorder(b *testing.B, stageCount int) (*Recorder, []*Stage, time.Time) {
	b.Helper()

	base := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)
	recorder := New(Config{LatencySampleLimit: benchmarkLatencyLimit})
	stages := make([]*Stage, 0, stageCount)
	for stageIndex := range stageCount {
		stage := recorder.Stage("stage-"+strconv.Itoa(stageIndex), "generated")
		stages = append(stages, stage)
		for sampleIndex := range benchmarkLatencyLimit {
			duration := time.Duration((sampleIndex*37)%benchmarkLatencyLimit+1) * time.Microsecond
			if !stage.Record(Sample{
				At:           base,
				Duration:     duration,
				Calls:        1,
				ItemsIn:      500,
				ItemsOut:     500,
				Transactions: 100,
			}) {
				b.Fatal("fixed valid sample rejected")
			}
		}
	}

	return recorder, stages, base.Add(2 * time.Second)
}

func validateBenchmarkStorage(b *testing.B, stages []*Stage, latencyLimit int) {
	b.Helper()

	for index, stage := range stages {
		if stage.inFlightAvailable {
			b.Fatalf("stage %d record-only benchmark made in-flight gauge available", index)
		}
		if len(stage.buckets) != rollingBucketCount {
			b.Fatalf("stage %d rolling storage = %d, want %d", index, len(stage.buckets), rollingBucketCount)
		}
		if len(stage.latencies) > latencyLimit || cap(stage.latencies) != latencyLimit {
			b.Fatalf("stage %d latency storage = len %d cap %d, want len <= %d cap %d",
				index, len(stage.latencies), cap(stage.latencies), latencyLimit, latencyLimit)
		}
	}
	b.ReportMetric(float64(rollingBucketCount), "rolling-buckets/stage")
	b.ReportMetric(float64(latencyLimit), "latency-cap/stage")
}
