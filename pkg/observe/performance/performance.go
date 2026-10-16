// Package performance records process-local diagnostics with bounded per-stage history.
// It deliberately exposes only measurements supplied by callers; a missing
// sample remains missing rather than being estimated from another stage.
package performance

import (
	"slices"
	"sort"
	"sync"
	"time"
)

const (
	defaultLatencySampleLimit = 1024
	shortWindow               = 10 * time.Second
	longWindow                = 60 * time.Second
	bucketResolution          = time.Second
	rollingBucketCount        = 62
)

// Config controls the bounded storage used by a Recorder.
type Config struct {
	// LatencySampleLimit is the number of most-recent, explicitly observed
	// latency values retained per stage. Values below one use the default.
	LatencySampleLimit int
}

// Sample is one aggregate observation for a stage. Duration is both active
// time and one latency observation, including when it is explicitly zero.
// Calls and the object counters are independent: the recorder never derives
// one from another.
type Sample struct {
	At           time.Time
	Duration     time.Duration
	Calls        uint64
	Errors       uint64
	ItemsIn      uint64
	ItemsOut     uint64
	Transactions uint64
}

// Counts contains exact observed totals for a stage or time window. Samples
// makes the availability of an otherwise all-zero observation explicit.
type Counts struct {
	Samples      uint64
	Calls        uint64
	Errors       uint64
	ItemsIn      uint64
	ItemsOut     uint64
	Transactions uint64
	ActiveTime   time.Duration
}

// Rates contains rates calculated only from a window with at least two calls
// and a positive observed wall-clock span. ActiveTime is completed-sample
// service time charged to the bucket containing the sample timestamp.
// ActiveSecondsPerSecond may exceed one when calls overlap or when a long call
// completes inside the window.
type Rates struct {
	Available              bool
	Elapsed                time.Duration
	CallsPerSecond         float64
	ErrorsPerSecond        float64
	ItemsInPerSecond       float64
	ItemsOutPerSecond      float64
	TransactionsPerSecond  float64
	ActiveSecondsPerSecond float64
}

// WindowSnapshot contains exact counts from complete one-second buckets inside
// [Start, End). Rates use the span from the first included sample through End,
// not the nominal window duration, and remain unavailable until two calls have
// been observed. End trails the requested snapshot time by less than one
// Resolution so a concurrent sample can never be counted before its timestamp.
type WindowSnapshot struct {
	Start      time.Time
	End        time.Time
	Resolution time.Duration
	Counts     Counts
	Rates      Rates
}

// LatencySnapshot contains nearest-rank percentiles over the retained samples.
// Samples is the retained count; Observed is the cumulative count before
// bounded retention discarded older values.
type LatencySnapshot struct {
	Samples  uint64
	Observed uint64
	P50      time.Duration
	P95      time.Duration
	P99      time.Duration
}

// StageSnapshot is an immutable point-in-time copy of one stage.
type StageSnapshot struct {
	Name              string
	Channel           string
	InFlightAvailable bool
	InFlight          uint64
	HighWater         uint64
	Totals            Counts
	Rolling10s        WindowSnapshot
	Rolling60s        WindowSnapshot
	Latency           LatencySnapshot
}

// Snapshot is a deterministic point-in-time copy of all registered stages.
// Stages are sorted by name, then channel.
type Snapshot struct {
	At     time.Time
	Stages []StageSnapshot
}

// QueueSnapshot is a transport-independent queue measurement. Available
// distinguishes an observed empty or unbounded queue (zero values) from a
// queue for which the adapter cannot report diagnostics.
type QueueSnapshot struct {
	Name         string
	Channel      string
	Available    bool
	Depth        uint64
	Capacity     uint64
	Bytes        uint64
	ByteCapacity uint64
	HighWater    uint64
	Enqueued     uint64
	Dequeued     uint64
	Dropped      uint64
	OldestAge    time.Duration
}

// Recorder owns a set of stages. Call Stage once per static name/channel pair
// and retain the returned handle on hot paths to avoid repeated map lookups.
// Its zero value is ready to use with the default configuration.
type Recorder struct {
	mu                 sync.RWMutex
	stages             map[stageKey]*Stage
	latencySampleLimit int
}

type stageKey struct {
	name    string
	channel string
}

// Stage is a race-safe handle for one static name/channel pair.
type Stage struct {
	name               string
	channel            string
	latencySampleLimit int

	mu                sync.Mutex
	inFlightAvailable bool
	inFlight          uint64
	highWater         uint64
	totals            Counts
	buckets           [rollingBucketCount]timeBucket
	latencies         []time.Duration
	latencyCursor     int
	latencyTotal      uint64
}

type timeBucket struct {
	epochSecond int64
	available   bool
	firstAt     time.Time
	counts      Counts
}

// New constructs a Recorder. A non-positive latency limit selects the
// bounded default rather than disabling retention.
func New(config Config) *Recorder {
	limit := config.LatencySampleLimit
	if limit < 1 {
		limit = defaultLatencySampleLimit
	}

	return &Recorder{
		stages:             make(map[stageKey]*Stage),
		latencySampleLimit: limit,
	}
}

// Stage returns the stable handle for name and channel, creating it when
// needed. Concurrent calls for the same pair return the same handle.
func (r *Recorder) Stage(name, channel string) *Stage {
	key := stageKey{name: name, channel: channel}

	r.mu.RLock()
	stage := r.stages[key]
	r.mu.RUnlock()
	if stage != nil {
		return stage
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if stage = r.stages[key]; stage != nil {
		return stage
	}
	if r.stages == nil {
		r.stages = make(map[stageKey]*Stage)
	}
	limit := r.latencySampleLimit
	if limit < 1 {
		limit = defaultLatencySampleLimit
		r.latencySampleLimit = limit
	}
	stage = &Stage{
		name:               name,
		channel:            channel,
		latencySampleLimit: limit,
		latencies:          make([]time.Duration, 0, limit),
	}
	r.stages[key] = stage

	return stage
}

// Record is a convenience for callers that cannot retain a Stage handle. It
// returns false for a zero timestamp, negative duration or aggregate overflow
// and records nothing.
func (r *Recorder) Record(name, channel string, sample Sample) bool {
	return r.Stage(name, channel).Record(sample)
}

// AddInFlight is a convenience for adjusting the in-flight count of a stage.
func (r *Recorder) AddInFlight(name, channel string, delta int64) bool {
	return r.Stage(name, channel).AddInFlight(delta)
}

// Snapshot copies every stage and sorts the result by name, then channel.
func (r *Recorder) Snapshot(now time.Time) Snapshot {
	r.mu.RLock()
	stages := make([]*Stage, 0, len(r.stages))
	for _, stage := range r.stages {
		stages = append(stages, stage)
	}
	r.mu.RUnlock()

	result := Snapshot{
		At:     now,
		Stages: make([]StageSnapshot, 0, len(stages)),
	}
	for _, stage := range stages {
		result.Stages = append(result.Stages, stage.snapshot(now))
	}
	sort.Slice(result.Stages, func(i, j int) bool {
		if result.Stages[i].Name != result.Stages[j].Name {
			return result.Stages[i].Name < result.Stages[j].Name
		}

		return result.Stages[i].Channel < result.Stages[j].Channel
	})

	return result
}

// Record adds an aggregate sample. It returns false for a zero timestamp, a
// negative duration, or an unrepresentable cumulative count or active duration,
// and leaves the stage unchanged.
func (s *Stage) Record(sample Sample) bool {
	if sample.At.IsZero() || sample.Duration < 0 {
		return false
	}

	counts := Counts{
		Samples:      1,
		Calls:        sample.Calls,
		Errors:       sample.Errors,
		ItemsIn:      sample.ItemsIn,
		ItemsOut:     sample.ItemsOut,
		Transactions: sample.Transactions,
		ActiveTime:   sample.Duration,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.totals.canAdd(counts) {
		return false
	}
	s.totals.add(counts)
	s.recordWindow(sample.At, counts)
	s.recordLatency(sample.Duration)

	return true
}

// AddInFlight applies delta atomically with high-water tracking. The first
// successful call marks the gauge available; delta zero can therefore publish
// an explicitly observed zero. A decrement larger than the current count or a
// positive overflow returns false and leaves the gauge and counters unchanged.
func (s *Stage) AddInFlight(delta int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if delta < 0 {
		// Adding one before negation handles MinInt64 without signed overflow;
		// the result is non-negative, so the conversion preserves its value.
		decrement := uint64(-(delta + 1)) + 1 //nolint:gosec // conversion is the proven non-negative magnitude of delta
		if decrement > s.inFlight {
			return false
		}
		s.inFlightAvailable = true
		s.inFlight -= decrement

		return true
	}

	increment := uint64(delta)
	if ^uint64(0)-s.inFlight < increment {
		return false
	}
	s.inFlightAvailable = true
	s.inFlight += increment
	s.highWater = max(s.highWater, s.inFlight)

	return true
}

func (s *Stage) snapshot(now time.Time) StageSnapshot {
	s.mu.Lock()
	result := StageSnapshot{
		Name:              s.name,
		Channel:           s.channel,
		InFlightAvailable: s.inFlightAvailable,
		InFlight:          s.inFlight,
		HighWater:         s.highWater,
		Totals:            s.totals,
		Rolling10s:        s.window(now, shortWindow),
		Rolling60s:        s.window(now, longWindow),
	}
	latencies := slices.Clone(s.latencies)
	latencyTotal := s.latencyTotal
	s.mu.Unlock()

	result.Latency = latencySnapshot(latencies, latencyTotal)

	return result
}

func (s *Stage) window(now time.Time, duration time.Duration) WindowSnapshot {
	end := now.Truncate(bucketResolution)
	result := WindowSnapshot{
		Start:      end.Add(-duration),
		End:        end,
		Resolution: bucketResolution,
	}
	var first time.Time
	startEpoch := result.Start.Unix()
	endEpoch := result.End.Unix()
	for i := range s.buckets {
		bucket := &s.buckets[i]
		if !bucket.available || bucket.epochSecond < startEpoch || bucket.epochSecond >= endEpoch {
			continue
		}
		result.Counts.add(bucket.counts)
		if first.IsZero() || bucket.firstAt.Before(first) {
			first = bucket.firstAt
		}
	}
	if result.Counts.Calls < 2 || first.IsZero() || !result.End.After(first) {
		return result
	}

	elapsed := result.End.Sub(first)
	seconds := elapsed.Seconds()
	result.Rates = Rates{
		Available:              true,
		Elapsed:                elapsed,
		CallsPerSecond:         float64(result.Counts.Calls) / seconds,
		ErrorsPerSecond:        float64(result.Counts.Errors) / seconds,
		ItemsInPerSecond:       float64(result.Counts.ItemsIn) / seconds,
		ItemsOutPerSecond:      float64(result.Counts.ItemsOut) / seconds,
		TransactionsPerSecond:  float64(result.Counts.Transactions) / seconds,
		ActiveSecondsPerSecond: result.Counts.ActiveTime.Seconds() / seconds,
	}

	return result
}

func (s *Stage) recordWindow(at time.Time, counts Counts) {
	epochSecond := at.Unix()
	index := epochSecond % rollingBucketCount
	if index < 0 {
		index += rollingBucketCount
	}
	bucket := &s.buckets[index]
	if bucket.available && bucket.epochSecond > epochSecond {
		// A sample more than the ring span older than an already retained
		// sample cannot contribute to a current rolling window. Do not let it
		// evict the newer bucket with the same ring position.
		return
	}
	if !bucket.available || bucket.epochSecond != epochSecond {
		*bucket = timeBucket{
			epochSecond: epochSecond,
			available:   true,
			firstAt:     at,
		}
	}
	bucket.counts.add(counts)
	if at.Before(bucket.firstAt) {
		bucket.firstAt = at
	}
}

func (s *Stage) recordLatency(duration time.Duration) {
	s.latencyTotal++
	if len(s.latencies) < s.latencySampleLimit {
		s.latencies = append(s.latencies, duration)

		return
	}
	s.latencies[s.latencyCursor] = duration
	s.latencyCursor = (s.latencyCursor + 1) % s.latencySampleLimit
}

func latencySnapshot(latencies []time.Duration, observed uint64) LatencySnapshot {
	result := LatencySnapshot{
		Samples:  uint64(len(latencies)),
		Observed: observed,
	}
	if len(latencies) == 0 {
		return result
	}

	slices.Sort(latencies)
	result.P50 = nearestRank(latencies, 50)
	result.P95 = nearestRank(latencies, 95)
	result.P99 = nearestRank(latencies, 99)

	return result
}

func nearestRank(ordered []time.Duration, percentile int) time.Duration {
	rank := (percentile*len(ordered) + 99) / 100

	return ordered[rank-1]
}

func (c *Counts) add(other Counts) {
	c.Samples += other.Samples
	c.Calls += other.Calls
	c.Errors += other.Errors
	c.ItemsIn += other.ItemsIn
	c.ItemsOut += other.ItemsOut
	c.Transactions += other.Transactions
	c.ActiveTime += other.ActiveTime
}

// Every rolling bucket and window is a subset of the accepted cumulative
// observations, so checking the cumulative sums also bounds their additions.
func (c Counts) canAdd(other Counts) bool {
	const maxUint = ^uint64(0)
	const maxDuration = time.Duration(1<<63 - 1)
	return maxUint-c.Samples >= other.Samples &&
		maxUint-c.Calls >= other.Calls &&
		maxUint-c.Errors >= other.Errors &&
		maxUint-c.ItemsIn >= other.ItemsIn &&
		maxUint-c.ItemsOut >= other.ItemsOut &&
		maxUint-c.Transactions >= other.Transactions &&
		maxDuration-c.ActiveTime >= other.ActiveTime
}
