package websocketclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/semaphore"

	"github.com/formancehq/go-libs/v5/pkg/observe/job"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

var (
	ErrSubscriptionTimeout = errors.New("wsx: subscriptions not confirmed within timeout")
	ErrWatchdog            = errors.New("wsx: activity watchdog expired")
	ErrReconnectRequested  = errors.New("wsx: provider requested reconnect")
)

// FrameResult is the provider protocol's lifecycle interpretation of one frame.
// The provider remains responsible for decoding and enqueueing its data.
type FrameResult struct {
	Confirmed bool
	Activity  bool
	Reconnect bool
}

// Supervisor owns the complete provider-independent WebSocket lifecycle. Open
// performs provider setup, dialing, subscription writes, and recovery, then
// transfers ownership of the returned connection to the Supervisor.
type Supervisor struct {
	Logger logging.Logger
	Open   func(context.Context, int) (Conn, error)
	// HandleFrame is serial and must return promptly when its context is cancelled.
	// Transport failures cancel it independently; owned work is joined before retry.
	HandleFrame func(context.Context, []byte) FrameResult

	OnConnected     func(context.Context)
	OnActivity      func(time.Time)
	SetDisconnected func()
	OnReconnect     func(context.Context)

	Backoff          func(int) time.Duration
	StableAfter      time.Duration
	PingInterval     time.Duration
	PingTimeout      time.Duration
	SubscribeTimeout time.Duration
	WatchdogWindow   time.Duration
	Monitor          *SupervisorMonitor
	FrameQueueBytes  int

	// FrameJob runs each frame's HandleFrame as this connector-declared
	// observe/job job, normally <connector>.handle_frame. A frame result is
	// data, so the job never records an error. Connection setup is not a job:
	// goroutines Open starts outlive it and would inherit its profiler labels,
	// and the wsx.dial metrics already measure it. Zero leaves frames
	// uninstrumented.
	FrameJob job.Desc
}

func (s Supervisor) Run(ctx context.Context) {
	backoff := s.Backoff
	if backoff == nil {
		backoff = Backoff
	}
	stableAfter := durationOr(s.StableAfter, 30*time.Second)
	logger := s.logger()

	attempt := 0
	for ctx.Err() == nil {
		establishedFor, err := s.runSession(ctx, attempt)
		if s.SetDisconnected != nil {
			s.SetDisconnected()
		}
		if ctx.Err() != nil {
			return
		}
		if s.OnReconnect != nil {
			s.OnReconnect(ctx)
		}
		if establishedFor > stableAfter {
			attempt = 0
		}
		if err != nil {
			logger.WithContext(ctx).WithFields(map[string]any{"attempt": attempt, "error.type": fmt.Sprintf("%T", err)}).Error("ws connection ended; reconnecting")
		}

		timer := time.NewTimer(backoff(attempt))
		select {
		case <-ctx.Done():
			stopTimer(timer)

			return
		case <-timer.C:
		}
		attempt++
	}
}

func (s Supervisor) runAttempt(parent context.Context, attempt int) error {
	_, err := s.runSession(parent, attempt)
	return err
}

func (s Supervisor) runSession(parent context.Context, attempt int) (establishedFor time.Duration, resultErr error) {
	if s.Open == nil || s.HandleFrame == nil {
		return 0, errors.New("wsx: supervisor requires Open and HandleFrame")
	}

	ctx, cancel := context.WithCancel(parent)
	conn, err := s.Open(ctx, attempt)
	if err != nil {
		cancel()
		return 0, err
	}

	now := time.Now()
	lastActivity := now
	pauses := &admissionPause{}
	lastPause := time.Duration(0)
	if s.OnActivity != nil {
		s.OnActivity(now)
	}

	limit := s.FrameQueueBytes
	if limit <= 0 {
		limit = DefaultFrameQueueBytes
	}
	budget := semaphore.NewWeighted(int64(max(1, limit/budgetChunk)))
	frames := make(chan []byte, SupervisorFrameQueueCapacity)
	frameSlots := make(chan struct{}, SupervisorFrameQueueCapacity)
	for range SupervisorFrameQueueCapacity {
		frameSlots <- struct{}{}
	}
	attemptMonitor := s.Monitor.attempt()
	readErr := make(chan error, 1)
	var readWG sync.WaitGroup
	jobs := make(chan []byte)
	results := make(chan frameOutcome, 1)
	readWG.Go(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case frame := <-jobs:
				results <- runFrame(ctx, s.FrameJob, s.HandleFrame, frame, attemptMonitor)
			}
		}
	})
	readWG.Go(func() { readFrames(ctx, conn, frames, frameSlots, readErr, attemptMonitor, budget, limit, pauses) })
	defer func() {
		cancel()
		_ = conn.Close()
		readWG.Wait()
		attemptMonitor.attemptEnded(ctx)
	}()

	pingErr := make(chan error, 1)
	readWG.Go(func() {
		pingConnection(ctx, conn, durationOr(s.PingInterval, 15*time.Second), durationOr(s.PingTimeout, 10*time.Second), pingErr, pauses)
	})

	subscription := time.NewTimer(durationOr(s.SubscribeTimeout, 20*time.Second))
	defer stopTimer(subscription)
	subscriptionC := subscription.C
	watchdog := time.NewTicker(watchdogTick(durationOr(s.WatchdogWindow, 30*time.Second)))
	defer watchdog.Stop()
	confirmed := false
	defer func() {
		if confirmed {
			establishedFor = time.Since(now)
		}
	}()
	var receiving <-chan []byte = frames

	for {
		select {
		case <-ctx.Done():
			return 0, context.Cause(ctx)
		case err := <-readErr:
			return 0, fmt.Errorf("ws read: %w", err)
		case err := <-pingErr:
			return 0, fmt.Errorf("ws ping: %w", err)
		case frame := <-receiving:
			budget.Release(int64((len(frame) + budgetChunk - 1) / budgetChunk))
			attemptMonitor.handlingStarted(ctx)
			frameSlots <- struct{}{}
			receiving = nil
			jobs <- frame
		case outcome := <-results:
			if outcome.panicked {
				panic(outcome.panicValue)
			}
			receiving = frames
			result := outcome.result
			if result.Activity {
				lastActivity = time.Now()
				lastPause = pauses.elapsed()
				if s.OnActivity != nil {
					s.OnActivity(lastActivity)
				}
			}
			if result.Confirmed && !confirmed {
				confirmed = true
				stopTimer(subscription)
				subscriptionC = nil
				if s.OnConnected != nil {
					s.OnConnected(ctx)
				}
			}
			if result.Reconnect {
				return 0, ErrReconnectRequested
			}
		case <-subscriptionC:
			return 0, ErrSubscriptionTimeout
		case <-watchdog.C:
			if time.Since(lastActivity)-(pauses.elapsed()-lastPause) > durationOr(s.WatchdogWindow, 30*time.Second) {
				return 0, ErrWatchdog
			}
		}
	}
}

func handleFrame(
	ctx context.Context,
	frameJob job.Desc,
	handle func(context.Context, []byte) FrameResult,
	frame []byte,
	monitor *supervisorAttemptMonitor,
) (result FrameResult) {
	started := time.Now()
	completed := false
	defer func() {
		if completed {
			monitor.handlingFinished(started, len(frame), ctx)
			return
		}
		monitor.handlingAborted(ctx)
	}()
	if frameJob.Name == "" {
		result = handle(ctx, frame)
	} else {
		_ = job.Run(ctx, frameJob, func(ctx context.Context) error {
			result = handle(ctx, frame)

			return nil
		}, attribute.Int("bytes", len(frame)))
	}
	completed = true

	return result
}

func readFrames(
	ctx context.Context,
	conn Conn,
	frames chan<- []byte,
	frameSlots chan struct{},
	readErr chan<- error,
	monitor *supervisorAttemptMonitor,
	budget *semaphore.Weighted,
	limit int,
	pauseStates ...*admissionPause,
) {
	pauses := firstPause(pauseStates)
	for {
		select {
		case <-frameSlots:
		default:
			pauses.start()
			select {
			case <-frameSlots:
			case <-ctx.Done():
				pauses.stop()
				return
			}
			pauses.stop()
		case <-ctx.Done():
			return
		}

		frame, err := conn.Read(ctx)
		if err != nil {
			frameSlots <- struct{}{}
			select {
			case readErr <- err:
			case <-ctx.Done():
			}

			return
		}
		monitor.frameRead(len(frame), ctx)
		charge := int64((len(frame) + budgetChunk - 1) / budgetChunk)
		var admissionErr error
		if len(frame) > limit || charge > int64(max(1, limit/budgetChunk)) {
			admissionErr = errors.New("WebSocket frame exceeds queue byte budget")
		} else {
			if !budget.TryAcquire(charge) {
				pauses.start()
				admissionErr = budget.Acquire(ctx, charge)
				pauses.stop()
			}
		}
		if admissionErr != nil {
			frameSlots <- struct{}{}
			select {
			case readErr <- admissionErr:
			case <-ctx.Done():
			}
			return
		}
		select {
		case frames <- frame:
		case <-ctx.Done():
			budget.Release(int64((len(frame) + budgetChunk - 1) / budgetChunk))
			frameSlots <- struct{}{}
			return
		}
	}
}

func pingConnection(ctx context.Context, conn Conn, interval, timeout time.Duration, pingErr chan<- error, pauseStates ...*admissionPause) {
	pauses := firstPause(pauseStates)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if paused, _ := pauses.state(); paused {
				continue
			}
			err := pingWithAdmission(ctx, conn, timeout, pauses)
			if err != nil {
				select {
				case pingErr <- err:
				case <-ctx.Done():
				}

				return
			}
		}
	}
}

func (s Supervisor) logger() logging.Logger {
	if s.Logger != nil {
		return s.Logger
	}

	return logging.NopZap()
}

func durationOr(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}

	return fallback
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

// frameOutcome carries callback panics back to the owner after cancellation and
// cleanup; a callback is never abandoned in a detached goroutine.
type frameOutcome struct {
	result     FrameResult
	panicValue any
	panicked   bool
}

func runFrame(ctx context.Context, desc job.Desc, handle func(context.Context, []byte) FrameResult, frame []byte, monitor *supervisorAttemptMonitor) (out frameOutcome) {
	completed := false
	defer func() {
		if !completed {
			out.panicValue = recover()
			out.panicked = true
		}
	}()
	out.result = handleFrame(ctx, desc, handle, frame, monitor)
	completed = true
	return out
}

// A control-frame wait retains a cancellable context, but local admission pauses
// suspend its deadline. Never cancel an in-progress control write just because
// the reader paused; transport implementations may treat that as terminal.
func pingWithAdmission(ctx context.Context, conn Conn, timeout time.Duration, pauses *admissionPause) error {
	pingCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- conn.Ping(pingCtx) }()
	timer := time.NewTimer(timeout)
	defer stopTimer(timer)
	for {
		paused, changed := pauses.state()
		if paused {
			stopTimer(timer)
		}
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			cancel()
			<-done
			return context.Cause(ctx)
		case <-timer.C:
			if pausedNow, _ := pauses.state(); pausedNow {
				continue
			}
			cancel()
			<-done
			return context.DeadlineExceeded
		case <-changed:
			if pausedNow, _ := pauses.state(); !pausedNow {
				stopTimer(timer)
				timer.Reset(timeout)
			}
		}
	}
}
