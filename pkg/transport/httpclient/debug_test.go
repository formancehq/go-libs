package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

func TestDebugHTTPTransportSkipsDumpWhenDebugDisabled(t *testing.T) {
	t.Parallel()

	logger := &recordingLogger{}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	transport := NewDebugHTTPTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	}))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.com", errorReadCloser{err: errors.New("body should not be read")})
	require.NoError(t, err)

	rsp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rsp.StatusCode)
	require.Empty(t, logger.debugMessages)
}

func TestDebugHTTPTransportRedactsAuthorizationHeader(t *testing.T) {
	t.Parallel()

	logger := &recordingLogger{debugEnabled: true}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	transport := NewDebugHTTPTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("response body")),
		}, nil
	}))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.com", strings.NewReader("request body"))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer secret-token")

	rsp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	body, err := io.ReadAll(rsp.Body)
	require.NoError(t, err)
	require.Equal(t, "response body", string(body))

	logs := strings.Join(logger.debugMessages, "\n")
	require.Contains(t, logs, "Authorization: [REDACTED]")
	require.NotContains(t, logs, "Bearer secret-token")
}

func TestDebugHTTPTransportDoesNotPanicOnResponseDumpError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("read failed")
	logger := &recordingLogger{debugEnabled: true}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	transport := NewDebugHTTPTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       errorReadCloser{err: readErr},
		}, nil
	}))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	var rsp *http.Response
	require.NotPanics(t, func() {
		rsp, err = transport.RoundTrip(req)
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rsp.StatusCode)
	require.Contains(t, strings.Join(logger.debugMessages, "\n"), "failed to dump HTTP response")

	_, err = io.ReadAll(rsp.Body)
	require.ErrorIs(t, err, readErr)
}

// TestDebugHTTPTransportDoesNotLeakAPIKeyInPath is the end-to-end guard for the
// finding: a connector whose credential lives in the base-URL path must not have
// it appear in a dump. The URL is the Alchemy shape, where the API key is a path
// segment of every request.
func TestDebugHTTPTransportDoesNotLeakAPIKeyInPath(t *testing.T) {
	t.Parallel()

	const apiKey = "alcht_0123456789abcdefghijklmnop"

	logger := &recordingLogger{debugEnabled: true}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	transport := NewDebugHTTPTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"result":"0x1"}`)),
		}, nil
	}))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://eth-mainnet.g.alchemy.com/v2/"+apiKey, strings.NewReader(`{"method":"eth_blockNumber"}`))
	require.NoError(t, err)

	_, err = transport.RoundTrip(req)
	require.NoError(t, err)

	logs := strings.Join(logger.debugMessages, "\n")
	require.NotEmpty(t, logs)
	require.NotContains(t, logs, apiKey)
	require.Contains(t, logs, "/v2/[REDACTED]")
	// The dump is only worth having if the rest of it stays legible.
	require.Contains(t, logs, "eth_blockNumber")
}

// TestDebugHTTPTransportTruncatesDumpButKeepsBodyReadable pins the part a naive
// dump breaks: bounding the log line must not shorten what the caller decodes.
func TestDebugHTTPTransportTruncatesDumpButKeepsBodyReadable(t *testing.T) {
	t.Parallel()

	// Far larger than the cap, so the re-wrap has both a buffered head and a tail.
	full := strings.Repeat("y", DefaultMaxDumpBody*8)

	logger := &recordingLogger{debugEnabled: true}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	transport := NewDebugHTTPTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(full)),
		}, nil
	}))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	rsp, err := transport.RoundTrip(req)
	require.NoError(t, err)

	// The caller still reads every byte.
	body, err := io.ReadAll(rsp.Body)
	require.NoError(t, err)
	require.Equal(t, full, string(body))
	require.NoError(t, rsp.Body.Close())

	// The dump does not.
	logs := strings.Join(logger.debugMessages, "\n")
	require.Equal(t, DefaultMaxDumpBody, strings.Count(logs, "y"))
	require.Contains(t, logs, "...[truncated]")
}

// TestDebugHTTPTransportShowsNonSensitiveHeadersInFull pins that the dump reports
// values rather than mere presence: a Content-Type or a rate-limit header is
// usually the thing being looked for.
func TestDebugHTTPTransportShowsNonSensitiveHeadersInFull(t *testing.T) {
	t.Parallel()

	const apiKey = "bitstamp-api-key-abc123"

	logger := &recordingLogger{debugEnabled: true}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	transport := NewDebugHTTPTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type":        {"application/json"},
				"Etag":                {`W/"686897696a7c876b7e"`},
				"Ratelimit-Remaining": {"42"},
				"Set-Cookie":          {"session=super-secret"},
			},
			Body: io.NopCloser(strings.NewReader("{}")),
		}, nil
	}))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.com", strings.NewReader("limit=10"))
	require.NoError(t, err)
	// Bitstamp puts "BITSTAMP <apiKey>" in a bare X-Auth, so the value is the
	// credential; its siblings carry no secret and must stay legible.
	req.Header.Set("X-Auth", "BITSTAMP "+apiKey)
	req.Header.Set("X-Auth-Timestamp", "1690000000000")
	req.Header.Set("X-Auth-Version", "v2")
	req.Header.Set("Accept", "application/json")

	_, err = transport.RoundTrip(req)
	require.NoError(t, err)

	logs := strings.Join(logger.debugMessages, "\n")
	require.NotContains(t, logs, apiKey)
	require.NotContains(t, logs, "super-secret")
	require.Contains(t, logs, "X-Auth: [REDACTED]")
	require.Contains(t, logs, "Set-Cookie: [REDACTED]")
	for _, want := range []string{
		"Accept: application/json",
		"X-Auth-Timestamp: 1690000000000",
		"X-Auth-Version: v2",
		"Content-Type: application/json",
		`Etag: W/"686897696a7c876b7e"`,
		"Ratelimit-Remaining: 42",
	} {
		require.Contains(t, logs, want)
	}
	require.NotContains(t, logs, "[present]")
}

// TestDebugHTTPTransportRedactsRequestBodySecrets covers the payload half: an
// OAuth exchange carries its credential in the body, not in a header.
func TestDebugHTTPTransportRedactsRequestBodySecrets(t *testing.T) {
	t.Parallel()

	logger := &recordingLogger{debugEnabled: true}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	transport := NewDebugHTTPTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// The transport still sends the body verbatim.
		sent, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Contains(t, string(sent), "sk-live-secret")

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"leak-me-not","expires_in":3600}`)),
		}, nil
	}))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.com/oauth/token",
		strings.NewReader(`{"client_secret":"sk-live-secret","grant_type":"client_credentials"}`))
	require.NoError(t, err)

	rsp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	body, err := io.ReadAll(rsp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "leak-me-not")

	logs := strings.Join(logger.debugMessages, "\n")
	require.NotContains(t, logs, "sk-live-secret")
	require.NotContains(t, logs, "leak-me-not")
	require.Contains(t, logs, "client_credentials")
	require.Contains(t, logs, "expires_in")
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

type recordingLogger struct {
	debugEnabled  bool
	debugMessages []string
}

func (l *recordingLogger) Tracef(string, ...any) {}
func (l *recordingLogger) Debugf(format string, args ...any) {
	l.debugMessages = append(l.debugMessages, fmt.Sprintf(format, args...))
}
func (l *recordingLogger) Infof(string, ...any)  {}
func (l *recordingLogger) Errorf(string, ...any) {}
func (l *recordingLogger) Trace(...any)          {}
func (l *recordingLogger) Debug(args ...any) {
	l.debugMessages = append(l.debugMessages, fmt.Sprint(args...))
}
func (l *recordingLogger) Info(...any)  {}
func (l *recordingLogger) Error(...any) {}
func (l *recordingLogger) WithFields(map[string]any) logging.Logger {
	return l
}
func (l *recordingLogger) WithField(string, any) logging.Logger {
	return l
}
func (l *recordingLogger) WithContext(context.Context) logging.Logger {
	return l
}
func (l *recordingLogger) Writer() io.Writer {
	return io.Discard
}
func (l *recordingLogger) Enabled(level logging.Level) bool {
	return l.debugEnabled && level == logging.DebugLevel
}
