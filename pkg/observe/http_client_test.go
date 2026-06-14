package observe

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBodiesTracingHTTPTransportDoesNotDumpSuccessfulRequestWhenDebugDisabled(t *testing.T) {
	t.Parallel()

	transport := WithBodiesTracingHTTPTransport{
		underlying: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       http.NoBody,
			}, nil
		}),
		debug: false,
	}
	req, err := http.NewRequest(http.MethodPost, "http://example.com", errorReadCloser{err: errors.New("body should not be read")})
	require.NoError(t, err)

	var rsp *http.Response
	require.NotPanics(t, func() {
		rsp, err = transport.RoundTrip(req)
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rsp.StatusCode)
}

func TestBodiesTracingHTTPTransportReturnsRequestDumpError(t *testing.T) {
	t.Parallel()

	called := false
	transport := WithBodiesTracingHTTPTransport{
		underlying: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return &http.Response{StatusCode: http.StatusOK}, nil
		}),
		debug: true,
	}
	req, err := http.NewRequest(http.MethodPost, "http://example.com", errorReadCloser{err: errors.New("read failed")})
	require.NoError(t, err)

	require.NotPanics(t, func() {
		_, err = transport.RoundTrip(req)
	})

	require.ErrorContains(t, err, "dump http request")
	require.False(t, called)
}

func TestBodiesTracingHTTPTransportDoesNotPanicOnResponseDumpError(t *testing.T) {
	t.Parallel()

	transport := WithBodiesTracingHTTPTransport{
		underlying: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       errorReadCloser{err: errors.New("read failed")},
			}, nil
		}),
		debug: true,
	}
	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	var rsp *http.Response
	require.NotPanics(t, func() {
		rsp, err = transport.RoundTrip(req)
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, rsp.StatusCode)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type errorReadCloser struct {
	err error
}

func (r errorReadCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (r errorReadCloser) Close() error {
	return nil
}
