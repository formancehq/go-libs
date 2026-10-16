package webhook_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/formancehq/go-libs/v5/pkg/transport/webhook"
)

func TestObservedOverrunWithReadErrorStillRejectsSize(t *testing.T) {
	var observed error
	var window int
	h := newReceiver(t, webhook.Config{MaxBody: 16, ObserveBody: func(_ context.Context, _ *http.Request, b []byte, err error) { observed = err; window = len(b) }, Verifier: webhook.VerifierFunc(func(context.Context, *http.Request, []byte) error { t.Fatal("oversize verified"); return nil }), Deliver: func(context.Context, webhook.Delivery) error { t.Fatal("oversize delivered"); return nil }})
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Body = io.NopCloser(overrunAndError{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var maxErr *http.MaxBytesError
	if w.Code != 413 || !errors.As(observed, &maxErr) || window != 17 {
		t.Fatalf("status=%d observed=%v window=%d", w.Code, observed, window)
	}
}

type overrunAndError struct{}

func (overrunAndError) Read(p []byte) (int, error) {
	return copy(p, strings.Repeat("x", 17)), io.ErrUnexpectedEOF
}
func TestAdditionalMethodsAreAdvertisedByDefaultResponse(t *testing.T) {
	cfg := (&probe{}).config(16)
	cfg.MethodHandlers = map[string]http.Handler{"GET": http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
	h := newReceiver(t, cfg)
	w := serve(h, "PUT", nil)
	if got := w.Header().Get("Allow"); got != "HEAD, POST, GET" {
		t.Fatalf("Allow=%q", got)
	}
}
func TestObservedHTTP1OverrunRespondsWithoutRemainder(t *testing.T) {
	cfg := (&probe{}).config(16)
	cfg.ObserveBody = func(context.Context, *http.Request, []byte, error) {}
	server := httptest.NewUnstartedServer(newReceiver(t, cfg))
	server.Config.ReadTimeout = 3 * time.Second
	server.Start()
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	_, err = io.WriteString(conn, "POST / HTTP/1.1\r\nHost: example.test\r\nTransfer-Encoding: chunked\r\n\r\n40\r\n"+strings.Repeat("x", 17))
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("rejection waited for missing chunk remainder: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != 413 || !response.Close {
		t.Fatalf("status=%d close=%v", response.StatusCode, response.Close)
	}
}
