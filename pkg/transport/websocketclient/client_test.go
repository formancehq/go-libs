package websocketclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestDialReadWriteAndClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
		defer cancel()
		for {
			kind, b, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if kind != websocket.MessageText {
				t.Errorf("outbound message type = %v, want text", kind)
				return
			}
			if err := conn.Write(ctx, websocket.MessageText, []byte(strings.ToUpper(string(b)))); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := NewDialer("")(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	if err := conn.Write(ctx, []byte("subscribe")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != "SUBSCRIBE" {
		t.Errorf("Read = %q, want SUBSCRIBE", got)
	}
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()

	readErr := make(chan error, 1)
	go func() {
		_, err := conn.Read(pingCtx)
		readErr <- err
	}()
	if err := conn.Ping(pingCtx); err != nil {
		t.Errorf("Ping: %v", err)
	}
	pingCancel()
	<-readErr

	if err := conn.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestDialFailsOnANonWebSocketEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if conn, err := NewDialer("")(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")); err == nil {
		_ = conn.Close()
		t.Fatal("Dial succeeded against a non-WebSocket endpoint")
	}
}

// property every streamer's shutdown path relies on.
func TestReadRespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		<-r.Context().Done() // never send anything
	}))
	defer srv.Close()

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer dialCancel()

	conn, err := NewDialer("")(dialCtx, "ws"+strings.TrimPrefix(srv.URL, "http"))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	readCtx, readCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer readCancel()

	if _, err := conn.Read(readCtx); err == nil {
		t.Fatal("Read returned with no error despite cancellation")
	} else if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context") {
		t.Errorf("Read error = %v, want a context error", err)
	}
}

func TestDialInjectsTraceContext(t *testing.T) {
	previous := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(previous) })
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Get("Traceparent")
		c, err := websocket.Accept(w, r, nil)
		if err == nil {
			_ = c.Close(websocket.StatusNormalClosure, "")
		}
	}))
	t.Cleanup(server.Close)
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled})
	ctx := trace.ContextWithSpanContext(t.Context(), sc)
	c, err := NewDialer("")(ctx, "ws"+strings.TrimPrefix(server.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	header := http.Header{"Traceparent": []string{<-received}}
	actual := trace.SpanContextFromContext(propagation.TraceContext{}.Extract(t.Context(), propagation.HeaderCarrier(header)))
	if actual.TraceID() != sc.TraceID() || actual.SpanID() != sc.SpanID() {
		t.Fatal("handshake did not inherit trace context")
	}
}

func TestInboundMessageIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.Close(websocket.StatusNormalClosure, "") }()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		defer cancel()
		_ = c.Write(ctx, websocket.MessageText, make([]byte, ReadLimit+1))
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	c, err := NewDialer("")(ctx, "ws"+strings.TrimPrefix(server.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if _, err := c.Read(ctx); err == nil || !strings.Contains(err.Error(), "read limited") {
		t.Fatalf("oversized message = %v, want read-limit rejection", err)
	}
}
