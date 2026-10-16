package websocketclient

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/sync/semaphore"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

func TestSlowFailedOpenAndReconnectDoNotResetBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var attempts []int
		Supervisor{Open: func(_ context.Context, n int) (Conn, error) {
			attempts = append(attempts, n)
			time.Sleep(2 * time.Second)
			if len(attempts) == 4 {
				cancel()
			}
			return nil, errors.New("failed")
		}, HandleFrame: func(context.Context, []byte) FrameResult { return FrameResult{} }, StableAfter: time.Second, OnReconnect: func(context.Context) { time.Sleep(3 * time.Second) }, Backoff: func(int) time.Duration { return time.Millisecond }}.Run(ctx)
		for i, n := range attempts {
			if i != n {
				t.Fatalf("attempts=%v", attempts)
			}
		}
	})
}
func TestRetryLogDoesNotExposeSetupURLOrErrorText(t *testing.T) {
	var buffer bytes.Buffer
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	Supervisor{Logger: logging.NewDefaultLoggerWithLevel(&buffer, logging.DebugLevel, true, false), Open: func(context.Context, int) (Conn, error) {
		calls++
		if calls == 2 {
			cancel()
		}
		return nil, &url.Error{Op: "dial", URL: "wss://example.test/secret-path?token=secret-query", Err: errors.New("secret-detail")}
	}, HandleFrame: func(context.Context, []byte) FrameResult { return FrameResult{} }, Backoff: func(int) time.Duration { return time.Nanosecond }}.Run(ctx)
	if buffer.Len() == 0 {
		t.Fatal("logger emitted nothing")
	}
	for _, secret := range []string{"secret-path", "secret-query", "secret-detail"} {
		if strings.Contains(buffer.String(), secret) {
			t.Fatal("retry log exposed setup error")
		}
	}
	if !strings.Contains(buffer.String(), "error.type") {
		t.Fatal("safe error classification missing")
	}
}
func TestSessionFailureCancelsAndJoinsBlockedFrame(t *testing.T) {
	for _, failure := range []string{"ping", "subscription", "watchdog"} {
		t.Run(failure, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				conn := newSupervisorConn(supervisorRead{frame: []byte("frame")})
				entered, finished := make(chan struct{}), make(chan struct{})
				s := testSupervisor(conn, func([]byte) FrameResult { return FrameResult{} })
				s.HandleFrame = func(ctx context.Context, _ []byte) FrameResult {
					close(entered)
					<-ctx.Done()
					close(finished)
					return FrameResult{}
				}
				s.SubscribeTimeout = time.Hour
				s.WatchdogWindow = time.Hour
				switch failure {
				case "ping":
					conn.pingErr = errors.New("ping failed")
					s.PingInterval = time.Second
				case "subscription":
					s.SubscribeTimeout = time.Second
				case "watchdog":
					s.WatchdogWindow = time.Second
				}
				done := make(chan error, 1)
				go func() { done <- s.runAttempt(t.Context(), 0) }()
				<-entered
				time.Sleep(3 * time.Second)
				select {
				case err := <-done:
					if err == nil {
						t.Fatal("missing session failure")
					}
				default:
					t.Fatal("blocked handler hid session failure")
				}
				select {
				case <-finished:
				default:
					t.Fatal("owned handler not joined")
				}
				if conn.closes.Load() != 1 {
					t.Fatal("connection close ownership changed")
				}
			})
		})
	}
}
func TestAdmissionPauseDefersHeartbeatFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pauses := &admissionPause{}
		conn := newSupervisorConn()
		pauses.start()
		if paused, _ := pauses.state(); !paused {
			t.Fatal("pause not observed")
		}
		time.Sleep(time.Second)
		pauses.stop()
		if pauses.elapsed() != time.Second {
			t.Fatal("watchdog pause accounting changed")
		}
		if paused, _ := pauses.state(); paused {
			t.Fatal("heartbeat did not resume")
		}
		if err := pingWithAdmission(t.Context(), conn, time.Second, pauses); err != nil {
			t.Fatal(err)
		}
	})
}

func TestRealPeerSurvivesByteAdmissionPauseAndPingResumes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := conn.CloseRead(r.Context())
		for range 2 {
			if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, budgetChunk)); err != nil {
				return
			}
		}
		<-ctx.Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := NewDialer("")(ctx, "ws"+strings.TrimPrefix(server.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	frames := make(chan []byte, 3)
	slots := make(chan struct{}, 3)
	for range 3 {
		slots <- struct{}{}
	}
	pauses := &admissionPause{}
	budget := semaphore.NewWeighted(1)
	errs := make(chan error, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		readFrames(ctx, conn, frames, slots, errs, NewSupervisorMonitor().attempt(), budget, budgetChunk, pauses)
	}()
	deadline := time.Now().Add(time.Second)
	for {
		if paused, _ := pauses.state(); paused {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reader never reached byte admission")
		}
		time.Sleep(time.Millisecond)
	}
	// Exactly one frame is admitted and one full frame is pending outside budget.
	if len(frames) != 1 {
		t.Fatalf("admitted frames=%d", len(frames))
	}
	pingDone := make(chan error, 1)
	go func() { pingDone <- pingWithAdmission(ctx, conn, 20*time.Millisecond, pauses) }()
	time.Sleep(60 * time.Millisecond)
	select {
	case err := <-pingDone:
		t.Fatalf("local backpressure ended heartbeat: %v", err)
	default:
	}
	<-frames
	budget.Release(1)
	select {
	case err := <-pingDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("ping did not resume")
	}
	cancel()
	<-readerDone
}
