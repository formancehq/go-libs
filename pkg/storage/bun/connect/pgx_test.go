package connect

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadWriteValidateConnectTimesOut(t *testing.T) {
	withReadWriteProbeTimeout(t, 100*time.Millisecond)
	server := startFakePG(t, 1)

	start := time.Now()
	db, err := OpenSQLDB(context.Background(), ConnectionOptions{
		DatabaseSourceName: server.dsn() + "&default_query_exec_mode=simple_protocol",
		MaxOpenConns:       1,
		MaxIdleConns:       1,
	})
	elapsed := time.Since(start)
	if db != nil {
		_ = db.Close()
	}

	if err == nil {
		t.Fatal("expected validate-connect probe to time out")
	}
	if elapsed > time.Second {
		t.Fatalf("validate-connect probe returned too late: %s", elapsed)
	}
	if got := server.showTransactionReadOnlyCount(); got != 1 {
		t.Fatalf("expected one transaction_read_only probe, got %d", got)
	}
}

func TestReadOnlyResetSessionTimesOut(t *testing.T) {
	withReadWriteProbeTimeout(t, 100*time.Millisecond)
	server := startFakePG(t, 2)

	db, err := OpenSQLDB(context.Background(), ConnectionOptions{
		DatabaseSourceName: server.dsn() + "&default_query_exec_mode=simple_protocol",
		MaxOpenConns:       1,
		MaxIdleConns:       1,
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	var value string
	err = db.QueryRowContext(ctx, "select 1").Scan(&value)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected reset-session probe to evict the blocked connection")
	}
	if elapsed > time.Second {
		t.Fatalf("reset-session probe returned too late: %s", elapsed)
	}
	if got := server.showTransactionReadOnlyCount(); got < 2 {
		t.Fatalf("expected validate-connect and reset-session probes, got %d", got)
	}
}

func withReadWriteProbeTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()

	previous := readWriteProbeTimeout
	readWriteProbeTimeout = timeout
	t.Cleanup(func() {
		readWriteProbeTimeout = previous
	})
}

type fakePGServer struct {
	ln            net.Listener
	conns         sync.Map
	closeOnce     sync.Once
	done          chan struct{}
	showCount     atomic.Int64
	hangShowAfter int64
}

func startFakePG(t *testing.T, hangShowAfter int64) *fakePGServer {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	server := &fakePGServer{ln: ln, done: make(chan struct{}), hangShowAfter: hangShowAfter}
	go server.acceptLoop()
	t.Cleanup(server.close)
	return server
}

func (s *fakePGServer) dsn() string {
	return "postgres://test:test@" + s.ln.Addr().String() + "/test?sslmode=disable"
}

func (s *fakePGServer) showTransactionReadOnlyCount() int64 {
	return s.showCount.Load()
}

func (s *fakePGServer) close() {
	s.closeOnce.Do(func() {
		close(s.done)
		_ = s.ln.Close()
		s.conns.Range(func(key, _ any) bool {
			_ = key.(net.Conn).Close()
			return true
		})
	})
}

func (s *fakePGServer) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.conns.Store(conn, struct{}{})
		go func() {
			defer s.conns.Delete(conn)
			defer func() {
				_ = conn.Close()
			}()
			_ = s.handleConn(conn)
		}()
	}
}

func (s *fakePGServer) handleConn(conn net.Conn) error {
	if err := readFakePGStartup(conn); err != nil {
		return err
	}
	if _, err := conn.Write(fakePGStartupOK()); err != nil {
		return err
	}

	for {
		typ := make([]byte, 1)
		if _, err := io.ReadFull(conn, typ); err != nil {
			return err
		}

		var lenBuf [4]byte
		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			return err
		}
		payload := make([]byte, int(binary.BigEndian.Uint32(lenBuf[:]))-4)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return err
		}

		if typ[0] != 'Q' {
			continue
		}

		query := strings.ToLower(strings.TrimSpace(strings.TrimRight(string(payload), "\x00")))
		switch query {
		case "show transaction_read_only":
			count := s.showCount.Add(1)
			if count >= s.hangShowAfter {
				<-s.done
				return nil
			}
			if _, err := conn.Write(fakePGQueryResult("transaction_read_only", "off", "SHOW")); err != nil {
				return err
			}
		case "show default_transaction_read_only":
			if _, err := conn.Write(fakePGQueryResult("default_transaction_read_only", "off", "SHOW")); err != nil {
				return err
			}
		case "select 1":
			if _, err := conn.Write(fakePGQueryResult("?column?", "1", "SELECT 1")); err != nil {
				return err
			}
		default:
			if _, err := conn.Write(fakePGEmptyQueryResult()); err != nil {
				return err
			}
		}
	}
}

func readFakePGStartup(conn net.Conn) error {
	for {
		var lenBuf [4]byte
		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			return err
		}
		n := int(binary.BigEndian.Uint32(lenBuf[:]))
		payload := make([]byte, n-4)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return err
		}
		if n == 8 && binary.BigEndian.Uint32(payload) == 80877103 {
			if _, err := conn.Write([]byte{'N'}); err != nil {
				return err
			}
			continue
		}
		return nil
	}
}

func fakePGStartupOK() []byte {
	var b bytes.Buffer
	fakePGWriteMsg(&b, 'R', func(p *bytes.Buffer) {
		fakePGWriteInt32(p, 0)
	})
	fakePGParameterStatus(&b, "server_version", "17.4")
	fakePGParameterStatus(&b, "server_encoding", "UTF8")
	fakePGParameterStatus(&b, "client_encoding", "UTF8")
	fakePGParameterStatus(&b, "DateStyle", "ISO, MDY")
	fakePGParameterStatus(&b, "integer_datetimes", "on")
	fakePGParameterStatus(&b, "standard_conforming_strings", "on")
	fakePGWriteMsg(&b, 'K', func(p *bytes.Buffer) {
		fakePGWriteInt32(p, 1)
		fakePGWriteInt32(p, 1)
	})
	fakePGWriteReady(&b)
	return b.Bytes()
}

func fakePGParameterStatus(b *bytes.Buffer, key, value string) {
	fakePGWriteMsg(b, 'S', func(p *bytes.Buffer) {
		p.WriteString(key)
		p.WriteByte(0)
		p.WriteString(value)
		p.WriteByte(0)
	})
}

func fakePGQueryResult(field, value, tag string) []byte {
	var b bytes.Buffer
	fakePGWriteMsg(&b, 'T', func(p *bytes.Buffer) {
		fakePGWriteInt16(p, 1)
		p.WriteString(field)
		p.WriteByte(0)
		fakePGWriteInt32(p, 0)
		fakePGWriteInt16(p, 0)
		fakePGWriteInt32(p, 25)
		fakePGWriteInt16(p, -1)
		fakePGWriteInt32(p, -1)
		fakePGWriteInt16(p, 0)
	})
	fakePGWriteMsg(&b, 'D', func(p *bytes.Buffer) {
		fakePGWriteInt16(p, 1)
		fakePGWriteInt32(p, int32(len(value)))
		p.WriteString(value)
	})
	fakePGWriteMsg(&b, 'C', func(p *bytes.Buffer) {
		p.WriteString(tag)
		p.WriteByte(0)
	})
	fakePGWriteReady(&b)
	return b.Bytes()
}

func fakePGEmptyQueryResult() []byte {
	var b bytes.Buffer
	fakePGWriteMsg(&b, 'I', func(*bytes.Buffer) {})
	fakePGWriteReady(&b)
	return b.Bytes()
}

func fakePGWriteReady(b *bytes.Buffer) {
	fakePGWriteMsg(b, 'Z', func(p *bytes.Buffer) {
		p.WriteByte('I')
	})
}

func fakePGWriteMsg(b *bytes.Buffer, typ byte, fn func(*bytes.Buffer)) {
	var payload bytes.Buffer
	fn(&payload)
	b.WriteByte(typ)
	fakePGWriteInt32(b, int32(payload.Len()+4))
	b.Write(payload.Bytes())
}

func fakePGWriteInt16(b *bytes.Buffer, v int16) {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], uint16(v))
	b.Write(buf[:])
}

func fakePGWriteInt32(b *bytes.Buffer, v int32) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(v))
	b.Write(buf[:])
}
