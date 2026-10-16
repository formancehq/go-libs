package httpclient

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSnapshotResponseBodyRetainsBoundAndReadError(t *testing.T) {
	sentinel := errors.New("read stopped")
	original := &snapshotFailBody{err: sentinel}
	response := &http.Response{Body: original}
	head, err := SnapshotResponseBody(response, 10)
	if string(head) != "abc" || !errors.Is(err, sentinel) {
		t.Fatalf("head=%q err=%v", head, err)
	}
	head, err = io.ReadAll(response.Body)
	if string(head) != "abc" || !errors.Is(err, sentinel) {
		t.Fatalf("restored=%q err=%v", head, err)
	}
	_ = response.Body.Close()
	if original.closed != 1 {
		t.Fatalf("closed=%d", original.closed)
	}
	response.Body = io.NopCloser(strings.NewReader("abcdef"))
	head, err = SnapshotResponseBody(response, 2)
	if string(head) != "abc" || err != nil {
		t.Fatalf("bounded window=%q err=%v", head, err)
	}
	head, err = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(head) != "abcdef" || err != nil {
		t.Fatalf("whole body=%q err=%v", head, err)
	}
	for _, limit := range []int64{-1, 1<<63 - 1} {
		body := &snapshotFailBody{err: sentinel}
		response.Body = body
		if _, err := SnapshotResponseBody(response, limit); err == nil || body.read {
			t.Fatal("invalid limit read the body")
		}
		_ = body.Close()
	}
}

type snapshotFailBody struct {
	err    error
	read   bool
	closed int
}

func (b *snapshotFailBody) Read(p []byte) (int, error) {
	if b.read {
		return 0, io.EOF
	}
	b.read = true
	return copy(p, "abc"), b.err
}
func (b *snapshotFailBody) Close() error { b.closed++; return nil }
