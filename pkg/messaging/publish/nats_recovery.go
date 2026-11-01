package publish

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"time"

	wNats "github.com/ThreeDotsLabs/watermill-nats/v2/pkg/nats"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/nats-io/nats.go"
)

// NATSRecoveryConfig is an explicit, per-message foreground retry policy.
// MaxAttempts includes the first send; zero means retry until Budget expires.
// No defaults are installed on existing Watermill publishers.
type NATSRecoveryConfig struct {
	Budget         time.Duration
	AttemptTimeout time.Duration
	RetryDelay     time.Duration
	MaxRetryDelay  time.Duration
	MaxAttempts    int
	// MessageContext optionally transforms the message context before applying
	// Budget. For detached synchronous audit, use context.WithoutCancel.
	// Nil preserves message cancellation. The function must be concurrency-safe.
	MessageContext func(context.Context) context.Context
}

// RecoveringNATSPublisher publishes synchronously with Watermill encoding and
// native JetStream acknowledgements. It owns the supplied connection, like
// Watermill's Publisher. Do not share that connection with another owner.
type RecoveringNATSPublisher struct {
	conn      *nats.Conn
	js        nats.JetStreamContext
	config    wNats.PublisherConfig
	recovery  NATSRecoveryConfig
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
}

// NewRecoveringNATSPublisherWithConn opts into contextual JetStream publishing.
// TrackMsgId is required: ambiguous acknowledgement failures can cause retries.
// Marshaler implementations must be safe for concurrent use, as with Watermill.
func NewRecoveringNATSPublisherWithConn(conn *nats.Conn, config wNats.PublisherConfig, recovery NATSRecoveryConfig) (*RecoveringNATSPublisher, error) {
	if conn == nil {
		return nil, fmt.Errorf("NATS connection is required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.JetStream.Disabled || config.JetStream.AckAsync || !config.JetStream.TrackMsgId {
		return nil, fmt.Errorf("NATS recovery requires synchronous JetStream and TrackMsgId")
	}
	if recovery.Budget <= 0 || recovery.AttemptTimeout <= 0 || recovery.RetryDelay <= 0 || recovery.MaxRetryDelay < recovery.RetryDelay || recovery.MaxAttempts < 0 {
		return nil, fmt.Errorf("NATS recovery requires positive budget, attempt timeout and retry delay, max retry delay >= retry delay, and nonnegative attempts")
	}
	// Snapshot option slices: callers may reuse or append to their configuration.
	config.JetStream.ConnectOptions = slices.Clone(config.JetStream.ConnectOptions)
	config.JetStream.PublishOptions = slices.Clone(config.JetStream.PublishOptions)
	js, err := conn.JetStream(config.JetStream.ConnectOptions...)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &RecoveringNATSPublisher{conn: conn, js: js, config: config, recovery: recovery, ctx: ctx, cancel: cancel}, nil
}

// Publish uses each message's context. Detaching HTTP cancellation is caller
// policy: configure MessageContext with context.WithoutCancel if required.
// Earlier acknowledged batch items are never replayed by a later item's retry.
func (p *RecoveringNATSPublisher) Publish(topic string, messages ...*message.Message) error {
	for _, msg := range messages {
		if msg == nil || msg.UUID == "" {
			return fmt.Errorf("NATS recovery requires a nonempty message UUID")
		}
		if err := p.publish(topic, msg); err != nil {
			return err
		}
	}
	return nil
}

func (p *RecoveringNATSPublisher) publish(topic string, msg *message.Message) error {
	parent := msg.Context()
	if p.recovery.MessageContext != nil {
		parent = p.recovery.MessageContext(parent)
	}
	if parent == nil {
		return fmt.Errorf("NATS message context policy returned nil")
	}
	ctx, cancel := context.WithTimeout(parent, p.recovery.Budget)
	defer cancel()
	stop := context.AfterFunc(p.ctx, cancel)
	defer stop()
	id := msg.UUID
	// Marshal once, so even a stateful marshaler cannot change retry identity/body.
	if err := p.checkContext(ctx); err != nil {
		return err
	}
	wire, err := p.config.Marshaler.Marshal(topic, msg)
	if err != nil {
		return err
	}
	if wire == nil {
		return fmt.Errorf("NATS marshaler returned a nil message")
	}
	wire = cloneNATSPublishMessage(wire)
	delay := p.recovery.RetryDelay
	for attempt := 1; ; attempt++ {
		if err := p.checkContext(ctx); err != nil {
			return err
		}
		attemptCtx, cancelAttempt := context.WithTimeout(ctx, p.recovery.AttemptTimeout)
		err := p.attempt(attemptCtx, topic, id, wire)
		cancelAttempt()
		if err == nil {
			return nil
		}
		if contextErr := p.checkContext(ctx); contextErr != nil {
			return contextErr
		}
		if (p.recovery.MaxAttempts > 0 && attempt >= p.recovery.MaxAttempts) || !IsRetryableNATSPublishError(err) {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return p.checkContext(ctx)
		case <-p.ctx.Done():
			timer.Stop()
			return nats.ErrConnectionClosed
		case <-timer.C:
		}
		// Saturate without duration overflow.
		delay = min(delay, p.recovery.MaxRetryDelay-delay) + delay
	}
}

func (p *RecoveringNATSPublisher) checkContext(ctx context.Context) error {
	if p.ctx.Err() != nil {
		return nats.ErrConnectionClosed
	}
	return ctx.Err()
}

func (p *RecoveringNATSPublisher) attempt(ctx context.Context, topic, id string, wire *nats.Msg) error {
	if p.config.JetStream.AutoProvision {
		_, err := p.js.StreamInfo(topic, nats.Context(ctx))
		// Unlike Watermill 2.1.3, never create a stream after timeout/permission errors.
		if errors.Is(err, nats.ErrStreamNotFound) {
			_, err = p.js.AddStream(&nats.StreamConfig{Name: topic, Subjects: p.config.SubjectCalculator("", topic).All()}, nats.Context(ctx))
		}
		if err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	opts := append(slices.Clone(p.config.JetStream.PublishOptions), nats.Context(ctx), nats.RetryAttempts(0), nats.MsgId(id))
	_, err := p.js.PublishMsg(cloneNATSPublishMessage(wire), opts...)
	return err
}

// IsRetryableNATSPublishError classifies transport and known transient JetStream
// failures. A generic API 503 or 409 is not sufficient evidence for a retry.
func IsRetryableNATSPublishError(err error) bool {
	if errors.Is(err, nats.ErrConnectionClosed) || errors.Is(err, context.Canceled) {
		return false
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "write" {
		return true
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return true
	}
	if errors.Is(err, net.ErrClosed) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, nats.ErrTimeout) || errors.Is(err, nats.ErrDisconnected) || errors.Is(err, nats.ErrNoResponders) || errors.Is(err, nats.ErrNoStreamResponse) || errors.Is(err, nats.ErrReconnectBufExceeded) {
		return true
	}
	var api *nats.APIError
	if !errors.As(err, &api) {
		return false
	}
	// nats-server server/errors.json: system temporarily unavailable,
	// duplicate ID in process, and expected subject sequence temporarily unavailable.
	switch api.ErrorCode {
	case 10008, 10158, 10163:
		return true
	default:
		return false
	}
}

// Close interrupts backoff and pending requests and closes the owned connection.
// Native connection locks can delay Close until transport writes complete.
func (p *RecoveringNATSPublisher) Close() error {
	p.closeOnce.Do(func() { p.cancel(); p.conn.Close() })
	return nil
}

var _ message.Publisher = (*RecoveringNATSPublisher)(nil)

// Keep the wire snapshot private; nats.go sets request and dedup headers.
func cloneNATSPublishMessage(msg *nats.Msg) *nats.Msg {
	result := &nats.Msg{Subject: msg.Subject, Reply: msg.Reply, Data: slices.Clone(msg.Data), Header: make(nats.Header, len(msg.Header))}
	for key, values := range msg.Header {
		result.Header[key] = slices.Clone(values)
	}
	return result
}
