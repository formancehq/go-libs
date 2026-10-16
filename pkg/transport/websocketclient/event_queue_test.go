package websocketclient

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEventQueueDropConservationAndOldestAge(t *testing.T) {
	queue := NewEventQueue[int](2, EventQueueDrop)
	if !queue.TryPush(Event[int]{Value: 1}) || !queue.TryPush(Event[int]{Value: 2}) {
		t.Fatal("queue rejected an event below capacity")
	}
	if queue.TryPush(Event[int]{Value: 3}) {
		t.Fatal("queue admitted an event above capacity")
	}
	time.Sleep(time.Millisecond)
	stats := queue.Snapshot()
	if stats.Queue.Depth != 2 || stats.Queue.Capacity != 2 || stats.Queue.HighWater != 2 ||
		stats.Queue.Enqueued != 2 || stats.Queue.Dequeued != 0 || stats.Queue.Dropped != 1 ||
		stats.Coalesced != 0 || stats.Queue.OldestAge <= 0 {
		t.Fatalf("saturated queue snapshot = %+v", stats)
	}

	first, ok := queue.Receive(t.Context())
	if !ok || first.Value != 1 {
		t.Fatalf("first receive = %+v, %t", first, ok)
	}
	second, ok := queue.Receive(t.Context())
	if !ok || second.Value != 2 {
		t.Fatalf("second receive = %+v, %t", second, ok)
	}
	stats = queue.Snapshot()
	if stats.Queue.Depth != 0 || stats.Queue.Enqueued != stats.Queue.Dequeued || stats.Queue.OldestAge != 0 {
		t.Fatalf("drained queue snapshot = %+v", stats)
	}
}

func TestEventQueueCoalescesWithoutReportingDrop(t *testing.T) {
	queue := NewEventQueue[struct{}](1, EventQueueCoalesce)
	if !queue.TryPush(Event[struct{}]{}) {
		t.Fatal("queue rejected first trigger")
	}
	if queue.TryPush(Event[struct{}]{}) {
		t.Fatal("queue admitted trigger above capacity")
	}
	stats := queue.Snapshot()
	if stats.Queue.Enqueued != 1 || stats.Queue.Dropped != 0 || stats.Coalesced != 1 {
		t.Fatalf("coalescing queue snapshot = %+v", stats)
	}
}

func TestEventQueueCloseDrainsBufferedEventsDespiteCanceledContext(t *testing.T) {
	queue := NewEventQueue[int](3, EventQueueDrop)
	for value := 1; value <= 3; value++ {
		if !queue.TryPush(Event[int]{Value: value}) {
			t.Fatalf("queue rejected value %d", value)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	queue.Close()

	for want := 1; want <= 3; want++ {
		event, ok := queue.Receive(ctx)
		if !ok || event.Value != want {
			t.Fatalf("receive = %+v, %t; want value %d", event, ok, want)
		}
	}
	if _, ok := queue.Receive(ctx); ok {
		t.Fatal("closed drained queue returned an event")
	}
	stats := queue.Snapshot()
	if stats.Queue.Depth != 0 || stats.Queue.Enqueued != 3 || stats.Queue.Dequeued != 3 {
		t.Fatalf("drained close snapshot = %+v", stats)
	}
}

func TestEventQueueCanceledOpenReceiveReturns(t *testing.T) {
	queue := NewEventQueue[int](1, EventQueueDrop)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, ok := queue.Receive(ctx); ok {
		t.Fatal("canceled open queue returned an event")
	}
}

func TestEventQueuePushAfterCloseIsRejectedWithoutDropOrCoalesce(t *testing.T) {
	queue := NewEventQueue[int](1, EventQueueCoalesce)
	queue.Close()
	queue.Close() // Close is deliberately idempotent.
	if queue.TryPush(Event[int]{Value: 42}) {
		t.Fatal("closed queue admitted an event")
	}
	stats := queue.Snapshot()
	if stats.RejectedClosed != 1 || stats.Queue.Enqueued != 0 || stats.Queue.Dropped != 0 || stats.Coalesced != 0 {
		t.Fatalf("closed rejection snapshot = %+v", stats)
	}
}

func TestEventQueueConcurrentCloseAndPushConservation(t *testing.T) {
	const writers = 8
	const perWriter = 2000
	queue := NewEventQueue[int](64, EventQueueDrop)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for writer := range writers {
		wg.Go(func() {
			<-start
			for value := range perWriter {
				queue.TryPush(Event[int]{Value: writer*perWriter + value})
			}
		})
	}
	closed := make(chan struct{})
	go func() {
		<-start
		runtime.Gosched()
		queue.Close()
		close(closed)
	}()
	close(start)
	wg.Wait()
	<-closed

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for {
		if _, ok := queue.Receive(ctx); !ok {
			break
		}
	}
	stats := queue.Snapshot()
	attempts := uint64(writers * perWriter)
	if stats.Queue.Enqueued+stats.Queue.Dropped+stats.RejectedClosed != attempts {
		t.Fatalf("admitted + dropped + closed = %d, want %d; snapshot %+v",
			stats.Queue.Enqueued+stats.Queue.Dropped+stats.RejectedClosed, attempts, stats)
	}
	if stats.Queue.Depth != 0 || stats.Queue.Enqueued != stats.Queue.Dequeued {
		t.Fatalf("closed queue conservation snapshot = %+v", stats)
	}
}

func TestEventQueueConcurrentConservation(t *testing.T) {
	const producers = 4
	const perProducer = 2000
	queue := NewEventQueue[int](64, EventQueueDrop)
	var consumed atomic.Uint64
	var consumers sync.WaitGroup
	for range 4 {
		consumers.Go(func() {
			for {
				_, ok := queue.Receive(context.Background())
				if !ok {
					return
				}
				consumed.Add(1)
			}
		})
	}

	var writers sync.WaitGroup
	for producer := range producers {
		writers.Go(func() {
			for value := range perProducer {
				queue.TryPush(Event[int]{Value: producer*perProducer + value})
			}
		})
	}
	writers.Wait()
	queue.Close()
	consumers.Wait()

	stats := queue.Snapshot()
	attempts := uint64(producers * perProducer)
	if stats.Queue.Enqueued+stats.Queue.Dropped != attempts {
		t.Fatalf("enqueued + dropped = %d, want %d", stats.Queue.Enqueued+stats.Queue.Dropped, attempts)
	}
	if stats.Queue.Enqueued != stats.Queue.Dequeued || stats.Queue.Dequeued != consumed.Load() || stats.Queue.Depth != 0 {
		t.Fatalf("final conservation snapshot = %+v, consumed %d", stats, consumed.Load())
	}
	if stats.Queue.HighWater > stats.Queue.Capacity {
		t.Fatalf("high-water %d exceeds capacity %d", stats.Queue.HighWater, stats.Queue.Capacity)
	}
}

func TestEventQueueHotPathDoesNotAllocate(t *testing.T) {
	queue := NewEventQueue[int](1, EventQueueDrop)
	event := Event[int]{Value: 42}
	allocs := testing.AllocsPerRun(1000, func() {
		if !queue.TryPush(event) {
			t.Fatal("enqueue failed")
		}
		if _, ok := queue.Receive(context.Background()); !ok {
			t.Fatal("receive failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("event queue hot-path allocations = %f, want 0", allocs)
	}
}

func BenchmarkEventQueuePushReceive(b *testing.B) {
	queue := NewEventQueue[int](1, EventQueueDrop)
	event := Event[int]{Value: 42}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		queue.TryPush(event)
		queue.Receive(ctx)
	}
}
