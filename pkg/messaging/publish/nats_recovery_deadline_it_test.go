//go:build it

package publish

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	wNats "github.com/ThreeDotsLabs/watermill-nats/v2/pkg/nats"
	"github.com/ThreeDotsLabs/watermill/message"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func TestRecoveringNATSPublisherDoesNotMultiplyNativeRetries(t *testing.T) {
	server, _ := startRecoveryNATSServer(t)
	var calls atomic.Int64
	installDelayedNATSResponder(t, server.ClientURL(), "delayed-audit", 80*time.Millisecond, &calls)
	publisher := connectDeadlineNATSPublisher(t, server.ClientURL(), false)
	publisher.recovery.AttemptTimeout = 150 * time.Millisecond
	publisher.recovery.Budget = time.Second
	publisher.recovery.RetryDelay = time.Millisecond

	err := publisher.Publish("delayed-audit", message.NewMessage("delayed-503", nil))
	if !errors.Is(err, nats.ErrNoStreamResponse) {
		t.Fatalf("got %v, want no stream response", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("got %d native sends, want exactly three wrapper attempts", got)
	}
}

func TestRecoveringNATSPublisherSharesDeadlineAcrossAttempts(t *testing.T) {
	server, _ := startRecoveryNATSServer(t)
	var calls atomic.Int64
	installDelayedNATSResponder(t, server.ClientURL(), "deadline-audit", 80*time.Millisecond, &calls)
	publisher := connectDeadlineNATSPublisher(t, server.ClientURL(), false)
	publisher.recovery.MaxAttempts = 100 // The elapsed budget must stop us before this count.
	publisher.recovery.AttemptTimeout = 150 * time.Millisecond
	publisher.recovery.Budget = 250 * time.Millisecond
	publisher.recovery.RetryDelay = time.Millisecond

	started := time.Now()
	err := publisher.Publish("deadline-audit", message.NewMessage("deadline-503", nil))
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want shared deadline exceeded", err)
	}
	if elapsed < 200*time.Millisecond || elapsed > 650*time.Millisecond {
		t.Fatalf("publication took %s with a 250ms elapsed budget", elapsed)
	}
	if got := calls.Load(); got > 4 {
		t.Fatalf("got %d native sends; elapsed deadline did not stop retries", got)
	}
}

func TestRecoveringNATSPublisherBoundsAutoProvisionRequests(t *testing.T) {
	// A core-only server and a silent API subscriber model unavailable
	// JetStream management without returning an immediate no-responders error.
	server, err := natsserver.NewServer(&natsserver.Options{
		Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	go server.Start()
	t.Cleanup(server.Shutdown)
	if !server.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS server did not become ready")
	}
	responder, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(responder.Close)
	var calls atomic.Int64
	var creates atomic.Int64
	if _, err := responder.Subscribe("$JS.API.>", func(msg *nats.Msg) {
		calls.Add(1)
		if strings.Contains(msg.Subject, ".STREAM.CREATE.") {
			creates.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := responder.Flush(); err != nil {
		t.Fatal(err)
	}
	publisher := connectDeadlineNATSPublisher(t, server.ClientURL(), true)
	publisher.recovery.Budget = 250 * time.Millisecond
	publisher.recovery.AttemptTimeout = 100 * time.Millisecond
	publisher.recovery.RetryDelay = time.Millisecond
	publisher.recovery.MaxAttempts = 100

	started := time.Now()
	err = publisher.Publish("audit", message.NewMessage("autoprovision-deadline", nil))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 650*time.Millisecond {
		t.Fatalf("management requests exceeded elapsed budget: %s", elapsed)
	}
	if creates.Load() != 0 {
		t.Fatal("timeout must not trigger stream creation")
	}
	if calls.Load() == 0 {
		t.Fatal("test did not exercise JetStream management requests")
	}
}

func installDelayedNATSResponder(t *testing.T, url, subject string, delay time.Duration, calls *atomic.Int64) {
	t.Helper()
	conn, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	if _, err := conn.Subscribe(subject, func(msg *nats.Msg) {
		calls.Add(1)
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-t.Context().Done():
			return
		case <-timer.C:
		}
		reply := nats.NewMsg(msg.Reply)
		reply.Header.Set("Status", "503")
		_ = conn.PublishMsg(reply)
	}); err != nil {
		t.Fatal(err)
	}
	if err := conn.Flush(); err != nil {
		t.Fatal(err)
	}
}

func connectDeadlineNATSPublisher(t *testing.T, url string, autoProvision bool) *RecoveringNATSPublisher {
	t.Helper()
	_, publisher := connectRecoveryNATSPublisher(t, url, func(config *wNats.PublisherConfig) { config.JetStream.AutoProvision = autoProvision })
	publisher.recovery.MaxAttempts = 3
	return publisher
}
