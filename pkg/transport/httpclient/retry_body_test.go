package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// replayableRequest builds a POST whose original body and every GetBody copy
// count their closes separately.
func replayableRequest(t *testing.T, originalClosed, copiesClosed *atomic.Int32) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://api.example/data", nil)
	require.NoError(t, err)
	req.Body = &countedReadCloser{Reader: strings.NewReader("body"), closed: originalClosed}
	req.GetBody = func() (io.ReadCloser, error) {
		return &countedReadCloser{Reader: strings.NewReader("body"), closed: copiesClosed}, nil
	}

	return req
}

// closingBase behaves like net/http's transport: it closes the body it is
// handed, then answers status.
func closingBase(status int) rtFunc {
	return func(r *http.Request) (*http.Response, error) {
		if r.Body != nil {
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
		}

		return rtResponse(r, status, "ok"), nil
	}
}

type failingLimiter struct{}

func (failingLimiter) Wait(context.Context) error { return errors.New("limiter closed") }

func TestRoundTripClosesTheOriginalBody(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		cfg        RetryConfig
		wantErr    bool
		wantCopies int32 // GetBody copies Base received and closed
	}{
		// With Sign or Bearer every attempt runs on a clone carrying a GetBody
		// copy, so Base never sees the original.
		{name: "signed success", cfg: RetryConfig{Base: closingBase(http.StatusOK), Sign: func(*http.Request) error { return nil }}, wantCopies: 1},
		{name: "bearer success", cfg: RetryConfig{Base: closingBase(http.StatusOK), Bearer: &fakeBearerSource{current: "tok"}}, wantCopies: 1},
		// Nothing reaches Base at all.
		{name: "limiter failure before send", cfg: RetryConfig{Base: closingBase(http.StatusOK), Limiter: failingLimiter{}}, wantErr: true},
		{name: "bearer failure before send", cfg: RetryConfig{Base: closingBase(http.StatusOK), Bearer: &fakeBearerSource{refreshErr: errors.New("mint failed")}}, wantErr: true},
		// The first unauthenticated attempt hands the original to Base, which
		// closes it; RoundTrip must not close it a second time.
		{name: "unauthenticated first attempt", cfg: RetryConfig{Base: closingBase(http.StatusOK)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var originalClosed, copiesClosed atomic.Int32
			tc.cfg.Logger = rtDiscardLogger()
			resp, err := NewRetryTransport(tc.cfg).RoundTrip(replayableRequest(t, &originalClosed, &copiesClosed))
			if resp != nil {
				_ = resp.Body.Close()
			}
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.EqualValues(t, 1, originalClosed.Load(), "the caller's body is closed exactly once")
			require.EqualValues(t, tc.wantCopies, copiesClosed.Load(), "every GetBody copy Base received is closed by Base")
		})
	}
}

// TestRetryTransportIsSafeForConcurrentUse shares one transport (bearer source,
// limiter, signer, metrics) across goroutines whose tokens are rejected and
// re-minted under them. Run with -race: the per-request state (attempt count,
// carried token) must not be shared between RoundTrip calls.
func TestRetryTransportIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	source := &fakeBearerSource{current: "old", replacement: "new"}
	var unauthorized atomic.Int32
	transport := NewRetryTransport(RetryConfig{
		Base: rtFunc(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Authorization") == "Bearer old" {
				unauthorized.Add(1)

				return rtResponse(r, http.StatusUnauthorized, "rejected"), nil
			}

			return rtResponse(r, http.StatusOK, "ok"), nil
		}),
		Bearer:  source,
		Limiter: NewTokenBucket(1e6, 1000),
		Sign: func(r *http.Request) error {
			r.Header.Set("X-Signed", "1")

			return nil
		},
		Logger: rtDiscardLogger(),
	})

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 25 {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.example/data", nil)
				if err != nil {
					t.Error(err)

					return
				}
				resp, err := transport.RoundTrip(req)
				if err != nil {
					t.Error(err)

					return
				}
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Errorf("status = %d, want 200 after the shared re-mint", resp.StatusCode)
				}
			}
		})
	}
	wg.Wait()

	require.Positive(t, unauthorized.Load(), "some requests carried the rejected token")
	require.Contains(t, source.invalidated(), "old")
}
