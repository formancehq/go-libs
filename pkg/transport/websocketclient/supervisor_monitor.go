package websocketclient

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/formancehq/go-libs/v5/pkg/observe/performance"
)

// SupervisorFrameQueueCapacity is the maximum number of successfully read
// WebSocket frames waiting for the provider's HandleFrame callback. A full
// queue applies backpressure to the connection reader; it does not drop a
// frame.
const SupervisorFrameQueueCapacity = 256

// SupervisorStats is a race-safe cumulative snapshot of the transport work
// performed by one Supervisor. FrameQueueDepth is the current read-to-handle
// backlog. FramesAbandoned counts frames that were read successfully but could
// not be handled before their connection attempt ended.
type SupervisorStats struct {
	FramesRead            uint64
	FramesHandled         uint64
	BytesRead             uint64
	FrameQueueDepth       int
	FrameQueueCapacity    int
	FrameQueueHighWater   int
	FramesAbandoned       uint64
	HandlingDurationCount uint64
	HandlingDurationTotal time.Duration
	HandlingDurationMax   time.Duration
	HandlingDurationLast  time.Duration
	HandlingInFlight      int
	FramePerformance      performance.StageSnapshot
}

// SupervisorMonitor records low-overhead, allocation-free per-frame counters
// for one Supervisor. Keep it behind a pointer: Supervisor intentionally has
// value receivers and is commonly constructed as a literal.
type SupervisorMonitor struct {
	mu                  sync.Mutex
	state               supervisorMonitorState
	performanceRecorder *performance.Recorder
	performanceStage    *performance.Stage
}

type supervisorMonitorState struct {
	framesRead            uint64
	framesHandled         uint64
	bytesRead             uint64
	frameQueueDepth       int
	frameQueueHighWater   int
	framesAbandoned       uint64
	handlingDurationCount uint64
	handlingDurationTotal time.Duration
	handlingDurationMax   time.Duration
	handlingDurationLast  time.Duration
	handlingInFlight      int
}

// NewSupervisorMonitor returns an empty monitor suitable for one Supervisor.
func NewSupervisorMonitor() *SupervisorMonitor {
	recorder := performance.New(performance.Config{})

	return &SupervisorMonitor{
		performanceRecorder: recorder,
		performanceStage:    recorder.Stage("websocket.frames", "websocket"),
	}
}

// Snapshot returns cumulative totals and current queue/handler gauges copied at
// one monitor linearization point. The retained rolling performance stage is
// then copied independently; it does not participate in frame conservation.
func (m *SupervisorMonitor) Snapshot() SupervisorStats {
	if m == nil {
		return SupervisorStats{FrameQueueCapacity: SupervisorFrameQueueCapacity}
	}

	m.mu.Lock()
	state := m.state
	m.mu.Unlock()
	stats := SupervisorStats{
		FramesRead:            state.framesRead,
		FramesHandled:         state.framesHandled,
		BytesRead:             state.bytesRead,
		FrameQueueDepth:       state.frameQueueDepth,
		FrameQueueCapacity:    SupervisorFrameQueueCapacity,
		FrameQueueHighWater:   state.frameQueueHighWater,
		FramesAbandoned:       state.framesAbandoned,
		HandlingDurationCount: state.handlingDurationCount,
		HandlingDurationTotal: state.handlingDurationTotal,
		HandlingDurationMax:   state.handlingDurationMax,
		HandlingDurationLast:  state.handlingDurationLast,
		HandlingInFlight:      state.handlingInFlight,
	}
	if m.performanceRecorder != nil {
		stages := m.performanceRecorder.Snapshot(time.Now()).Stages
		if len(stages) > 0 {
			stats.FramePerformance = stages[0]
		}
	}

	return stats
}

// supervisorAttemptMonitor separates per-attempt outstanding work from the
// cumulative monitor. It lets attempt shutdown account for every successfully
// read but unhandled frame exactly, including a frame read while cancellation
// raced with delivery to the channel.
type supervisorAttemptMonitor struct {
	monitor   *SupervisorMonitor
	read      atomic.Uint64
	handled   atomic.Uint64
	abandoned atomic.Uint64
	queued    atomic.Int64
}

func (m *SupervisorMonitor) attempt() *supervisorAttemptMonitor {
	return &supervisorAttemptMonitor{monitor: m}
}

func (m *supervisorAttemptMonitor) frameRead(size int) {
	m.read.Add(1)
	m.queued.Add(1)
	if m.monitor == nil {
		return
	}

	m.monitor.mu.Lock()
	m.monitor.state.framesRead++
	m.monitor.state.bytesRead += uint64(size) //nolint:gosec // len(frame) is non-negative and cannot exceed uint64
	m.monitor.state.frameQueueDepth++
	m.monitor.state.frameQueueHighWater = max(m.monitor.state.frameQueueHighWater, m.monitor.state.frameQueueDepth)
	m.monitor.mu.Unlock()
}

func (m *supervisorAttemptMonitor) handlingStarted() {
	m.queued.Add(-1)
	if m.monitor == nil {
		return
	}

	m.monitor.mu.Lock()
	m.monitor.state.frameQueueDepth--
	m.monitor.state.handlingInFlight++
	m.monitor.mu.Unlock()
	if m.monitor.performanceStage != nil {
		m.monitor.performanceStage.AddInFlight(1)
	}
}

func (m *supervisorAttemptMonitor) handlingFinished(started time.Time, frameBytes int) {
	m.handled.Add(1)
	if m.monitor == nil {
		return
	}

	ended := time.Now()
	duration := ended.Sub(started)
	m.monitor.mu.Lock()
	m.monitor.state.handlingInFlight--
	m.monitor.state.framesHandled++
	m.monitor.state.handlingDurationCount++
	m.monitor.state.handlingDurationTotal += duration
	m.monitor.state.handlingDurationLast = duration
	m.monitor.state.handlingDurationMax = max(m.monitor.state.handlingDurationMax, duration)
	m.monitor.mu.Unlock()
	if m.monitor.performanceStage != nil {
		m.monitor.performanceStage.AddInFlight(-1)
		m.monitor.performanceStage.Record(performance.Sample{
			At:       ended,
			Duration: duration,
			Calls:    1,
			ItemsIn:  uint64(frameBytes), //nolint:gosec // len(frame) is non-negative and cannot exceed uint64
			ItemsOut: 1,
		})
	}
}

func (m *supervisorAttemptMonitor) handlingAborted() {
	m.abandoned.Add(1)
	if m.monitor != nil {
		m.monitor.mu.Lock()
		m.monitor.state.handlingInFlight--
		m.monitor.state.framesAbandoned++
		m.monitor.mu.Unlock()
		if m.monitor.performanceStage != nil {
			m.monitor.performanceStage.AddInFlight(-1)
		}
	}
}

func (m *supervisorAttemptMonitor) attemptEnded() {
	if m.monitor == nil {
		return
	}

	read := m.read.Load()
	resolved := m.handled.Load() + m.abandoned.Load()
	queued := m.queued.Load()
	m.monitor.mu.Lock()
	if read > resolved {
		m.monitor.state.framesAbandoned += read - resolved
	}
	if queued > 0 {
		m.monitor.state.frameQueueDepth -= int(queued)
	}
	m.monitor.mu.Unlock()
}
