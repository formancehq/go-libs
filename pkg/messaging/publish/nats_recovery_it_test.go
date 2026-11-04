//go:build it

package publish

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	wNats "github.com/ThreeDotsLabs/watermill-nats/v2/pkg/nats"
	"github.com/ThreeDotsLabs/watermill/message"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func TestRecoveringNATSPublisherRecoversFromSilentConnection(t *testing.T) {
	server, control := startRecoveryNATSServer(t)
	proxy := startRecoveryNATSProxy(t, server.Addr().String())
	conn, publisher := connectRecoveryNATSPublisher(t, proxy.URL()+","+server.ClientURL())
	if conn.ConnectedUrl() != proxy.URL() {
		t.Fatalf("expected initial connection through fault proxy, got %q", conn.ConnectedUrl())
	}

	proxy.blackhole.Store(true)
	msg := message.NewMessage("silent-connection-audit", []byte(`{"audit_id":"silent-connection-audit"}`))
	if err := publisher.Publish("audit", msg); err != nil {
		t.Fatalf("publish did not recover through the surviving endpoint: %v", err)
	}
	if conn.ConnectedUrl() != server.ClientURL() {
		t.Fatalf("expected connection to healthy endpoint %q, got %q", server.ClientURL(), conn.ConnectedUrl())
	}
	if conn.Stats().Reconnects == 0 {
		t.Fatal("expected a reconnect after silent packet loss")
	}
	assertNATSStreamMessages(t, control, 1)
}

func TestRecoveringNATSPublisherDeduplicatesRetryAfterLostACK(t *testing.T) {
	server, control := startRecoveryNATSServer(t)
	proxy := startRecoveryNATSProxy(t, server.Addr().String())
	conn, publisher := connectRecoveryNATSPublisher(t, proxy.URL())
	proxy.dropACK.Store(true)

	msg := message.NewMessage("lost-ack-audit", []byte(`{"audit_id":"lost-ack-audit"}`))
	result := make(chan error, 1)
	go func() { result <- publisher.Publish("audit", msg) }()
	select {
	case <-proxy.ackDropped:
		// JetStream accepted the first publication, but the publisher cannot
		// distinguish its lost ACK from a message which was never stored.
		assertNATSStreamMessages(t, control, 1)
	case <-time.After(5 * time.Second):
		t.Fatal("proxy did not observe a JetStream publish ACK")
	}

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("retry after lost ACK failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("publish did not finish after its ACK was lost")
	}
	if conn.Stats().Reconnects != 0 {
		t.Fatal("a lost publish ACK must not force a healthy connection to reconnect")
	}
	assertNATSStreamMessages(t, control, 1)
}

func startRecoveryNATSServer(t *testing.T) (*natsserver.Server, nats.JetStreamContext) {
	t.Helper()
	server, err := natsserver.NewServer(&natsserver.Options{
		Host: "127.0.0.1", Port: -1, JetStream: true,
		StoreDir: t.TempDir(), NoLog: true, NoSigs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	go server.Start()
	t.Cleanup(server.Shutdown)
	if !server.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS server did not become ready")
	}
	conn, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	js, err := conn.JetStream(nats.MaxWait(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	_, err = js.AddStream(&nats.StreamConfig{Name: "AUDIT", Subjects: []string{"audit"}})
	if err != nil {
		t.Fatal(err)
	}
	return server, js
}

func testNATSConfig() wNats.PublisherConfig {
	return wNats.PublisherConfig{Marshaler: &wNats.NATSMarshaler{}, SubjectCalculator: wNats.DefaultSubjectCalculator, JetStream: wNats.JetStreamConfig{TrackMsgId: true}}
}

func connectRecoveryNATSPublisher(t *testing.T, urls string, configure ...func(*wNats.PublisherConfig)) (*nats.Conn, *RecoveringNATSPublisher) {
	t.Helper()
	dialer, err := NewNATSBoundedDialer(net.Dialer{}, 2*time.Second, 4*time.Second, 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	config := testNATSConfig()
	config.URL = urls
	config.NatsOptions = []nats.Option{nats.SetCustomDialer(dialer), nats.MaxReconnects(30), nats.ReconnectWait(10 * time.Millisecond), nats.PingInterval(100 * time.Millisecond), nats.MaxPingsOutstanding(2), nats.DontRandomize()}
	for _, configure := range configure {
		configure(&config)
	}
	conn, err := NewNatsConn(config)
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := NewRecoveringNATSPublisherWithConn(conn, config, NATSRecoveryConfig{Budget: 16 * time.Second, AttemptTimeout: 100 * time.Millisecond, RetryDelay: 250 * time.Millisecond, MaxRetryDelay: time.Second})
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = publisher.Close() })
	return conn, publisher
}

func assertNATSStreamMessages(t *testing.T, js nats.JetStreamContext, want uint64) {
	t.Helper()
	info, err := js.StreamInfo("AUDIT", nats.Context(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs != want {
		t.Fatalf("stream contains %d audit events, want %d", info.State.Msgs, want)
	}
}

// Both endpoints reach one healthy JetStream server. This isolates client
// failover and duplicate handling from quorum and leader election.
type recoveryNATSProxy struct {
	listener    net.Listener
	blackhole   atomic.Bool
	dropACK     atomic.Bool
	ackDropped  chan struct{}
	mu          sync.Mutex
	connections []net.Conn
	stopReads   atomic.Bool
	resumeReads chan struct{}
	closed      bool
	wg          sync.WaitGroup
}

func startRecoveryNATSProxy(t *testing.T, upstream string) *recoveryNATSProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &recoveryNATSProxy{listener: listener, ackDropped: make(chan struct{}), resumeReads: make(chan struct{})}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			server, err := net.DialTimeout("tcp", upstream, time.Second)
			if err != nil {
				_ = client.Close()
				continue
			}
			p.mu.Lock()
			if p.closed {
				p.mu.Unlock()
				_ = client.Close()
				_ = server.Close()
				return
			}
			p.connections = append(p.connections, client, server)
			p.wg.Add(2)
			p.mu.Unlock()
			go func() { defer p.wg.Done(); p.copyClientPackets(server, client) }()
			go func() { defer p.wg.Done(); p.copyServerPackets(client, server) }()
		}
	}()
	t.Cleanup(func() {
		p.mu.Lock()
		p.closed = true
		close(p.resumeReads)
		_ = p.listener.Close()
		for _, conn := range p.connections {
			_ = conn.Close()
		}
		p.mu.Unlock()
		p.wg.Wait()
	})
	return p
}

func (p *recoveryNATSProxy) URL() string { return "nats://" + p.listener.Addr().String() }

func (p *recoveryNATSProxy) copyClientPackets(dst, src net.Conn) {
	buf := make([]byte, 32*1024)
	for {
		if p.stopReads.Load() {
			<-p.resumeReads
			return
		}
		n, err := src.Read(buf)
		if err != nil {
			return
		}
		if !p.blackhole.Load() {
			if _, err = dst.Write(buf[:n]); err != nil {
				return
			}
		}
	}
}

func (p *recoveryNATSProxy) copyServerPackets(dst, src net.Conn) {
	reader := bufio.NewReader(src)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		frame := []byte(line)
		fields := strings.Fields(line)
		if len(fields) > 0 && (fields[0] == "MSG" || fields[0] == "HMSG") {
			size, err := strconv.Atoi(fields[len(fields)-1])
			if err != nil || size < 0 {
				return
			}
			payload := make([]byte, size+2)
			if _, err := io.ReadFull(reader, payload); err != nil {
				return
			}
			frame = append(frame, payload...)
			if bytes.Contains(payload, []byte(`"stream":"AUDIT"`)) && p.dropACK.CompareAndSwap(true, false) {
				close(p.ackDropped)
				continue
			}
		}
		if !p.blackhole.Load() {
			if _, err := dst.Write(frame); err != nil {
				return
			}
		}
	}
}

func TestRecoveringNATSPublisherBlockedWritesRecover(t *testing.T) {
	server, control := startRecoveryNATSServer(t)
	proxy := startRecoveryNATSProxy(t, server.Addr().String())
	conn, publisher := connectRecoveryNATSPublisher(t, proxy.URL()+","+server.ClientURL(), func(a *wNats.PublisherConfig) {
		// Isolate socket-write recovery: a heartbeat cannot rescue this test.
		a.NatsOptions = append(a.NatsOptions, nats.PingInterval(time.Hour))
	})
	publisher.recovery.AttemptTimeout = 5 * time.Second
	proxy.stopReads.Store(true)
	proxy.blackhole.Store(true)
	const count = 64
	results := make(chan error, count)
	for i := range count {
		go func() {
			results <- publisher.Publish("audit", message.NewMessage(fmt.Sprintf("blocked-%d", i), bytes.Repeat([]byte("x"), 128*1024)))
		}()
	}
	deadline := time.NewTimer(18 * time.Second)
	defer deadline.Stop()
	for range count {
		select {
		case err := <-results:
			if err != nil {
				t.Errorf("blocked publication: %v", err)
			}
		case <-deadline.C:
			t.Fatal("blocked writers did not recover within event budget plus transport overhead")
		}
	}
	if conn.Stats().Reconnects == 0 {
		t.Fatal("blocked transport did not reconnect")
	}
	assertNATSStreamMessages(t, control, count)
}

func TestRecoveringNATSPublisherProductionTiming(t *testing.T) {
	server, control := startRecoveryNATSServer(t)
	proxy := startRecoveryNATSProxy(t, server.Addr().String())
	_, publisher := connectRecoveryNATSPublisher(t, proxy.URL()+","+server.ClientURL(), func(a *wNats.PublisherConfig) {
		a.NatsOptions = append(a.NatsOptions, nats.PingInterval(2*time.Second))
		a.NatsOptions = append(a.NatsOptions, nats.ReconnectWait(2*time.Second))
	})
	publisher.recovery.AttemptTimeout = 5 * time.Second
	proxy.blackhole.Store(true)
	started := time.Now()
	if err := publisher.Publish("audit", message.NewMessage("production-timing", nil)); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 18*time.Second {
		t.Fatalf("recovery took %s", elapsed)
	}
	assertNATSStreamMessages(t, control, 1)
}
