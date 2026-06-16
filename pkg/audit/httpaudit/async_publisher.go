package httpaudit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/formancehq/go-libs/v5/pkg/audit"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

const (
	defaultAsyncPublishingQueueCapacity = 1024
	defaultAsyncPublishingWorkerCount   = 1
)

var ErrAsyncPublisherClosed = errors.New("async audit publisher closed")

type auditEventPublisher interface {
	Publish(ctx context.Context, payload audit.Payload)
}

type syncAuditEventPublisher struct {
	publisher message.Publisher
	topicName string
	appName   string
}

func (p syncAuditEventPublisher) Publish(ctx context.Context, payload audit.Payload) {
	audit.PublishEvent(ctx, p.publisher, p.topicName, p.appName, payload)
}

type asyncPublishingConfig struct {
	queueCapacity int
	workerCount   int
	onDrop        func(context.Context, audit.Payload)
	onError       func(context.Context, audit.Payload, error)
}

func defaultAsyncPublishingConfig() asyncPublishingConfig {
	return asyncPublishingConfig{
		queueCapacity: defaultAsyncPublishingQueueCapacity,
		workerCount:   defaultAsyncPublishingWorkerCount,
	}
}

// AsyncPublishingOption configures HTTP audit async publishing.
type AsyncPublishingOption func(*asyncPublishingConfig)

// WithAsyncPublishingQueueCapacity sets the fixed queue capacity used by async audit publishing.
func WithAsyncPublishingQueueCapacity(capacity int) AsyncPublishingOption {
	return func(c *asyncPublishingConfig) {
		c.queueCapacity = capacity
	}
}

// WithAsyncPublishingWorkerCount sets the fixed number of workers used by async audit publishing.
func WithAsyncPublishingWorkerCount(workerCount int) AsyncPublishingOption {
	return func(c *asyncPublishingConfig) {
		c.workerCount = workerCount
	}
}

// WithAsyncPublishingDropCallback is called after an audit event is dropped because the queue is full.
func WithAsyncPublishingDropCallback(fn func(context.Context, audit.Payload)) AsyncPublishingOption {
	return func(c *asyncPublishingConfig) {
		c.onDrop = fn
	}
}

// WithAsyncPublishingErrorCallback is called after a worker fails to publish an audit event.
func WithAsyncPublishingErrorCallback(fn func(context.Context, audit.Payload, error)) AsyncPublishingOption {
	return func(c *asyncPublishingConfig) {
		c.onError = fn
	}
}

func newAsyncPublishingConfig(opts ...AsyncPublishingOption) asyncPublishingConfig {
	cfg := defaultAsyncPublishingConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.queueCapacity < 1 {
		cfg.queueCapacity = defaultAsyncPublishingQueueCapacity
	}
	if cfg.workerCount < 1 {
		cfg.workerCount = defaultAsyncPublishingWorkerCount
	}
	return cfg
}

type asyncAuditEvent struct {
	ctx     context.Context
	payload audit.Payload
	message *message.Message
}

// AsyncPublisherStats reports async audit publishing counters.
type AsyncPublisherStats struct {
	Enqueued      uint64
	Dropped       uint64
	Published     uint64
	PublishErrors uint64
}

// AsyncPublisher publishes audit events through a bounded worker pool.
type AsyncPublisher struct {
	publisher message.Publisher
	topicName string
	appName   string
	cfg       asyncPublishingConfig

	queue chan asyncAuditEvent

	mu     sync.RWMutex
	closed bool
	wg     sync.WaitGroup

	enqueued      atomic.Uint64
	dropped       atomic.Uint64
	published     atomic.Uint64
	publishErrors atomic.Uint64
}

// NewAsyncPublisher creates a bounded async publisher for HTTP audit events.
func NewAsyncPublisher(publisher message.Publisher, topicName string, appName string, opts ...AsyncPublishingOption) *AsyncPublisher {
	cfg := newAsyncPublishingConfig(opts...)
	p := &AsyncPublisher{
		publisher: publisher,
		topicName: topicName,
		appName:   appName,
		cfg:       cfg,
		queue:     make(chan asyncAuditEvent, cfg.queueCapacity),
	}

	p.wg.Add(cfg.workerCount)
	for range cfg.workerCount {
		go p.worker()
	}

	return p
}

// Publish enqueues an audit event without waiting for the underlying publisher.
// If the bounded queue is full, the event is dropped and counted.
func (p *AsyncPublisher) Publish(ctx context.Context, payload audit.Payload) {
	detachedCtx := context.WithoutCancel(ctx)
	detachedCtx = logging.ContextWithLogger(detachedCtx, logging.FromContext(ctx))
	msg, err := audit.NewEventMessageWithError(detachedCtx, p.appName, payload)
	if err != nil {
		p.publishErrors.Add(1)
		logger := logging.FromContext(ctx)
		logger.WithField("audit_payload_id", payload.ID).Errorf("failed to build audit message asynchronously: %v", err)
		if p.cfg.onError != nil {
			p.cfg.onError(detachedCtx, payload, err)
		}
		return
	}

	p.mu.RLock()
	if p.closed {
		onDrop := p.cfg.onDrop
		p.mu.RUnlock()

		p.dropped.Add(1)
		logger := logging.FromContext(ctx)
		logger.WithField("audit_payload_id", payload.ID).Errorf("failed to enqueue audit message: %v", ErrAsyncPublisherClosed)
		if onDrop != nil {
			onDrop(ctx, payload)
		}
		return
	}

	select {
	case p.queue <- asyncAuditEvent{
		ctx:     detachedCtx,
		payload: payload,
		message: msg,
	}:
		p.mu.RUnlock()
		p.enqueued.Add(1)
	default:
		onDrop := p.cfg.onDrop
		p.mu.RUnlock()

		p.dropped.Add(1)
		logger := logging.FromContext(ctx)
		logger.WithField("audit_payload_id", payload.ID).Errorf("audit publish queue full, dropping audit message")
		if onDrop != nil {
			onDrop(ctx, payload)
		}
	}
}

// Close stops accepting new events and waits for workers to drain queued events
// until ctx is canceled.
func (p *AsyncPublisher) Close(ctx context.Context) error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.queue)
	}
	p.mu.Unlock()

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stats returns a snapshot of async audit publishing counters.
func (p *AsyncPublisher) Stats() AsyncPublisherStats {
	return AsyncPublisherStats{
		Enqueued:      p.enqueued.Load(),
		Dropped:       p.dropped.Load(),
		Published:     p.published.Load(),
		PublishErrors: p.publishErrors.Load(),
	}
}

func (p *AsyncPublisher) worker() {
	defer p.wg.Done()

	for event := range p.queue {
		if err := p.publisher.Publish(p.topicName, event.message); err != nil {
			p.publishErrors.Add(1)
			logger := logging.FromContext(event.ctx)
			logger.WithField("audit_payload_id", event.payload.ID).Errorf("failed to publish audit message asynchronously: %v", err)
			if p.cfg.onError != nil {
				p.cfg.onError(event.ctx, event.payload, err)
			}
			continue
		}
		p.published.Add(1)
	}
}
