package publish

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// NATSBoundedDialer bounds a whole dial (including all resolved addresses) and
// raw writes below native NATS TLS. Install with nats.SetCustomDialer.
// NATS still owns TLS verification, heartbeats, server selection and reconnect.
type NATSBoundedDialer struct {
	dialer       net.Dialer
	dialTimeout  time.Duration
	writeTimeout time.Duration
	stallTimeout time.Duration
}

// NewNATSBoundedDialer creates an opt-in transport. WriteTimeout caps the entire
// raw Write; stallTimeout caps each 16KiB chunk. Choose the total bound for the
// maximum payload and minimum supported throughput, including TLS overhead.
func NewNATSBoundedDialer(dialer net.Dialer, dialTimeout, writeTimeout, stallTimeout time.Duration) (*NATSBoundedDialer, error) {
	if dialTimeout <= 0 || writeTimeout <= 0 || stallTimeout <= 0 || stallTimeout > writeTimeout {
		return nil, fmt.Errorf("NATS dial and write timeouts must be positive, stall timeout <= write timeout")
	}
	return &NATSBoundedDialer{dialer: dialer, dialTimeout: dialTimeout, writeTimeout: writeTimeout, stallTimeout: stallTimeout}, nil
}

func (d *NATSBoundedDialer) Dial(network, address string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d.dialTimeout)
	defer cancel()
	conn, err := d.dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	return &boundedNATSConn{Conn: conn, writeTimeout: d.writeTimeout, stallTimeout: d.stallTimeout}, nil
}

type boundedNATSConn struct {
	net.Conn
	writeTimeout   time.Duration
	stallTimeout   time.Duration
	writeMu        sync.Mutex
	deadlineMu     sync.Mutex
	writeDeadline  time.Time
	activeDeadline time.Time
	chunkDeadline  time.Time
}

func (c *boundedNATSConn) Write(data []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.deadlineMu.Lock()
	c.activeDeadline = time.Now().Add(c.writeTimeout)
	c.deadlineMu.Unlock()
	defer func() {
		c.deadlineMu.Lock()
		c.activeDeadline = time.Time{}
		c.chunkDeadline = time.Time{}
		c.deadlineMu.Unlock()
	}()
	total := 0
	for len(data) > 0 {
		c.deadlineMu.Lock()
		c.chunkDeadline = time.Now().Add(c.stallTimeout)
		err := c.applyWriteDeadline()
		c.deadlineMu.Unlock()
		if err != nil {
			_ = c.Conn.Close()
			return total, err
		}
		n, err := c.Conn.Write(data[:min(len(data), 16*1024)])
		total += n
		data = data[n:]
		if err != nil {
			_ = c.Conn.Close()
			return total, err
		}
		if n == 0 {
			_ = c.Conn.Close()
			return total, fmt.Errorf("NATS transport write made no progress")
		}
	}
	return total, nil
}

func (c *boundedNATSConn) SetWriteDeadline(deadline time.Time) error {
	c.deadlineMu.Lock()
	defer c.deadlineMu.Unlock()
	c.writeDeadline = deadline
	return c.applyWriteDeadline()
}

// Caller deadline updates cannot extend an active write or chunk deadline.
func (c *boundedNATSConn) applyWriteDeadline() error {
	deadline := c.writeDeadline
	for _, bound := range []time.Time{c.activeDeadline, c.chunkDeadline} {
		if !bound.IsZero() && (deadline.IsZero() || bound.Before(deadline)) {
			deadline = bound
		}
	}
	return c.Conn.SetWriteDeadline(deadline)
}

func (c *boundedNATSConn) SetDeadline(deadline time.Time) error {
	if err := c.SetReadDeadline(deadline); err != nil {
		return err
	}
	return c.SetWriteDeadline(deadline)
}
