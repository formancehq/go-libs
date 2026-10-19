package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

type pacingTimeoutSource struct {
	calls, failAt int
	refreshed     bool
	cancel        context.CancelFunc
	failure       error
}

func (s *pacingTimeoutSource) Token(ctx context.Context) (string, error) {
	s.calls++
	if s.calls == s.failAt {
		if s.cancel != nil {
			s.cancel()
			s.failure = ctx.Err()
		} else {
			child, cancel := context.WithDeadline(ctx, time.Unix(1, 0))
			defer cancel()
			s.failure = fmt.Errorf("source-owned timeout: %w", child.Err())
		}
		return "", s.failure
	}
	if !s.refreshed {
		return "old", nil
	}
	return "new", nil
}
func (s *pacingTimeoutSource) Invalidate(string) { s.refreshed = true }

func TestPacingPreservesTokenTimeoutProvenance(t *testing.T) {
	for _, tc := range []struct {
		name            string
		status, failAt  int
		limiter, cancel bool
	}{
		{"replay acquisition", 401, 3, false, false},
		{"replay revalidation", 401, 5, true, false},
		{"ordinary retry acquisition", 503, 2, false, false},
		{"ordinary retry revalidation", 503, 4, true, false},
		{"caller cancels replay", 401, 3, false, true},
		{"caller cancels revalidation", 401, 5, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			source := &pacingTimeoutSource{failAt: tc.failAt}
			if tc.cancel {
				source.cancel = cancel
			}
			mp, collect := meterFixture(t)
			calls := 0
			cfg := RetryConfig{Logger: logging.NopZap(), Bearer: source, MaxElapsed: time.Minute, MaxAttempts: 2,
				BaseDelay: time.Nanosecond, MaxDelay: time.Nanosecond, MeterProvider: mp,
				Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("rejected")), Request: req}, nil
				}),
			}
			if tc.limiter {
				cfg.Limiter = &countingLimiter{}
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.test/resource", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := NewRetryClient(cfg).Do(req)
			if resp != nil {
				_ = resp.Body.Close()
			}
			if source.failure == nil || !errors.Is(err, source.failure) {
				t.Fatalf("original token error lost: %v", err)
			}
			if tc.cancel {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("caller cancellation lost: %v", err)
				}
			} else if ctx.Err() != nil {
				t.Fatal("transport cancelled caller on independent source timeout")
			}
			if calls != 1 || source.calls != tc.failAt {
				t.Fatalf("wire calls=%d token calls=%d", calls, source.calls)
			}
			got := collect()
			if n := got.total(metricRetryExhausted); n != 0 {
				t.Fatalf("false transport-budget exhaustion: %d", n)
			}
		})
	}
}
