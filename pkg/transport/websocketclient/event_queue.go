package websocketclient

import (
	"context"
	"sync"
	"time"

	"github.com/formancehq/go-libs/v5/pkg/observe/performance"
)

// EventQueueOverflow defines the truthful outcome of a non-blocking enqueue
// when the decoded-event worker queue is full.
type EventQueueOverflow uint8

const (
	// EventQueueDrop means the rejected event itself is not admitted. The
	// provider must recover completeness through its authoritative backstop.
	EventQueueDrop EventQueueOverflow = iota
	// EventQueueCoalesce means an already queued trigger causes the same
	// authoritative refresh, so rejecting another trigger is not data loss.
	EventQueueCoalesce
)

// EventQueueSnapshot is an immutable snapshot of a decoded-event worker queue.
// Coalesced is kept separate from Queue.Dropped because the former is a
// lossless trigger optimization rather than a rejected provider event.
// RejectedClosed counts calls made after shutdown, separately from runtime
// backpressure outcomes.
type EventQueueSnapshot struct {
	Queue          performance.QueueSnapshot
	Coalesced      uint64
	RejectedClosed uint64
	Discarded      uint64
}

// EventQueue is a bounded, non-blocking worker queue with exact admission and
// dequeue accounting. Its fixed timestamp ring mirrors channel FIFO order, so
// oldest age stays exact without allocating per event. A notification channel
// wakes idle receivers without holding the dequeue lock; event processing
// remains fully concurrent.
type EventQueue[T any] struct {
	events   chan Event[T]
	slots    chan struct{}
	overflow EventQueueOverflow

	ready          chan struct{}
	mu             sync.Mutex
	times          []time.Time
	sizes          []int
	bytes          int
	byteLimit      int
	head           int
	tail           int
	depth          int
	highWater      int
	enqueued       uint64
	dequeued       uint64
	dropped        uint64
	discarded      uint64
	coalesced      uint64
	rejectedClosed uint64
	closed         bool
}

// NewEventQueue constructs a queue with fixed capacity and overflow semantics.
func NewEventQueue[T any](capacity int, overflow EventQueueOverflow) *EventQueue[T] {
	if capacity < 0 {
		capacity = 0
	}

	queue := &EventQueue[T]{
		events:   make(chan Event[T], capacity),
		slots:    make(chan struct{}, capacity),
		overflow: overflow,
		ready:    make(chan struct{}, 1),
		times:    make([]time.Time, capacity),
		sizes:    make([]int, capacity), byteLimit: DefaultQueueBytes,
	}
	for range capacity {
		queue.slots <- struct{}{}
	}

	return queue
}

// TryPush admits event without blocking. False means a dropped event, a
// coalesced trigger, or an event rejected because the queue is closed; the
// snapshot reports those outcomes separately.
func (q *EventQueue[T]) TryPush(event Event[T]) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		q.rejectedClosed++

		return false
	}

	size, err := encodedSize(event.Value)
	if err != nil || size > q.byteLimit || q.bytes+size > q.byteLimit {
		q.dropped++
		return false
	}
	select {
	case <-q.slots:
	default:
		if q.overflow == EventQueueCoalesce {
			q.coalesced++
		} else {
			q.dropped++
		}

		return false
	}

	enqueuedAt := time.Now()
	if len(q.times) > 0 {
		q.times[q.tail] = enqueuedAt
		q.sizes[q.tail] = size
		q.bytes += size
		q.tail = (q.tail + 1) % len(q.times)
	}
	q.depth++
	q.enqueued++
	q.highWater = max(q.highWater, q.depth)
	q.events <- event
	q.notify()

	return true
}

// Receive waits for one event or context cancellation. Dequeued means handed
// to a worker; it does not imply that provider processing has completed.
func (q *EventQueue[T]) Receive(ctx context.Context) (Event[T], bool) {
	for {
		q.mu.Lock()
		select {
		case event, ok := <-q.events:
			if ok {
				q.removeHead()
				q.dequeued++
				if q.depth > 0 && !q.closed {
					q.notify()
				}
			}
			q.mu.Unlock()
			return event, ok
		default:
			q.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			var zero Event[T]
			return zero, false
		case <-q.ready:
		}
	}
}

// notify is called with mu held; readiness is coalesced, not the events.
func (q *EventQueue[T]) notify() {
	select {
	case q.ready <- struct{}{}:
	default:
	}
}

// removeHead is called with mu held after a channel dequeue.
func (q *EventQueue[T]) removeHead() {
	if len(q.times) > 0 {
		q.times[q.head] = time.Time{}
		q.bytes -= q.sizes[q.head]
		q.sizes[q.head] = 0
		q.head = (q.head + 1) % len(q.times)
	}
	q.depth--
	q.slots <- struct{}{}
}

// Drop discards events still queued at the reset boundary, without waiting for
// idle workers. Events already handed to a worker belong to that worker; its
// owner must fence their results before resetting its downstream buffers.
func (q *EventQueue[T]) Drop() {
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.depth > 0 {
		<-q.events
		q.removeHead()
		q.discarded++
	}
}

// Close prevents further enqueue and lets workers finish receiving buffered
// events. Providers call it only after their Supervisor has stopped producing.
func (q *EventQueue[T]) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	close(q.events)
	close(q.ready)
}

// Snapshot returns one coherent queue snapshot. A nil queue is unavailable.
func (q *EventQueue[T]) Snapshot() EventQueueSnapshot {
	if q == nil {
		return EventQueueSnapshot{}
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	oldestAge := time.Duration(0)
	if q.depth > 0 && len(q.times) > 0 {
		oldestAge = max(0, time.Since(q.times[q.head]))
	}

	return EventQueueSnapshot{
		Queue: performance.QueueSnapshot{
			Name:      "websocket.events",
			Channel:   "websocket",
			Available: true,
			Depth:     uint64(q.depth),                                    //nolint:gosec // admission and dequeue keep bounded depth nonnegative
			Bytes:     uint64(q.bytes), ByteCapacity: uint64(q.byteLimit), //nolint:gosec // bounded channel depth is non-negative
			Capacity:  uint64(cap(q.events)),
			HighWater: uint64(q.highWater), //nolint:gosec // bounded queue high-water is non-negative
			Enqueued:  q.enqueued,
			Dequeued:  q.dequeued,
			Dropped:   q.dropped,
			OldestAge: oldestAge,
		},
		Coalesced:      q.coalesced,
		RejectedClosed: q.rejectedClosed,
		Discarded:      q.discarded,
	}
}
