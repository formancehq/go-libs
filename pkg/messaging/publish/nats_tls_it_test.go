//go:build it

package publish

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func TestRecoveringNATSPublisherNativeVerifiedTLS(t *testing.T) {
	certServer := httptest.NewTLSServer(nil)
	t.Cleanup(certServer.Close)
	server, err := natsserver.NewServer(&natsserver.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true, TLSConfig: certServer.TLS.Clone()})
	if err != nil {
		t.Fatal(err)
	}
	go server.Start()
	t.Cleanup(server.Shutdown)
	if !server.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS not ready")
	}
	roots := x509.NewCertPool()
	roots.AddCert(certServer.Certificate())
	dialer, err := NewNATSBoundedDialer(net.Dialer{}, time.Second, 4*time.Second, 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	config := testNATSConfig()
	config.URL = server.ClientURL()
	config.JetStream.AutoProvision = true
	config.NatsOptions = []nats.Option{nats.SetCustomDialer(dialer), nats.Secure(&tls.Config{RootCAs: roots, ServerName: "example.com", MinVersion: tls.VersionTLS12})}
	conn, err := NewNatsConn(config)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewRecoveringNATSPublisherWithConn(conn, config, NATSRecoveryConfig{Budget: time.Second, AttemptTimeout: time.Second, RetryDelay: time.Millisecond, MaxRetryDelay: time.Millisecond})
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	if err = p.Publish("tls", message.NewMessage("tls", nil)); err != nil {
		t.Fatal(err)
	}
	state, err := conn.TLSConnectionState()
	if err != nil || len(state.VerifiedChains) == 0 {
		t.Fatalf("unverified native TLS: %v", err)
	}
	// Negative control: the wrapper must not bypass hostname verification.
	if bad, err := nats.Connect(server.ClientURL(), nats.SetCustomDialer(dialer), nats.Secure(&tls.Config{RootCAs: roots, ServerName: "invalid.example", MinVersion: tls.VersionTLS12})); err == nil {
		bad.Close()
		t.Fatal("invalid hostname accepted")
	}
}
