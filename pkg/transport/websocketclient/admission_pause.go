package websocketclient

import (
	"sync"
	"time"
)

// admissionPause coordinates heartbeat ownership with the sole reader's bounded
// admission waits. An interrupted ping is not evidence of a failed peer.
type admissionPause struct {
	mu      sync.Mutex
	since   time.Time
	total   time.Duration
	changed chan struct{}
}

func firstPause(states []*admissionPause) *admissionPause {
	if len(states) > 0 && states[0] != nil {
		return states[0]
	}
	return &admissionPause{}
}
func (p *admissionPause) signalLocked() {
	if p.changed != nil {
		select {
		case p.changed <- struct{}{}:
		default:
		}
	}
}
func (p *admissionPause) start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.since = time.Now()
	p.signalLocked()
}
func (p *admissionPause) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.since.IsZero() {
		p.total += time.Since(p.since)
		p.since = time.Time{}
		p.signalLocked()
	}
}
func (p *admissionPause) elapsed() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.since.IsZero() {
		return p.total + time.Since(p.since)
	}
	return p.total
}
func (p *admissionPause) state() (bool, <-chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.changed == nil {
		p.changed = make(chan struct{}, 1)
	}
	return !p.since.IsZero(), p.changed
}
