//go:build it

package publish

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	wNats "github.com/ThreeDotsLabs/watermill-nats/v2/pkg/nats"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/nats-io/nats.go"
)

type countingNATSMarshaler struct{ calls atomic.Int64 }

func (m *countingNATSMarshaler) Marshal(topic string, msg *message.Message) (*nats.Msg, error) {
	m.calls.Add(1)
	return (&wNats.NATSMarshaler{}).Marshal(topic, msg)
}

func TestRecoveringNATSPublisherIdentityBatchAnd10158(t *testing.T) {
	server, _ := startRecoveryNATSServer(t)
	responder, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer responder.Close()
	var mu sync.Mutex
	counts := map[string]int{}
	var original []byte
	var invalid atomic.Bool
	_, err = responder.Subscribe("sequence", func(msg *nats.Msg) {
		id := msg.Header.Get(nats.MsgIdHdr)
		mu.Lock()
		counts[id]++
		call := counts[id]
		if id == "second" {
			if original == nil {
				original = bytes.Clone(msg.Data)
			} else if !bytes.Equal(original, msg.Data) {
				invalid.Store(true)
			}
		}
		mu.Unlock()
		if id == "second" && call == 1 {
			return
		} // Ambiguous ACK timeout.
		if id == "second" && call == 2 {
			_ = msg.Respond([]byte(`{"error":{"code":409,"err_code":10158,"description":"in process"}}`))
			return
		}
		_ = msg.Respond([]byte(`{"stream":"sequence","seq":1}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = responder.Flush(); err != nil {
		t.Fatal(err)
	}
	marshaler := &countingNATSMarshaler{}
	_, publisher := connectRecoveryNATSPublisher(t, server.ClientURL(), func(c *wNats.PublisherConfig) { c.Marshaler = marshaler })
	publisher.recovery.RetryDelay = time.Millisecond
	publisher.recovery.MaxAttempts = 3
	if err = publisher.Publish("sequence", message.NewMessage("first", []byte("one")), message.NewMessage("second", []byte("two"))); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if counts["first"] != 1 || counts["second"] != 3 || marshaler.calls.Load() != 2 || invalid.Load() {
		t.Fatalf("identity/batch: counts=%v marshals=%d changed=%v", counts, marshaler.calls.Load(), invalid.Load())
	}
}

func TestRecoveringNATSPublisherCancellationAndClose(t *testing.T) {
	for _, closePublisher := range []bool{false, true} {
		t.Run(fmt.Sprint(closePublisher), func(t *testing.T) {
			server, _ := startRecoveryNATSServer(t)
			var calls atomic.Int64
			installDelayedNATSResponder(t, server.ClientURL(), "cancel", time.Millisecond, &calls)
			_, publisher := connectRecoveryNATSPublisher(t, server.ClientURL())
			publisher.recovery.RetryDelay = time.Hour
			publisher.recovery.MaxRetryDelay = time.Hour
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			msg := message.NewMessage("cancel", nil)
			msg.SetContext(ctx)
			done := make(chan error, 1)
			go func() { done <- publisher.Publish("cancel", msg) }()
			// Wait for the real server to receive a request before interrupting.
			deadline := time.NewTimer(time.Second)
			defer deadline.Stop()
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			for calls.Load() == 0 {
				select {
				case <-deadline.C:
					t.Fatal("no publication")
				case <-ticker.C:
				}
			}
			if closePublisher {
				_ = publisher.Close()
				_ = publisher.Close()
			} else {
				cancel()
			}
			select {
			case err := <-done:
				want := context.Canceled
				if closePublisher {
					want = nats.ErrConnectionClosed
				}
				if !errors.Is(err, want) {
					t.Fatalf("got %v want %v", err, want)
				}
			case <-time.After(time.Second):
				t.Fatal("cancellation did not interrupt publication")
			}
			if closePublisher && !publisher.conn.IsClosed() {
				t.Fatal("owned connection remained open")
			}
		})
	}
}

func TestRecoveringNATSPublisherValidationAndLegacyCompatibility(t *testing.T) {
	server, control := startRecoveryNATSServer(t)
	conn, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cfg := testNATSConfig()
	recovery := NATSRecoveryConfig{Budget: time.Second, AttemptTimeout: time.Second, RetryDelay: time.Millisecond, MaxRetryDelay: time.Millisecond}
	for _, mutate := range []func(*wNats.PublisherConfig){func(c *wNats.PublisherConfig) { c.JetStream.TrackMsgId = false }, func(c *wNats.PublisherConfig) { c.JetStream.Disabled = true }, func(c *wNats.PublisherConfig) { c.JetStream.AckAsync = true }} {
		invalid := cfg
		mutate(&invalid)
		if _, err = NewRecoveringNATSPublisherWithConn(conn, invalid, recovery); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	if _, err = NewRecoveringNATSPublisherWithConn(conn, cfg, NATSRecoveryConfig{}); err == nil {
		t.Fatal("unbounded recovery accepted")
	}
	legacy, err := NewNatsPublisherWithConn(conn, watermill.NopLogger{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	msg := message.NewMessage("legacy", nil)
	msg.SetContext(ctx)
	// The old constructor retains Watermill's context behavior and no retry policy.
	if err = legacy.Publish("audit", msg); err != nil {
		t.Fatal(err)
	}
	assertNATSStreamMessages(t, control, 1)
	p, err := NewRecoveringNATSPublisherWithConn(conn, cfg, recovery)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	if err = p.Publish("audit", msg); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled publication: %v", err)
	}
	if err = p.Publish("audit", message.NewMessage("", nil)); err == nil {
		t.Fatal("empty UUID accepted")
	}
	assertNATSStreamMessages(t, control, 1)
}

func TestRecoveringNATSPublisherFastFailuresUseBudget(t *testing.T) {
	server, _ := startRecoveryNATSServer(t)
	conn, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	started := time.Now()
	var calls atomic.Int64
	_, err = conn.Subscribe("fast", func(msg *nats.Msg) {
		calls.Add(1)
		if time.Since(started) < 250*time.Millisecond {
			_ = msg.Respond([]byte(`{"error":{"code":409,"err_code":10158}}`))
			return
		}
		_ = msg.Respond([]byte(`{"stream":"fast","seq":1}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = conn.Flush(); err != nil {
		t.Fatal(err)
	}
	_, p := connectRecoveryNATSPublisher(t, server.ClientURL())
	p.recovery.RetryDelay = 20 * time.Millisecond
	p.recovery.MaxRetryDelay = 50 * time.Millisecond
	if err = p.Publish("fast", message.NewMessage("fast", nil)); err != nil {
		t.Fatal(err)
	}
	if calls.Load() <= 3 {
		t.Fatalf("test did not exercise >3 fast failures: %d", calls.Load())
	}
}

func TestRecoveringNATSPublisherAutoProvision(t *testing.T) {
	server, _ := startRecoveryNATSServer(t)
	_, p := connectRecoveryNATSPublisher(t, server.ClientURL(), func(c *wNats.PublisherConfig) { c.JetStream.AutoProvision = true })
	if err := p.Publish("created", message.NewMessage("created", []byte("payload"))); err != nil {
		t.Fatal(err)
	}
	info, err := p.js.StreamInfo("created", nats.Context(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs != 1 || len(info.Config.Subjects) != 1 || info.Config.Subjects[0] != "created" {
		t.Fatalf("unexpected provisioned stream: %+v", info)
	}
}

func TestRecoveringNATSPublisherPermanentAPIErrors(t *testing.T) {
	for _, body := range []string{`{"error":{"code":409,"err_code":10071}}`, `{"error":{"code":503,"err_code":10047}}`} {
		t.Run(body, func(t *testing.T) {
			server, _ := startRecoveryNATSServer(t)
			responder, err := nats.Connect(server.ClientURL())
			if err != nil {
				t.Fatal(err)
			}
			defer responder.Close()
			var calls atomic.Int64
			_, err = responder.Subscribe("permanent", func(msg *nats.Msg) { calls.Add(1); _ = msg.Respond([]byte(body)) })
			if err != nil {
				t.Fatal(err)
			}
			if err = responder.Flush(); err != nil {
				t.Fatal(err)
			}
			_, p := connectRecoveryNATSPublisher(t, server.ClientURL())
			p.recovery.Budget = 500 * time.Millisecond
			p.recovery.RetryDelay = time.Millisecond
			var api *nats.APIError
			if err = p.Publish("permanent", message.NewMessage("permanent", nil)); !errors.As(err, &api) {
				t.Fatalf("got %v, want API error", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("permanent error retried %d times", calls.Load())
			}
		})
	}
}

func TestRecoveringNATSPublisherExplicitDetachedContextPolicy(t *testing.T) {
	server, control := startRecoveryNATSServer(t)
	_, p := connectRecoveryNATSPublisher(t, server.ClientURL())
	p.recovery.MessageContext = context.WithoutCancel
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	msg := message.NewMessage("detached", nil)
	msg.SetContext(ctx)
	if err := p.Publish("audit", msg); err != nil {
		t.Fatalf("explicit detached audit policy: %v", err)
	}
	assertNATSStreamMessages(t, control, 1)
	// Detachment must not detach the publisher's own foreground budget.
	var calls atomic.Int64
	installDelayedNATSResponder(t, server.ClientURL(), "detached-budget", 80*time.Millisecond, &calls)
	p.recovery.Budget = 100 * time.Millisecond
	p.recovery.RetryDelay = time.Millisecond
	if err := p.Publish("detached-budget", msg); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("detached budget: %v", err)
	}
}

func TestRecoveringNATSPublisherTransientAPIErrors(t *testing.T) {
	for _, code := range []int{10008, 10163} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			server, _ := startRecoveryNATSServer(t)
			responder, err := nats.Connect(server.ClientURL())
			if err != nil {
				t.Fatal(err)
			}
			defer responder.Close()
			var calls atomic.Int64
			_, err = responder.Subscribe("transient", func(msg *nats.Msg) {
				if calls.Add(1) == 1 {
					_ = msg.Respond(fmt.Appendf(nil, `{"error":{"code":503,"err_code":%d}}`, code))
				} else {
					_ = msg.Respond([]byte(`{"stream":"transient","seq":1}`))
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if err = responder.Flush(); err != nil {
				t.Fatal(err)
			}
			_, p := connectRecoveryNATSPublisher(t, server.ClientURL())
			p.recovery.RetryDelay = time.Millisecond
			if err = p.Publish("transient", message.NewMessage("transient-id", nil)); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 2 {
				t.Fatalf("got %d calls, want two", calls.Load())
			}
		})
	}
}
