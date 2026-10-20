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

func TestDebugHTTPTransportPreservesTraffic(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, status := range []int{http.StatusOK, http.StatusSwitchingProtocols} {
			t.Run(fmt.Sprintf("debug=%t/status=%d", enabled, status), func(t *testing.T) {
				logger := &recordingLogger{debugEnabled: enabled}
				ctx := logging.ContextWithLogger(t.Context(), logger)
				requestBody := &observedBody{Reader: strings.NewReader("synthetic-request-body")}
				responseBody := &observedBody{Reader: strings.NewReader("synthetic-response-body")}
				req, err := http.NewRequestWithContext(ctx, http.MethodPost,
					"https://synthetic-user:synthetic-password@synthetic-host.test/synthetic-path?key=synthetic-query", requestBody)
				require.NoError(t, err)
				req.Host = "synthetic-host.test"
				req.Header.Set("Authorization", "synthetic-request-header")
				req.GetBody = func() (io.ReadCloser, error) { t.Fatal("diagnostic opened a body copy"); return nil, nil }
				response := &http.Response{StatusCode: status, Status: "synthetic-status-text",
					Proto: "synthetic-proto", Header: http.Header{"X-Secret": {"synthetic-response-header"}}, Body: responseBody}
				calls := 0
				transport := NewDebugHTTPTransport(roundTripFunc(func(got *http.Request) (*http.Response, error) {
					calls++
					require.Same(t, req, got)
					require.Same(t, requestBody, got.Body)
					require.Zero(t, requestBody.reads)
					require.Zero(t, requestBody.closes)
					require.Equal(t, "synthetic-request-header", got.Header.Get("Authorization"))
					data, readErr := io.ReadAll(got.Body)
					require.NoError(t, readErr)
					require.Equal(t, "synthetic-request-body", string(data))
					return response, nil
				}))
				got, err := transport.RoundTrip(req)
				require.NoError(t, err)
				require.Same(t, response, got)
				require.Same(t, responseBody, got.Body)
				require.Equal(t, 1, calls)
				require.Zero(t, requestBody.closes)
				require.Zero(t, responseBody.reads)
				require.Zero(t, responseBody.closes)
				if enabled {
					require.Equal(t, []string{"Sending HTTP request", fmt.Sprintf("HTTP response status: %d", status)}, logger.debugMessages)
				} else {
					require.Empty(t, logger.debugMessages)
				}
				require.NotContains(t, strings.Join(logger.debugMessages, "\n"), "synthetic-")
				// Upgraded connections must retain their write capability.
				if status == http.StatusSwitchingProtocols {
					_, ok := got.Body.(io.ReadWriteCloser)
					require.True(t, ok)
				}
				data, readErr := io.ReadAll(got.Body)
				require.NoError(t, readErr)
				require.Equal(t, "synthetic-response-body", string(data))
				require.NoError(t, got.Body.Close())
				require.Equal(t, 1, responseBody.closes)
			})
		}
	}
}

func TestDebugHTTPTransportPreservesErrors(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("debug=%t", enabled), func(t *testing.T) {
			logger := &recordingLogger{debugEnabled: enabled}
			req, err := http.NewRequestWithContext(logging.ContextWithLogger(t.Context(), logger), http.MethodGet, "https://example.test", nil)
			require.NoError(t, err)
			failure := errors.New("synthetic-transport-secret")
			response := &http.Response{StatusCode: http.StatusBadGateway}
			calls := 0
			transport := NewDebugHTTPTransport(roundTripFunc(func(got *http.Request) (*http.Response, error) {
				calls++
				require.Same(t, req, got)
				return response, failure
			}))
			got, err := transport.RoundTrip(req)
			require.Same(t, response, got)
			require.Same(t, failure, err)
			require.Equal(t, 1, calls)
			if enabled {
				require.Equal(t, []string{"Sending HTTP request", "HTTP request failed"}, logger.debugMessages)
			} else {
				require.Empty(t, logger.debugMessages)
			}
		})
	}
}

func TestDebugHTTPTransportLeavesReadErrorsToCaller(t *testing.T) {
	failure := errors.New("synthetic-read-secret")
	body := &observedBody{Reader: failingReader{failure}}
	logger := &recordingLogger{debugEnabled: true}
	req, err := http.NewRequestWithContext(logging.ContextWithLogger(t.Context(), logger), http.MethodGet, "https://example.test", nil)
	require.NoError(t, err)
	response := &http.Response{StatusCode: http.StatusOK, Body: body}
	transport := NewDebugHTTPTransport(roundTripFunc(func(*http.Request) (*http.Response, error) { return response, nil }))
	got, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Zero(t, body.reads)
	require.Zero(t, body.closes)
	_, err = got.Body.Read(make([]byte, 1))
	require.Same(t, failure, err)
	require.NotContains(t, strings.Join(logger.debugMessages, "\n"), "synthetic-")
}

type observedBody struct {
	io.Reader
	reads, closes int
}

func (b *observedBody) Read(p []byte) (int, error)  { b.reads++; return b.Reader.Read(p) }
func (b *observedBody) Close() error                { b.closes++; return nil }
func (b *observedBody) Write(p []byte) (int, error) { return len(p), nil }

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
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
