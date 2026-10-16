package websocketclient

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/formancehq/go-libs/v5/pkg/observe/traces"
)

// KeyedBuffer is a bounded, insertion-ordered buffer for live events. Pushing
// an existing key refreshes its value without moving it; pushing a new key at
// capacity evicts the oldest entry.
type KeyedBuffer[T any] struct {
	metrics     *collectionMetrics
	mu          sync.Mutex
	capacity    int
	byteLimit   int
	bytes       int
	rejected    uint64
	entries     map[string]bufferEntry[T]
	order       []string
	highWater   int
	pushed      uint64
	inserted    uint64
	replaced    uint64
	drained     uint64
	evicted     uint64
	discarded   uint64
	clockOrigin time.Time
}

type bufferEntry[T any] struct {
	value          T
	bytes          int
	link           trace.SpanContext
	queuedAtOffset time.Duration
}

// BufferStats is one concurrency-safe snapshot of a KeyedBuffer.
type BufferStats struct {
	Depth           int
	Bytes           int
	ByteCapacity    int
	Rejected        uint64
	Capacity        int
	HighWater       int
	Pushed          uint64
	Inserted        uint64
	Replaced        uint64
	Drained         uint64
	Evicted         uint64
	Discarded       uint64
	OldestQueuedAt  time.Time
	OldestQueuedAge time.Duration
}

func NewKeyedBuffer[T any](capacity int, options ...CollectionOption) *KeyedBuffer[T] {
	if capacity <= 0 {
		capacity = MaxBuffered
	}

	buffer := &KeyedBuffer[T]{
		metrics:     newCollectionMetrics("buffer", options),
		capacity:    capacity,
		byteLimit:   DefaultQueueBytes,
		entries:     make(map[string]bufferEntry[T]),
		clockOrigin: time.Now(),
	}
	buffer.recordMetrics()
	return buffer
}

func (b *KeyedBuffer[T]) Push(key string, value T, links ...trace.SpanContext) {
	b.mu.Lock()
	defer b.mu.Unlock()
	defer b.recordMetrics()

	b.pushed++
	size, err := encodedSize(value)
	if err != nil || size+len(key) > b.byteLimit {
		b.rejected++
		return
	}
	size += len(key)
	entry, ok := b.entries[key]
	for b.bytes-entry.bytes+size > b.byteLimit {
		index := 0
		if b.order[index] == key {
			index++
		}
		oldest := b.order[index]
		b.bytes -= b.entries[oldest].bytes
		b.order = append(b.order[:index], b.order[index+1:]...)
		delete(b.entries, oldest)
		b.evicted++
	}
	if !ok {
		b.inserted++
		if len(b.entries) >= b.capacity {
			oldest := b.order[0]
			b.order = b.order[1:]
			b.bytes -= b.entries[oldest].bytes
			delete(b.entries, oldest)
			b.evicted++
		}
		b.order = append(b.order, key)
		entry.queuedAtOffset = time.Since(b.clockOrigin)
	} else {
		b.replaced++
	}
	var link trace.SpanContext
	if len(links) > 0 {
		link = links[0]
	}
	b.bytes += size - entry.bytes
	entry.bytes = size
	entry.value = value
	entry.link = link
	b.entries[key] = entry
	if depth := len(b.entries); depth > b.highWater {
		b.highWater = depth
	}
}

// Drain removes up to limit values in insertion order. A non-positive limit
// drains the whole buffer. hasMore reports whether values remain.
func (b *KeyedBuffer[T]) Drain(ctx context.Context, limit int64) (values []T, hasMore bool) {
	b.mu.Lock()

	n := len(b.order)
	if limit > 0 && int64(n) > limit {
		n = int(limit)
		hasMore = true
	}
	values = make([]T, 0, n)
	links := make([]trace.SpanContext, 0, n)
	for _, key := range b.order[:n] {
		entry := b.entries[key]
		values = append(values, entry.value)
		links = append(links, entry.link)
		b.bytes -= entry.bytes
		delete(b.entries, key)
	}
	b.order = b.order[n:]
	b.drained += uint64(n)
	b.recordMetrics()
	b.mu.Unlock()

	traces.AddLinks(ctx, links...)

	return values, hasMore
}

func (b *KeyedBuffer[T]) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	return len(b.entries)
}

// Drop discards the live tail at a source restart. Future pushes retain the
// configured bounds; activity counters remain process-local diagnostics.
func (b *KeyedBuffer[T]) Drop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	defer b.recordMetrics()
	b.discarded += uint64(len(b.entries))
	clear(b.entries)
	b.order = nil
	b.bytes = 0
}

// Stats returns one internally consistent snapshot of buffer counters and
// gauges. Pushed counts all Push calls; Inserted and Replaced split that total,
// so Inserted - Drained - Evicted - Discarded equals Depth. OldestQueuedAt and
// OldestQueuedAge report key residency and are zero when the buffer is empty.
// Replacing an existing key keeps its original queue position and queued-at
// timestamp.
func (b *KeyedBuffer[T]) Stats() BufferStats {
	b.mu.Lock()
	defer b.mu.Unlock()

	stats := BufferStats{
		Depth: len(b.entries),
		Bytes: b.bytes, ByteCapacity: b.byteLimit, Rejected: b.rejected,
		Capacity:  b.capacity,
		HighWater: b.highWater,
		Pushed:    b.pushed,
		Inserted:  b.inserted,
		Replaced:  b.replaced,
		Drained:   b.drained,
		Evicted:   b.evicted,
		Discarded: b.discarded,
	}
	if len(b.order) > 0 {
		oldestOffset := b.entries[b.order[0]].queuedAtOffset
		stats.OldestQueuedAt = b.clockOrigin.Add(oldestOffset)
		if nowOffset := time.Since(b.clockOrigin); nowOffset > oldestOffset {
			stats.OldestQueuedAge = nowOffset - oldestOffset
		}
	}

	return stats
}

func (b *KeyedBuffer[T]) recordMetrics() {
	if b.metrics == nil {
		return
	}
	oldest := int64(0)
	if len(b.order) > 0 {
		oldest = b.clockOrigin.Add(b.entries[b.order[0]].queuedAtOffset).UnixNano()
	}
	b.metrics.record([8]uint64{b.pushed, b.inserted, b.replaced, b.drained, b.evicted, b.discarded, b.rejected}, [8]int64{int64(len(b.entries)), int64(b.capacity), int64(b.bytes), int64(b.byteLimit), int64(b.highWater), oldest})
}
