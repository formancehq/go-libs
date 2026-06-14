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

	logger := &recordingLogger{debugEnabled: true}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	transport := NewDebugHTTPTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       errorReadCloser{err: errors.New("read failed")},
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
