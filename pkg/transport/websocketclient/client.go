// Package websocketclient provides bounded WebSocket dialing and duplex I/O.
// Supervisor optionally owns transport session lifecycle and bounded raw-frame
// admission. Protocol messages, subscriptions, recovery and decoded-event queues
// belong to callers.
package websocketclient

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Conn is a bounded duplex connection. Write sends text messages; Close uses
// normal closure. Read, Write and Ping honor their contexts. Keep one reader
// servicing control frames while Ping runs concurrently: Ping waits for that
// reader to consume the pong. Suspending reads can therefore stall Ping even
// when the peer is healthy. Cancelling or timing out an active Read is terminal
// for this connection; callers must reconnect rather than reuse it.
type Conn interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, b []byte) error
	Ping(ctx context.Context) error
	Close() error
}

// Dialer opens a Conn to a URL.
type Dialer func(ctx context.Context, url string) (Conn, error)

// ReadLimit bounds one inbound message in bytes, including fragmented messages.
const ReadLimit = 32 << 20

// NewDialer returns a Dialer backed by github.com/coder/websocket and
// recording wsx.dial.attempts and wsx.dial.duration under scope (empty =
// DefaultMetricsScope). Supply an application scope to group its telemetry.
//
// Instrumentation is built once, here, rather than per dial: registering an
// instrument on every reconnect would churn the meter for a connection that is
// expected to be re-established on a backoff.
func NewDialer(scope string) Dialer {
	m := newMetrics(scope)

	return func(ctx context.Context, url string) (Conn, error) {
		start := time.Now()
		conn, err := dial(ctx, url)
		m.recordDial(ctx, time.Since(start), err)

		return conn, err
	}
}

// dial is the uninstrumented transport, so NewDialer owns the measurement and this
// stays a plain adapter over coder/websocket.
func dial(ctx context.Context, url string) (Conn, error) {
	headers := make(http.Header)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(headers))
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{ //nolint:bodyclose // coder/websocket owns and closes the handshake response body
		HTTPHeader: headers,
	})
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(ReadLimit)

	return &coderConn{conn: conn}, nil
}

// coderConn adapts a coder/websocket connection to Conn.
type coderConn struct{ conn *websocket.Conn }

func (k *coderConn) Read(ctx context.Context) ([]byte, error) {
	_, b, err := k.conn.Read(ctx)

	return b, err
}

func (k *coderConn) Write(ctx context.Context, b []byte) error {
	return k.conn.Write(ctx, websocket.MessageText, b)
}

func (k *coderConn) Ping(ctx context.Context) error { return k.conn.Ping(ctx) }

func (k *coderConn) Close() error { return k.conn.Close(websocket.StatusNormalClosure, "") }
