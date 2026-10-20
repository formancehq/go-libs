package httpclient

import (
	"errors"
	"io"
	"math"
	"strings"
	"testing"
)

func TestReadBoundedAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		limit      int64
		oversized  bool
	}{
		{"below", "abc", 4, false}, {"exact", "abcd", 4, false},
		{"over", "abcde", 4, true}, {"zero-default", "abc", 0, false},
		{"negative-default", "abc", -1, false}, {"largest-limit", "abc", math.MaxInt64, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ReadBounded(strings.NewReader(tc.body), tc.limit)
			if tc.oversized {
				if got != nil || !errors.Is(err, ErrResponseTooLarge) {
					t.Fatalf("got %q, %v", got, err)
				}
				return
			}
			if err != nil || string(got) != tc.body {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

func TestReadBoundedWindowLeavesRemainderUnread(t *testing.T) {
	r := strings.NewReader("0123456789")
	data, exceeded, err := ReadBoundedWindow(r, 4)
	if err != nil || !exceeded || string(data) != "01234" || r.Len() != 5 {
		t.Fatalf("got %q, %v, %v; unread %d", data, exceeded, err, r.Len())
	}
}

type failingBody struct{ cause error }

func (r failingBody) Read(p []byte) (int, error) { return copy(p, "abc"), r.cause }

func TestReadBoundedPreservesReadFailure(t *testing.T) {
	cause := errors.New("read interrupted")
	data, exceeded, err := ReadBoundedWindow(failingBody{cause}, 2)
	if string(data) != "abc" || exceeded || !errors.Is(err, cause) {
		t.Fatalf("got %q, %v, %v", data, exceeded, err)
	}
	data, err = ReadBounded(failingBody{cause}, 2)
	if data != nil || !errors.Is(err, cause) {
		t.Fatalf("got %q, %v", data, err)
	}
}

func TestReadBoundedEmpty(t *testing.T) {
	data, err := ReadBounded(strings.NewReader(""), 1)
	if len(data) != 0 || err != nil {
		t.Fatalf("got %q, %v", data, err)
	}
	data, _, err = ReadBoundedWindow(failingBody{io.ErrUnexpectedEOF}, 4)
	if string(data) != "abc" || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("got %q, %v", data, err)
	}
}
