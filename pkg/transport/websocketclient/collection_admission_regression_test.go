package websocketclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
)

type collectionMarshalProbe struct {
	run func()
	err error
}

func (p collectionMarshalProbe) MarshalJSON() ([]byte, error) {
	if p.run != nil {
		p.run()
	}
	if p.err != nil {
		return nil, p.err
	}
	return []byte(`{}`), nil
}

// A regressed lock must terminate only its subprocess, not leave blocked test
// goroutines or prevent cleanup in the parent race suite.
func TestCollectionMarshalCanInspectSameCollection(t *testing.T) {
	const childEnv = "GOLIBS_COLLECTION_MARSHAL_PROBE"
	if mode := os.Getenv(childEnv); mode != "" {
		switch mode {
		case "queue":
			q := NewEventQueue[collectionMarshalProbe](1, EventQueueDrop)
			defer q.Close()
			if !q.TryPush(Event[collectionMarshalProbe]{Value: collectionMarshalProbe{run: func() { _ = q.Snapshot() }}}) {
				t.Fatal("queue rejected probe")
			}
		case "buffer":
			b := NewKeyedBuffer[collectionMarshalProbe](1)
			b.Push("key", collectionMarshalProbe{run: func() { _ = b.Stats() }})
			if b.Stats().Inserted != 1 {
				t.Fatal("buffer rejected probe")
			}
		default:
			t.Fatal("unknown probe")
		}
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"queue", "buffer"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			// #nosec G204 -- current test binary, fixed test arguments, no shell.
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestCollectionMarshalCanInspectSameCollection$", "-test.timeout=2s")
			cmd.Env = append(os.Environ(), childEnv+"="+mode)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("collection inspection during MarshalJSON failed: %v\n%s", err, output)
			}
		})
	}
}

func TestEventQueueCoalesceRequiresRetainedTrigger(t *testing.T) {
	for _, capacity := range []int{0, -1} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			q := NewEventQueue[int](capacity, EventQueueCoalesce)
			if q.TryPush(Event[int]{Value: 1}) {
				t.Fatal("zero-capacity admission")
			}
			s := q.Snapshot()
			if s.Queue.Dropped != 1 || s.Coalesced != 0 || s.Queue.Depth != 0 || s.Queue.Enqueued != 0 {
				t.Fatalf("no retained trigger: %+v", s)
			}
			q.Close()
			if q.TryPush(Event[int]{Value: 2}) {
				t.Fatal("closed admission")
			}
			s = q.Snapshot()
			if s.RejectedClosed != 1 || s.Queue.Dropped != 1 || s.Coalesced != 0 {
				t.Fatalf("closed precedence: %+v", s)
			}
		})
	}
}

func TestEventQueueCloseDuringMarshalingWinsOverByteRejection(t *testing.T) {
	for _, fail := range []bool{false, true} {
		q := NewEventQueue[collectionMarshalProbe](1, EventQueueCoalesce)
		q.byteLimit = 1
		entered, release, done := make(chan struct{}), make(chan struct{}), make(chan bool, 1)
		value := collectionMarshalProbe{run: func() { close(entered); <-release }}
		if fail {
			value.err = errors.New("synthetic encoding failure")
		}
		go func() { done <- q.TryPush(Event[collectionMarshalProbe]{Value: value}) }()
		<-entered
		q.Close()
		close(release)
		if <-done {
			t.Fatal("admitted after close")
		}
		s := q.Snapshot()
		if s.RejectedClosed != 1 || s.Queue.Dropped != 0 || s.Coalesced != 0 || s.Queue.Enqueued != 0 {
			t.Fatalf("close lost precedence: %+v", s)
		}
	}
	q := NewEventQueue[collectionMarshalProbe](1, EventQueueDrop)
	q.Close()
	called := false
	q.TryPush(Event[collectionMarshalProbe]{Value: collectionMarshalProbe{run: func() { called = true }}})
	if called || q.Snapshot().RejectedClosed != 1 {
		t.Fatal("already closed queue serialized a value")
	}
}

func TestKeyedBufferBulkByteEvictionKeepsReplacementAndSurvivors(t *testing.T) {
	for _, key := range []string{"a", "b", "e", "new"} {
		t.Run(key, func(t *testing.T) {
			b := NewKeyedBuffer[json.RawMessage](10)
			b.byteLimit = 20
			for _, k := range []string{"a", "b", "c", "d", "e"} {
				b.Push(k, json.RawMessage(`0`))
			}
			original := b.entries[key]
			origin := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}})
			replacement := json.RawMessage(`123456789012345`)
			b.Push(key, replacement, origin)
			expected := map[string][]string{"a": {"a", "d", "e"}, "b": {"b", "d", "e"}, "e": {"c", "d", "e"}, "new": {"e", "new"}}[key]
			// The new three-byte key costs 18 bytes; one two-byte old entry survives.
			if !slices.Equal(b.order, expected) {
				t.Fatalf("order=%v want=%v", b.order, expected)
			}
			entry := b.entries[key]
			if key != "new" && entry.queuedAtOffset != original.queuedAtOffset {
				t.Fatal("replacement age changed")
			}
			if !entry.link.Equal(origin) {
				t.Fatal("latest origin lost")
			}
			s := b.Stats()
			wantBytes := 20
			if s.Bytes != wantBytes || s.Pushed != s.Inserted+s.Replaced+s.Rejected || s.Inserted-s.Evicted-s.Drained-s.Discarded != uint64(s.Depth) {
				t.Fatalf("accounting=%+v", s)
			}
			wantValues := make([]json.RawMessage, len(expected))
			for i, survivor := range expected {
				wantValues[i] = b.entries[survivor].value
			}
			values, more := b.Drain(t.Context(), 0)
			for i, value := range values {
				if string(value) != string(wantValues[i]) {
					t.Fatal("drain changed survivor order or value")
				}
			}
			if more || len(values) != len(expected) || b.Stats().Bytes != 0 {
				t.Fatal("drain accounting")
			}
		})
	}
}

func BenchmarkKeyedBufferBulkByteEviction(b *testing.B) {
	for _, n := range []int{1000, 10000, 50000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			keys := make([]string, n)
			for i := range keys {
				keys[i] = fmt.Sprintf("%08d", i)
			}
			value := json.RawMessage(`0`)
			limit := n * 9
			large := make(json.RawMessage, limit-9)
			for i := range large {
				large[i] = '1'
			}
			for b.Loop() {
				b.StopTimer()
				buffer := NewKeyedBuffer[json.RawMessage](n + 1)
				buffer.byteLimit = limit
				for _, key := range keys {
					buffer.Push(key, value)
				}
				b.StartTimer()
				buffer.Push("new", large)
			}
		})
	}
}

func TestEventQueueConcurrentEncodingStillAdmitsBytesAtomically(t *testing.T) {
	q := NewEventQueue[collectionMarshalProbe](2, EventQueueDrop)
	defer q.Close()
	q.byteLimit = 2
	entered, release, done := make(chan struct{}, 2), make(chan struct{}), make(chan bool, 2)
	for range 2 {
		go func() {
			done <- q.TryPush(Event[collectionMarshalProbe]{Value: collectionMarshalProbe{run: func() { entered <- struct{}{}; <-release }}})
		}()
	}
	<-entered
	<-entered
	close(release)
	accepted := 0
	for range 2 {
		if <-done {
			accepted++
		}
	}
	s := q.Snapshot()
	if accepted != 1 || s.Queue.Depth != 1 || s.Queue.Enqueued != 1 || s.Queue.Dropped != 1 || s.Queue.Bytes != 2 {
		t.Fatalf("atomic byte admission: %+v accepted=%d", s, accepted)
	}
}
