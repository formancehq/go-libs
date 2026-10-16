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
	Logger      logging.Logger
	Open        func(context.Context, int) (Conn, error)
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
		started := time.Now()
		err := s.runAttempt(ctx, attempt)
		if s.SetDisconnected != nil {
			s.SetDisconnected()
		}
		if ctx.Err() != nil {
			return
		}
		if s.OnReconnect != nil {
			s.OnReconnect(ctx)
		}
		if time.Since(started) > stableAfter {
			attempt = 0
		}
		if err != nil {
			logger.WithContext(ctx).WithFields(map[string]any{"attempt": attempt, "error": err}).Error("ws connection ended; reconnecting")
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
	if s.Open == nil || s.HandleFrame == nil {
		return errors.New("wsx: supervisor requires Open and HandleFrame")
	}

	ctx, cancel := context.WithCancel(parent)
	conn, err := s.Open(ctx, attempt)
	if err != nil {
		cancel()
		return err
	}

	now := time.Now()
	lastActivity := now
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
	readWG.Go(func() { readFrames(ctx, conn, frames, frameSlots, readErr, attemptMonitor, budget, limit) })
	defer func() {
		cancel()
		_ = conn.Close()
		readWG.Wait()
		attemptMonitor.attemptEnded()
	}()

	pingErr := make(chan error, 1)
	go pingConnection(ctx, conn, durationOr(s.PingInterval, 15*time.Second), durationOr(s.PingTimeout, 10*time.Second), pingErr)

	subscription := time.NewTimer(durationOr(s.SubscribeTimeout, 20*time.Second))
	defer stopTimer(subscription)
	subscriptionC := subscription.C
	watchdog := time.NewTicker(watchdogTick(durationOr(s.WatchdogWindow, 30*time.Second)))
	defer watchdog.Stop()
	confirmed := false

	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case err := <-readErr:
			return fmt.Errorf("ws read: %w", err)
		case err := <-pingErr:
			return fmt.Errorf("ws ping: %w", err)
		case frame := <-frames:
			budget.Release(int64((len(frame) + budgetChunk - 1) / budgetChunk))
			attemptMonitor.handlingStarted()
			frameSlots <- struct{}{}
			result := handleFrame(ctx, s.FrameJob, s.HandleFrame, frame, attemptMonitor)
			if result.Activity {
				lastActivity = time.Now()
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
				return ErrReconnectRequested
			}
		case <-subscriptionC:
			return ErrSubscriptionTimeout
		case <-watchdog.C:
			if time.Since(lastActivity) > durationOr(s.WatchdogWindow, 30*time.Second) {
				return ErrWatchdog
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
			monitor.handlingFinished(started, len(frame))
			return
		}
		monitor.handlingAborted()
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
) {
	for {
		select {
		case <-frameSlots:
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
		monitor.frameRead(len(frame))
		charge := int64((len(frame) + budgetChunk - 1) / budgetChunk)
		var admissionErr error
		if len(frame) > limit || charge > int64(max(1, limit/budgetChunk)) {
			admissionErr = errors.New("WebSocket frame exceeds queue byte budget")
		} else {
			admissionErr = budget.Acquire(ctx, charge)
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

func pingConnection(ctx context.Context, conn Conn, interval, timeout time.Duration, pingErr chan<- error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, timeout)
			err := conn.Ping(pingCtx)
			cancel()
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
