package websocketclient

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestKeyedBufferOrderRefreshAndDrain(t *testing.T) {
	b := NewKeyedBuffer[string](3)
	b.Push("a", "old")
	b.Push("b", "second")
	b.Push("a", "new")
	b.Push("c", "third")

	got, more := b.Drain(t.Context(), 2)
	if more != true || len(got) != 2 || got[0] != "new" || got[1] != "second" {
		t.Fatalf("drain = %v, more=%v", got, more)
	}
	if b.Len() != 1 {
		t.Fatalf("len = %d, want 1", b.Len())
	}
}

func TestKeyedBufferEvictsOldest(t *testing.T) {
	b := NewKeyedBuffer[int](2)
	b.Push("a", 1)
	b.Push("b", 2)
	b.Push("c", 3)

	got, more := b.Drain(t.Context(), 0)
	if more || len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("drain = %v, more=%v", got, more)
	}
}

func TestKeyedBufferConcurrentAccess(t *testing.T) {
	b := NewKeyedBuffer[int](MaxBuffered)
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Go(func() {
			b.Push("shared", i)
			_ = b.Len()
		})
	}
	wg.Wait()
	got, _ := b.Drain(context.Background(), 0)
	if len(got) != 1 {
		t.Fatalf("deduplicated values = %d, want 1", len(got))
	}
	stats := b.Stats()
	if stats.Pushed != 100 || stats.Inserted != 1 || stats.Replaced != 99 ||
		stats.Drained != 1 || stats.HighWater != 1 || stats.Evicted != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestKeyedBufferStatsTrackCapacityActivityAndAge(t *testing.T) {
	b := NewKeyedBuffer[string](2)
	if got := b.Stats(); got.Depth != 0 || got.Capacity != 2 || got.HighWater != 0 ||
		got.Pushed != 0 || got.Inserted != 0 || got.Replaced != 0 || got.Drained != 0 || got.Evicted != 0 ||
		!got.OldestQueuedAt.IsZero() || got.OldestQueuedAge != 0 {
		t.Fatalf("initial stats = %+v", got)
	}

	before := time.Now()
	b.Push("a", "old")
	after := time.Now()
	first := b.Stats()
	if first.Depth != 1 || first.HighWater != 1 || first.Pushed != 1 || first.Inserted != 1 ||
		first.Replaced != 0 || first.OldestQueuedAt.IsZero() {
		t.Fatalf("stats after first push = %+v", first)
	}
	if first.OldestQueuedAt.Before(before) || first.OldestQueuedAt.After(after) || first.OldestQueuedAge < 0 {
		t.Fatalf("oldest after first push = %s age=%s, want timestamp in [%s, %s]",
			first.OldestQueuedAt, first.OldestQueuedAge, before, after)
	}

	// A replacement is still a push, but it preserves the key's queue position
	// and age just as Push's existing ordering contract promises.
	b.Push("a", "new")
	replaced := b.Stats()
	if replaced.Pushed != 2 || replaced.Inserted != 1 || replaced.Replaced != 1 || replaced.Depth != 1 ||
		!replaced.OldestQueuedAt.Equal(first.OldestQueuedAt) {
		t.Fatalf("stats after replacement = %+v, first oldest=%s", replaced, first.OldestQueuedAt)
	}

	time.Sleep(time.Millisecond)
	b.Push("b", "second")
	b.Push("c", "third") // evicts a
	full := b.Stats()
	if full.Depth != 2 || full.Capacity != 2 || full.HighWater != 2 ||
		full.Pushed != 4 || full.Inserted != 3 || full.Replaced != 1 || full.Drained != 0 || full.Evicted != 1 {
		t.Fatalf("stats at capacity = %+v", full)
	}
	if full.OldestQueuedAt.IsZero() || !full.OldestQueuedAt.After(first.OldestQueuedAt) {
		t.Fatalf("oldest after eviction = %s, want after evicted timestamp %s", full.OldestQueuedAt, first.OldestQueuedAt)
	}

	got, more := b.Drain(t.Context(), 1)
	if !more || len(got) != 1 || got[0] != "second" {
		t.Fatalf("partial drain = %v, more=%v", got, more)
	}
	partial := b.Stats()
	if partial.Depth != 1 || partial.Drained != 1 || partial.HighWater != 2 || partial.Evicted != 1 {
		t.Fatalf("stats after partial drain = %+v", partial)
	}

	b.Drain(t.Context(), 0)
	empty := b.Stats()
	if empty.Depth != 0 || empty.Drained != 2 || empty.HighWater != 2 || empty.Evicted != 1 ||
		!empty.OldestQueuedAt.IsZero() || empty.OldestQueuedAge != 0 {
		t.Fatalf("stats after full drain = %+v", empty)
	}
}
