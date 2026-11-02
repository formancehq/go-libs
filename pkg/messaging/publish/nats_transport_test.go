package publish

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestRecoveryNATSConnBlockedWriteClosesSocket(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	conn := testBoundedNATSConn(client, 50*time.Millisecond)
	t.Cleanup(func() { _ = conn.Close() })

	readResult := make(chan error, 1)
	go func() {
		_, err := conn.Read(make([]byte, 1))
		readResult <- err
	}()
	started := time.Now()
	_, err := conn.Write([]byte("blocked"))
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("expected write timeout, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("write remained blocked for %s", elapsed)
	}
	select {
	case err := <-readResult:
		if err == nil {
			t.Fatal("read succeeded on failed socket")
		}
	case <-time.After(time.Second):
		t.Fatal("write failure did not wake the reader")
	}
	if _, err := conn.Write([]byte("queued")); err == nil {
		t.Fatal("queued write succeeded on failed socket")
	}
}

func TestRecoveryNATSConnPreservesShorterDeadline(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	conn := testBoundedNATSConn(client, time.Second)
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(30 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := conn.Write([]byte("blocked")); err == nil {
		t.Fatal("expected deadline failure")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("caller deadline was extended: %s", elapsed)
	}
}

func TestRecoveryNATSConnHealthySlowWrites(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	conn := testBoundedNATSConn(client, 250*time.Millisecond)
	t.Cleanup(func() { _ = conn.Close() })
	for range 3 {
		readResult := make(chan error, 1)
		go func() {
			time.Sleep(100 * time.Millisecond)
			_, err := io.ReadFull(server, make([]byte, 4))
			readResult <- err
		}()
		if _, err := conn.Write([]byte("slow")); err != nil {
			t.Fatalf("healthy slow write failed: %v", err)
		}
		if err := <-readResult; err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecoveryNATSConnDeadlineUpdatesDoNotExtendBlockedWrite(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	conn := testBoundedNATSConn(client, 50*time.Millisecond)
	t.Cleanup(func() { _ = conn.Close() })
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_ = conn.SetWriteDeadline(time.Time{})
			}
		}
	})
	started := time.Now()
	_, err := conn.Write([]byte("blocked"))
	close(stop)
	wg.Wait()
	if err == nil {
		t.Fatal("expected write timeout")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("deadline updates extended write to %s", elapsed)
	}
}

func TestRecoveryNATSConnTLS(t *testing.T) {
	// Use the standard library's test certificate; NATS itself owns the TLS
	// layer and performs certificate verification above the raw wrapper.
	certServer := httptest.NewTLSServer(nil)
	t.Cleanup(certServer.Close)
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	conn := testBoundedNATSConn(client, time.Second)
	t.Cleanup(func() { _ = conn.Close() })
	tlsServer := tls.Server(server, certServer.TLS)
	roots := x509.NewCertPool()
	roots.AddCert(certServer.Certificate())
	tlsClient := tls.Client(conn, &tls.Config{RootCAs: roots, ServerName: "example.com"})
	serverResult := make(chan error, 1)
	go func() {
		if err := tlsServer.HandshakeContext(t.Context()); err != nil {
			serverResult <- err
			return
		}
		_, err := io.ReadFull(tlsServer, make([]byte, 4))
		serverResult <- err
	}()
	if err := tlsClient.HandshakeContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := tlsClient.Write([]byte("test")); err != nil {
		t.Fatal(err)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

// Signal entry into the underlying write, rather than merely starting a
// goroutine. The pipe has no reader, so this write cannot finish before Close.
type writeStartedNATSConn struct {
	net.Conn
	started chan struct{}
	once    sync.Once
}

func (c *writeStartedNATSConn) Write(data []byte) (int, error) {
	c.once.Do(func() { close(c.started) })
	return c.Conn.Write(data)
}

func TestRecoveryNATSConnCloseInterruptsBlockedWrite(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	raw := &writeStartedNATSConn{Conn: client, started: make(chan struct{})}
	conn := testBoundedNATSConn(raw, time.Minute)
	t.Cleanup(func() { _ = conn.Close() })
	result := make(chan error, 1)
	go func() {
		_, err := conn.Write([]byte("blocked"))
		result <- err
	}()
	select {
	case <-raw.started:
	case <-time.After(time.Second):
		t.Fatal("writer did not enter the underlying connection")
	}
	select {
	case err := <-result:
		t.Fatalf("write finished before close: %v", err)
	default:
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("write succeeded after close")
		}
	case <-time.After(time.Second):
		t.Fatal("close did not interrupt writer")
	}
}

func testBoundedNATSConn(conn net.Conn, timeout time.Duration) *boundedNATSConn {
	return &boundedNATSConn{Conn: conn, writeTimeout: timeout, stallTimeout: timeout}
}

func TestBoundedNATSConnProgressingLargeWrite(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	conn := &boundedNATSConn{Conn: client, writeTimeout: 3 * time.Second, stallTimeout: 100 * time.Millisecond}
	t.Cleanup(func() { _ = conn.Close() })
	done := make(chan error, 1)
	go func() {
		buffer := make([]byte, 16*1024)
		for range 64 {
			time.Sleep(20 * time.Millisecond)
			if _, err := io.ReadFull(server, buffer); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	started := time.Now()
	if n, err := conn.Write(make([]byte, 1024*1024)); err != nil || n != 1024*1024 {
		t.Fatalf("progressing write: %d %v", n, err)
	}
	if time.Since(started) < 500*time.Millisecond {
		t.Fatal("test did not exercise a long progressing write")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestBoundedNATSConnWholeWriteBound(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	conn := &boundedNATSConn{Conn: client, writeTimeout: 150 * time.Millisecond, stallTimeout: 100 * time.Millisecond}
	t.Cleanup(func() { _ = conn.Close() })
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 16*1024)
		for {
			time.Sleep(20 * time.Millisecond)
			if _, err := server.Read(buffer); err != nil {
				return
			}
		}
	}()
	started := time.Now()
	if _, err := conn.Write(make([]byte, 1024*1024)); err == nil {
		t.Fatal("whole write bound did not expire")
	}
	if time.Since(started) > time.Second {
		t.Fatal("whole write bound exceeded")
	}
	_ = server.Close()
	<-done
}
