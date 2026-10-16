package websocketclient

import (
	"context"
	"testing"
)

func TestIndependentCanceledDispatchConservesHandling(t *testing.T) {
	for i := 0; i < 3000; i++ {
		ctx, cancel := context.WithCancel(t.Context())
		conn := newSupervisorConn(supervisorRead{frame: []byte("one")}, supervisorRead{frame: []byte("two")}, supervisorRead{frame: []byte("three")})
		m := NewSupervisorMonitor()
		s := testSupervisor(conn, func([]byte) FrameResult { cancel(); return FrameResult{} })
		s.Monitor = m
		_ = s.runAttempt(ctx, 0)
		cancel()
		got := m.Snapshot()
		if got.HandlingInFlight != 0 || got.FrameQueueDepth != 0 || got.FramesRead != got.FramesHandled+got.FramesAbandoned {
			t.Fatalf("iteration %d: unresolved after joined cancellation: %+v", i, got)
		}
	}
}
