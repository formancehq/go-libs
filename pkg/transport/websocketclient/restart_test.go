package websocketclient

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
)

func TestKeyedBufferDropRestoresAdmission(t *testing.T) {
	b := NewKeyedBuffer[string](2)
	b.Push("old", "before restart")
	b.Drop()
	b.Drop()
	if stats := b.Stats(); stats.Depth != 0 || stats.Bytes != 0 || !stats.OldestQueuedAt.IsZero() {
		t.Fatalf("after drop: %+v", stats)
	}
	b.Push("fresh", "after restart")
	got, more := b.Drain(t.Context(), 0)
	if more || len(got) != 1 || got[0] != "after restart" {
		t.Fatalf("drained pre-restart state: %v, %v", got, more)
	}
}

func TestEventQueueDropRestoresAdmission(t *testing.T) {
	q := NewEventQueue[int](2, EventQueueDrop)
	q.TryPush(Event[int]{Value: 1})
	q.TryPush(Event[int]{Value: 2})
	q.Drop()
	q.Drop()
	if stats := q.Snapshot(); stats.Queue.Depth != 0 || stats.Queue.Bytes != 0 || stats.Queue.OldestAge != 0 {
		t.Fatalf("after drop: %+v", stats)
	}
	for value := 3; value <= 4; value++ {
		if !q.TryPush(Event[int]{Value: value}) {
			t.Fatal("drop did not return admission slots")
		}
	}
	q.Close()
	q.Drop()
	if _, ok := q.Receive(t.Context()); ok || q.TryPush(Event[int]{Value: 5}) {
		t.Fatal("drop reopened a closed queue or retained an event")
	}
}

func TestEventQueueDropDoesNotWaitForAnIdleReceiver(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := NewEventQueue[int](2, EventQueueDrop)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			got, ok := q.Receive(ctx)
			if !ok || got.Value != 3 {
				t.Errorf("receive = %+v, %v", got, ok)
			}
		}()
		synctest.Wait()
		q.Drop()
		q.TryPush(Event[int]{Value: 3})
		<-finished
	})
}

func TestEventQueueConcurrentDropPreservesAccountingAndClose(t *testing.T) {
	q := NewEventQueue[int](32, EventQueueDrop)
	var consumers, writers sync.WaitGroup
	for range 4 {
		consumers.Go(func() {
			for {
				if _, ok := q.Receive(t.Context()); !ok {
					return
				}
			}
		})
	}
	for writer := range 4 {
		writers.Go(func() {
			for value := range 1000 {
				q.TryPush(Event[int]{Value: writer*1000 + value})
				if value%17 == 0 {
					q.Drop()
				}
			}
		})
	}
	writers.Wait()
	q.Close()
	consumers.Wait()
	stats := q.Snapshot()
	if stats.Queue.Depth != 0 || stats.Queue.Bytes != 0 || stats.Queue.Enqueued != stats.Queue.Dequeued+stats.Discarded || stats.Queue.Enqueued+stats.Queue.Dropped != 4000 {
		t.Fatalf("reset lost queue accounting: %+v", stats)
	}
}
