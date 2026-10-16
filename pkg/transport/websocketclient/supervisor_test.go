package websocketclient

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/sync/semaphore"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

type supervisorRead struct {
	frame []byte
	err   error
}

type supervisorConn struct {
	reads   chan supervisorRead
	pingErr error
	closed  chan struct{}
	once    sync.Once
	closes  atomic.Int32
}

func newSupervisorConn(reads ...supervisorRead) *supervisorConn {
	c := &supervisorConn{reads: make(chan supervisorRead, len(reads)), closed: make(chan struct{})}
	for _, read := range reads {
		c.reads <- read
	}

	return c
}

func (c *supervisorConn) Read(ctx context.Context) ([]byte, error) {
	select {
	case read := <-c.reads:
		return read.frame, read.err
	case <-c.closed:
		return nil, errors.New("closed")
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}

func (*supervisorConn) Write(context.Context, []byte) error { return nil }
func (c *supervisorConn) Ping(context.Context) error        { return c.pingErr }
func (c *supervisorConn) Close() error {
	c.closes.Add(1)
	c.once.Do(func() { close(c.closed) })

	return nil
}

func testSupervisor(conn *supervisorConn, handle func([]byte) FrameResult) Supervisor {
	return Supervisor{
		Logger: logging.NopZap(),
		Open: func(context.Context, int) (Conn, error) {
			return conn, nil
		},
		HandleFrame:      func(_ context.Context, frame []byte) FrameResult { return handle(frame) },
		PingInterval:     time.Hour,
		SubscribeTimeout: time.Second,
		WatchdogWindow:   time.Second,
	}
}

func TestSupervisorAttemptHandlesFramesAndRequestedReconnect(t *testing.T) {
	conn := newSupervisorConn(
		supervisorRead{frame: []byte("ready")},
		supervisorRead{frame: []byte("activity")},
		supervisorRead{frame: []byte("reconnect")},
	)
	var connected, activities atomic.Int32
	s := testSupervisor(conn, func(frame []byte) FrameResult {
		switch string(frame) {
		case "ready":
			return FrameResult{Confirmed: true}
		case "activity":
			return FrameResult{Activity: true}
		default:
			return FrameResult{Reconnect: true}
		}
	})
	s.OnConnected = func(context.Context) { connected.Add(1) }
	s.OnActivity = func(time.Time) { activities.Add(1) }

	err := s.runAttempt(t.Context(), 0)
	if !errors.Is(err, ErrReconnectRequested) {
		t.Fatalf("error = %v, want requested reconnect", err)
	}
	if connected.Load() != 1 || activities.Load() != 2 {
		t.Fatalf("connected=%d activities=%d", connected.Load(), activities.Load())
	}
	if conn.closes.Load() != 1 {
		t.Fatalf("closes = %d, want 1", conn.closes.Load())
	}
}

func TestSupervisorAttemptReturnsReadError(t *testing.T) {
	readErr := errors.New("read failed")
	conn := newSupervisorConn(supervisorRead{err: readErr})
	err := testSupervisor(conn, func([]byte) FrameResult { return FrameResult{} }).runAttempt(t.Context(), 0)
	if !errors.Is(err, readErr) {
		t.Fatalf("error = %v, want read failure", err)
	}
}

func TestSupervisorAttemptReturnsPingError(t *testing.T) {
	pingErr := errors.New("ping failed")
	conn := newSupervisorConn()
	conn.pingErr = pingErr
	s := testSupervisor(conn, func([]byte) FrameResult { return FrameResult{} })
	s.PingInterval = time.Millisecond
	s.PingTimeout = time.Second

	err := s.runAttempt(t.Context(), 0)
	if !errors.Is(err, pingErr) {
		t.Fatalf("error = %v, want ping failure", err)
	}
}

func TestSupervisorAttemptRequiresConfirmation(t *testing.T) {
	conn := newSupervisorConn()
	s := testSupervisor(conn, func([]byte) FrameResult { return FrameResult{} })
	s.SubscribeTimeout = 5 * time.Millisecond

	if err := s.runAttempt(t.Context(), 0); !errors.Is(err, ErrSubscriptionTimeout) {
		t.Fatalf("error = %v, want subscription timeout", err)
	}
}

func TestSupervisorAttemptWatchesActivityAfterConfirmation(t *testing.T) {
	conn := newSupervisorConn(supervisorRead{frame: []byte("ready")})
	s := testSupervisor(conn, func([]byte) FrameResult { return FrameResult{Confirmed: true} })
	s.WatchdogWindow = 5 * time.Millisecond

	if err := s.runAttempt(t.Context(), 0); !errors.Is(err, ErrWatchdog) {
		t.Fatalf("error = %v, want watchdog expiry", err)
	}
}

func TestSupervisorAttemptCancellationClosesOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	conn := newSupervisorConn()
	s := testSupervisor(conn, func([]byte) FrameResult { return FrameResult{} })
	done := make(chan error, 1)
	go func() { done <- s.runAttempt(ctx, 0) }()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
	if conn.closes.Load() != 1 {
		t.Fatalf("closes = %d, want 1", conn.closes.Load())
	}
}

func TestSupervisorRetriesAndMarksDisconnected(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	var calls, disconnected, reconnects atomic.Int32
	Supervisor{
		Logger: logging.NopZap(),
		Open: func(context.Context, int) (Conn, error) {
			if calls.Add(1) == 2 {
				cancel()
			}

			return nil, errors.New("open failed")
		},
		HandleFrame:     func(context.Context, []byte) FrameResult { return FrameResult{} },
		SetDisconnected: func() { disconnected.Add(1) },
		OnReconnect:     func(context.Context) { reconnects.Add(1) },
		Backoff:         func(int) time.Duration { return time.Millisecond },
	}.Run(ctx)

	if calls.Load() != 2 || disconnected.Load() != 2 || reconnects.Load() != 1 {
		t.Fatalf("calls=%d disconnected=%d reconnects=%d", calls.Load(), disconnected.Load(), reconnects.Load())
	}
}

func TestSupervisorCancellationInterruptsBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		Supervisor{
			Logger:      logging.NopZap(),
			Open:        func(context.Context, int) (Conn, error) { return nil, errors.New("open failed") },
			HandleFrame: func(context.Context, []byte) FrameResult { return FrameResult{} },
			Backoff:     func(int) time.Duration { return time.Hour },
		}.Run(ctx)
		close(done)
	}()
	time.Sleep(time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not stop during backoff")
	}
}

func TestSupervisorMonitorMeasuresBackpressureAndAbandonedFrames(t *testing.T) {
	reads := make([]supervisorRead, SupervisorFrameQueueCapacity+44)
	for i := range reads {
		reads[i] = supervisorRead{frame: []byte{'x'}}
	}
	conn := newSupervisorConn(reads...)
	monitor := NewSupervisorMonitor()
	entered := make(chan struct{})
	release := make(chan struct{})
	s := testSupervisor(conn, func([]byte) FrameResult {
		close(entered)
		<-release

		return FrameResult{Reconnect: true}
	})
	s.Monitor = monitor
	done := make(chan error, 1)
	go func() { done <- s.runAttempt(t.Context(), 0) }()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler did not receive the first frame")
	}
	deadline := time.Now().Add(time.Second)
	for monitor.Snapshot().FrameQueueDepth != SupervisorFrameQueueCapacity && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	stats := monitor.Snapshot()
	assertSupervisorConservation(t, stats)
	if stats.FramesRead != SupervisorFrameQueueCapacity+1 {
		t.Fatalf("frames read = %d, want %d", stats.FramesRead, SupervisorFrameQueueCapacity+1)
	}
	if stats.BytesRead != SupervisorFrameQueueCapacity+1 {
		t.Fatalf("bytes read = %d, want %d", stats.BytesRead, SupervisorFrameQueueCapacity+1)
	}
	if stats.FrameQueueDepth != SupervisorFrameQueueCapacity || stats.FrameQueueHighWater != SupervisorFrameQueueCapacity {
		t.Fatalf("frame queue depth/high-water = %d/%d, want %d/%d", stats.FrameQueueDepth, stats.FrameQueueHighWater, SupervisorFrameQueueCapacity, SupervisorFrameQueueCapacity)
	}
	if stats.HandlingInFlight != 1 || stats.FramesHandled != 0 {
		t.Fatalf("handling in-flight/handled = %d/%d, want 1/0", stats.HandlingInFlight, stats.FramesHandled)
	}
	if stats.FramePerformance.InFlight != 1 || stats.FramePerformance.HighWater != 1 {
		t.Fatalf("performance in-flight/high-water = %d/%d, want 1/1", stats.FramePerformance.InFlight, stats.FramePerformance.HighWater)
	}

	close(release)
	if err := <-done; !errors.Is(err, ErrReconnectRequested) {
		t.Fatalf("error = %v, want requested reconnect", err)
	}
	stats = monitor.Snapshot()
	assertSupervisorConservation(t, stats)
	if stats.FramesRead != SupervisorFrameQueueCapacity+1 || stats.FramesHandled != 1 {
		t.Fatalf("frames read/handled = %d/%d, want %d/1", stats.FramesRead, stats.FramesHandled, SupervisorFrameQueueCapacity+1)
	}
	if stats.FramesAbandoned != SupervisorFrameQueueCapacity {
		t.Fatalf("frames abandoned = %d, want %d", stats.FramesAbandoned, SupervisorFrameQueueCapacity)
	}
	if stats.FrameQueueDepth != 0 || stats.HandlingInFlight != 0 {
		t.Fatalf("final queue/in-flight = %d/%d, want 0/0", stats.FrameQueueDepth, stats.HandlingInFlight)
	}
	if stats.HandlingDurationCount != 1 || stats.HandlingDurationTotal <= 0 ||
		stats.HandlingDurationLast != stats.HandlingDurationTotal ||
		stats.HandlingDurationMax != stats.HandlingDurationTotal {
		t.Fatalf("handling durations = count %d total %s last %s max %s", stats.HandlingDurationCount, stats.HandlingDurationTotal, stats.HandlingDurationLast, stats.HandlingDurationMax)
	}
	performanceTotals := stats.FramePerformance.Totals
	if stats.FramePerformance.Name != "websocket.frames" || stats.FramePerformance.Channel != "websocket" ||
		performanceTotals.Calls != 1 || performanceTotals.Errors != 0 ||
		performanceTotals.ItemsIn != 1 || performanceTotals.ItemsOut != 1 ||
		stats.FramePerformance.Latency.Observed != 1 {
		t.Fatalf("frame performance = %+v", stats.FramePerformance)
	}
}

func TestSupervisorMonitorCountsPanickedFrameAsAbandoned(t *testing.T) {
	conn := newSupervisorConn(supervisorRead{frame: []byte("panic")})
	monitor := NewSupervisorMonitor()
	s := testSupervisor(conn, func([]byte) FrameResult { panic("handler panic") })
	s.Monitor = monitor
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = s.runAttempt(t.Context(), 0)
	}()
	if recovered == nil {
		t.Fatal("provider panic did not propagate")
	}

	stats := monitor.Snapshot()
	assertSupervisorConservation(t, stats)
	if stats.FramesRead != 1 || stats.FramesHandled != 0 || stats.FramesAbandoned != 1 {
		t.Fatalf("frames read/handled/abandoned = %d/%d/%d, want 1/0/1", stats.FramesRead, stats.FramesHandled, stats.FramesAbandoned)
	}
	if stats.HandlingInFlight != 0 || stats.FrameQueueDepth != 0 {
		t.Fatalf("in-flight/queue = %d/%d, want 0/0", stats.HandlingInFlight, stats.FrameQueueDepth)
	}
	totals := stats.FramePerformance.Totals
	if totals.Calls != 0 || totals.Errors != 0 || totals.ItemsIn != 0 || totals.ItemsOut != 0 || stats.FramePerformance.InFlight != 0 {
		t.Fatalf("frame performance after panic = %+v", stats.FramePerformance)
	}
}

func TestSupervisorMonitorPerFrameRecordingDoesNotAllocate(t *testing.T) {
	monitor := NewSupervisorMonitor()
	attempt := monitor.attempt()
	allocs := testing.AllocsPerRun(1000, func() {
		attempt.frameRead(42)
		attempt.handlingStarted()
		started := time.Now()
		attempt.handlingFinished(started, 42)
	})
	if allocs != 0 {
		t.Fatalf("per-frame monitor allocations = %f, want 0", allocs)
	}
}

func TestSupervisorMonitorSnapshotConservationUnderConcurrency(t *testing.T) {
	const frameCount = 512
	reads := make([]supervisorRead, frameCount)
	for i := range reads {
		reads[i] = supervisorRead{frame: []byte("frame")}
	}
	conn := newSupervisorConn(reads...)
	monitor := NewSupervisorMonitor()
	var handled atomic.Uint64
	s := testSupervisor(conn, func([]byte) FrameResult {
		runtime.Gosched()

		return FrameResult{Reconnect: handled.Add(1) == frameCount}
	})
	s.Monitor = monitor
	done := make(chan error, 1)
	go func() { done <- s.runAttempt(t.Context(), 0) }()

	for {
		assertSupervisorConservation(t, monitor.Snapshot())
		select {
		case err := <-done:
			if !errors.Is(err, ErrReconnectRequested) {
				t.Fatalf("error = %v, want requested reconnect", err)
			}
			stats := monitor.Snapshot()
			assertSupervisorConservation(t, stats)
			if stats.FramesRead != frameCount || stats.FramesHandled != frameCount || stats.FramesAbandoned != 0 {
				t.Fatalf("final frames = read %d handled %d abandoned %d", stats.FramesRead, stats.FramesHandled, stats.FramesAbandoned)
			}

			return
		default:
			runtime.Gosched()
		}
	}
}

func assertSupervisorConservation(t *testing.T, stats SupervisorStats) {
	t.Helper()
	if stats.FrameQueueDepth < 0 || stats.HandlingInFlight < 0 {
		t.Fatalf("negative gauges: queue %d handling %d", stats.FrameQueueDepth, stats.HandlingInFlight)
	}
	resolved := stats.FramesHandled + stats.FramesAbandoned +
		uint64(stats.FrameQueueDepth) + uint64(stats.HandlingInFlight)
	if stats.FramesRead != resolved {
		t.Fatalf("frame conservation: read %d != handled %d + abandoned %d + queued %d + in-flight %d",
			stats.FramesRead, stats.FramesHandled, stats.FramesAbandoned, stats.FrameQueueDepth, stats.HandlingInFlight)
	}
	if stats.HandlingDurationCount != stats.FramesHandled {
		t.Fatalf("duration count %d != handled %d", stats.HandlingDurationCount, stats.FramesHandled)
	}
	if stats.HandlingDurationLast > stats.HandlingDurationMax || stats.HandlingDurationMax > stats.HandlingDurationTotal {
		t.Fatalf("impossible durations: last %s max %s total %s", stats.HandlingDurationLast, stats.HandlingDurationMax, stats.HandlingDurationTotal)
	}
	if stats.FrameQueueHighWater > stats.FrameQueueCapacity {
		t.Fatalf("frame queue high-water %d > capacity %d", stats.FrameQueueHighWater, stats.FrameQueueCapacity)
	}
}

func BenchmarkSupervisorMonitorFrameLifecycle(b *testing.B) {
	monitor := NewSupervisorMonitor()
	attempt := monitor.attempt()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		attempt.frameRead(42)
		attempt.handlingStarted()
		attempt.handlingFinished(time.Now(), 42)
	}
}

// Exercise admission at the actual socket-reader boundary, including reservations
// retained by queued frames and returned when a send is cancelled.
func TestReadFramesByteAdmission(t *testing.T) {
	for _, tc := range []struct {
		name        string
		limit, size int
		rejected    bool
	}{
		{name: "zero", limit: budgetChunk, size: 0},
		{name: "exact", limit: 2 * budgetChunk, size: 2 * budgetChunk},
		{name: "rounded", limit: 2 * budgetChunk, size: budgetChunk + 1},
		{name: "minimum capacity", limit: 1, size: 1},
		{name: "byte overrun", limit: budgetChunk, size: budgetChunk + 1, rejected: true},
		{name: "chunk overrun", limit: budgetChunk + 1, size: budgetChunk + 1, rejected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				conn := newSupervisorConn(supervisorRead{frame: make([]byte, tc.size)})
				budget := semaphore.NewWeighted(int64(max(1, tc.limit/budgetChunk)))
				frames := make(chan []byte) // Force the reader to retain its reservation.
				slots := make(chan struct{}, 1)
				slots <- struct{}{}
				readErr := make(chan error, 1)
				done := make(chan struct{})
				go func() {
					defer close(done)
					readFrames(ctx, conn, frames, slots, readErr, NewSupervisorMonitor().attempt(), budget, tc.limit)
				}()
				synctest.Wait()
				if tc.rejected {
					select {
					case err := <-readErr:
						if err.Error() != "WebSocket frame exceeds queue byte budget" {
							t.Fatal(err)
						}
					default:
						t.Fatal("oversized frame did not fail admission")
					}
				} else {
					charge := int64((tc.size + budgetChunk - 1) / budgetChunk)
					capacity := int64(max(1, tc.limit/budgetChunk))
					if charge > 0 && budget.TryAcquire(capacity) {
						t.Fatal("reader did not reserve queued bytes")
					}
				}
				cancel()
				<-done
				if !budget.TryAcquire(int64(max(1, tc.limit/budgetChunk))) {
					t.Fatal("rejection or cancelled send leaked byte capacity")
				}
			})
		})
	}
}

func TestSupervisorByteBackpressureReleasesOnDequeueAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		conn := newSupervisorConn(
			supervisorRead{frame: []byte("first")},
			supervisorRead{frame: make([]byte, budgetChunk+1)},
			supervisorRead{frame: []byte("last")},
		)
		entered := make(chan int, 3)
		proceed := make(chan struct{})
		var handled int
		s := testSupervisor(conn, func([]byte) FrameResult {
			handled++
			entered <- handled
			select {
			case <-proceed:
			case <-ctx.Done():
			}
			return FrameResult{Confirmed: true}
		})
		s.FrameQueueBytes = 2 * budgetChunk
		s.Monitor = NewSupervisorMonitor()
		done := make(chan error, 1)
		go func() { done <- s.runAttempt(ctx, 0) }()
		if n := <-entered; n != 1 {
			t.Fatalf("first handler = %d", n)
		}
		synctest.Wait()
		stats := s.Monitor.Snapshot()
		// The second frame consumes both chunks, leaving the third read waiting
		// for admission. Item capacity alone would allow both to be queued.
		if stats.FramesRead != 3 || stats.FrameQueueDepth != 2 {
			t.Fatalf("byte backpressure not reached: %+v", stats)
		}
		proceed <- struct{}{}
		if n := <-entered; n != 2 {
			t.Fatalf("second handler = %d", n)
		}
		synctest.Wait()
		proceed <- struct{}{}
		if n := <-entered; n != 3 {
			t.Fatalf("dequeue failed to free byte capacity: %d", n)
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
		if conn.closes.Load() != 1 {
			t.Fatal("cancel did not close connection exactly once")
		}
	})
}

func TestReadFramesCancelledAdmissionReturnsCapacity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		conn := newSupervisorConn(supervisorRead{frame: make([]byte, 2*budgetChunk)})
		budget := semaphore.NewWeighted(2)
		if err := budget.Acquire(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		slots := make(chan struct{}, 1)
		slots <- struct{}{}
		done := make(chan struct{})
		go func() {
			defer close(done)
			readFrames(ctx, conn, make(chan []byte), slots, make(chan error, 1), NewSupervisorMonitor().attempt(), budget, 2*budgetChunk)
		}()
		synctest.Wait()
		cancel()
		<-done
		budget.Release(1)
		if !budget.TryAcquire(2) {
			t.Fatal("cancelled admission leaked capacity")
		}
	})
}

func TestReadFramesByteBudgetBlocksUntilDequeue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		conn := newSupervisorConn(
			supervisorRead{frame: []byte("first")},
			supervisorRead{frame: make([]byte, budgetChunk+1)},
		)
		budget := semaphore.NewWeighted(2)
		frames := make(chan []byte, 3)
		slots := make(chan struct{}, 3)
		for range 3 {
			slots <- struct{}{}
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			readFrames(ctx, conn, frames, slots, make(chan error, 1), NewSupervisorMonitor().attempt(), budget, 2*budgetChunk)
		}()
		synctest.Wait()
		if len(frames) != 1 {
			t.Fatalf("queued %d frames; byte admission must hold second frame", len(frames))
		}
		frame := <-frames
		budget.Release(int64((len(frame) + budgetChunk - 1) / budgetChunk))
		synctest.Wait()
		if len(frames) != 1 {
			t.Fatal("dequeue did not unblock second frame")
		}
		frame = <-frames
		if len(frame) != budgetChunk+1 {
			t.Fatalf("second frame length: %d", len(frame))
		}
		budget.Release(int64((len(frame) + budgetChunk - 1) / budgetChunk))
		cancel()
		<-done
		if !budget.TryAcquire(2) {
			t.Fatal("dequeue leaked rounded charge")
		}
	})
}

func TestSupervisorRunResetsBackoffAfterStableSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		conn := newSupervisorConn(supervisorRead{frame: []byte("reconnect")})
		var attempts, delays []int
		var disconnected, reconnects int
		Supervisor{
			Open: func(_ context.Context, attempt int) (Conn, error) {
				attempts = append(attempts, attempt)
				switch len(attempts) {
				case 1:
					return nil, errors.New("initial dial failed")
				case 2:
					return conn, nil
				default:
					cancel()
					return nil, context.Canceled
				}
			},
			HandleFrame: func(context.Context, []byte) FrameResult {
				time.Sleep(2 * time.Second)
				return FrameResult{Confirmed: true, Reconnect: true}
			},
			StableAfter: time.Second,
			Backoff: func(attempt int) time.Duration {
				delays = append(delays, attempt)
				return time.Millisecond
			},
			SetDisconnected: func() { disconnected++ },
			OnReconnect:     func(context.Context) { reconnects++ },
		}.Run(ctx)
		if !slices.Equal(attempts, []int{0, 1, 1}) || !slices.Equal(delays, []int{0, 0}) {
			t.Fatalf("open attempts %v, backoff attempts %v", attempts, delays)
		}
		if disconnected != 3 || reconnects != 2 || conn.closes.Load() != 1 {
			t.Fatalf("disconnected=%d reconnects=%d closes=%d", disconnected, reconnects, conn.closes.Load())
		}
	})
}
