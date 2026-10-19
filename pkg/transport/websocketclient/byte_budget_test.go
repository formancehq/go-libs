package websocketclient

import (
	"testing"
)

func TestKeyedBufferEvictsWithinByteBudgetAndPreservesReplacement(t *testing.T) {
	b := NewKeyedBuffer[string](100)
	b.byteLimit = 30
	b.Push("a", "one")
	b.Push("b", "two")
	if b.Len() != 1 || b.Stats().Evicted != 1 || b.Stats().Bytes > b.Stats().ByteCapacity {
		t.Fatalf("stats=%+v", b.Stats())
	}
	b.Push("b", "much-too-large")
	if b.Stats().Rejected != 1 || b.Len() != 1 {
		t.Fatal("oversized replacement discarded old value")
	}
	stats := b.Stats()
	if stats.Pushed != stats.Inserted+stats.Replaced+stats.Rejected {
		t.Fatalf("push accounting mismatch: %+v", stats)
	}
	if stats.Inserted-stats.Drained-stats.Evicted-stats.Discarded != uint64(stats.Depth) {
		t.Fatalf("depth accounting mismatch: %+v", stats)
	}
	values, _ := b.Drain(t.Context(), 1)
	if len(values) != 1 || values[0] != "two" || b.Stats().Bytes != 0 {
		t.Fatalf("values=%v stats=%+v", values, b.Stats())
	}
}
func TestEventQueueRejectsByteOverflowBeforeAdmission(t *testing.T) {
	q := NewEventQueue[int](10, EventQueueDrop)
	q.byteLimit = 32
	if !q.TryPush(Event[int]{Value: 1}) || q.TryPush(Event[int]{Value: 2}) {
		t.Fatal("byte admission incorrect")
	}
	if _, ok := q.Receive(t.Context()); !ok {
		t.Fatal("missing admitted event")
	}
	if !q.TryPush(Event[int]{Value: 3}) {
		t.Fatal("dequeue did not free bytes")
	}
}

func TestByteRejectionInCoalesceModeIsStillADrop(t *testing.T) {
	q := NewEventQueue[int](1, EventQueueCoalesce)
	q.byteLimit = 1
	if q.TryPush(Event[int]{Value: 42}) {
		t.Fatal("over-budget trigger admitted")
	}
	stats := q.Snapshot()
	if stats.Queue.Dropped != 1 || stats.Coalesced != 0 {
		t.Fatalf("outcome=%+v", stats)
	}
}

func TestQueueZeroAndNegativeItemCapacityRejectWithoutBlocking(t *testing.T) {
	for _, capacity := range []int{0, -1} {
		q := NewEventQueue[int](capacity, EventQueueDrop)
		if q.TryPush(Event[int]{Value: 42}) {
			t.Fatal("zero-capacity queue admitted work")
		}
		q.Close()
		if _, ok := q.Receive(t.Context()); ok {
			t.Fatal("closed empty queue produced work")
		}
	}
}
