package httpclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

// debugReceived is what the test server saw, so tests can prove redaction is
// display-only: the provider still receives every credential byte.
type debugReceived struct {
	apiKey    string
	accessKey string
	body      []byte
}

func debugServer(t *testing.T, header http.Header, body []byte) (*httptest.Server, <-chan debugReceived) {
	t.Helper()

	received := make(chan debugReceived, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		received <- debugReceived{
			apiKey:    r.URL.Query().Get("api_key"),
			accessKey: r.Header.Get("X-Cb-Access-Key"),
			body:      data,
		}
		for name, values := range header {
			w.Header()[name] = values
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	return srv, received
}

func TestDebugHTTPTransportRedactsCredentialsAndKeepsDiagnostics(t *testing.T) {
	t.Parallel()

	const (
		pathKey     = "alcht_0123456789abcdefghijklmnop"
		requestBody = `{"side":"buy","api_key":"sk-live-secret"}`
		respBody    = `{"data":[{"id":"txn-1"}],"session_token":"leak-me-not"}`
	)
	srv, received := debugServer(t, http.Header{
		"Content-Type":        {"application/json"},
		"Etag":                {`"v1-etag"`},
		"Ratelimit-Remaining": {"42"},
		"Set-Cookie":          {"session=super-secret"},
	}, []byte(respBody))

	logger := &recordingLogger{debugEnabled: true}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	client := &http.Client{Transport: NewDebugHTTPTransport(srv.Client().Transport)}

	target := "http://svcuser:svcpass@" + strings.TrimPrefix(srv.URL, "http://") +
		"/v2/" + pathKey + "/getAssetTransfers?api_key=query-secret&cursor=next"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(requestBody))
	require.NoError(t, err)
	req.Header.Set("X-Cb-Access-Key", "cb-key-value")
	req.Header.Set("X-Auth", "BITSTAMP bitstamp-api-key-abc123")
	req.Header.Set("X-Auth-Version", "v2")
	req.Header.Set("X-Auth-Subaccount", "sub-42")

	rsp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = rsp.Body.Close() }()
	body, err := io.ReadAll(rsp.Body)
	require.NoError(t, err)
	require.Equal(t, respBody, string(body), "the caller must read the unredacted response")

	got := <-received
	require.Equal(t, "query-secret", got.apiKey)
	require.Equal(t, "cb-key-value", got.accessKey)
	require.Equal(t, requestBody, string(got.body), "the server must receive the unredacted request")

	logs := strings.Join(logger.debugMessages, "\n")
	for _, secret := range []string{
		"svcuser", "svcpass", base64.StdEncoding.EncodeToString([]byte("svcuser:svcpass")),
		pathKey, "query-secret", "cb-key-value", "bitstamp-api-key-abc123",
		"sk-live-secret", "super-secret", "leak-me-not",
	} {
		require.NotContains(t, logs, secret)
	}
	for _, want := range []string{
		"POST /v2/[REDACTED]/getAssetTransfers?api_key=%5BREDACTED%5D&cursor=next HTTP/1.1",
		"Authorization: [REDACTED]",
		"X-Cb-Access-Key: [REDACTED]",
		"X-Auth: [REDACTED]",
		"X-Auth-Version: v2",
		"X-Auth-Subaccount: sub-42",
		`"side":"buy"`,
		"Content-Type: application/json",
		`Etag: "v1-etag"`,
		"Ratelimit-Remaining: 42",
		"Set-Cookie: [REDACTED]",
		"txn-1",
	} {
		require.Contains(t, logs, want)
	}
}

// debugPayload is larger than size bytes and mixes credential-shaped JSON with
// every byte value, so a preview that redacted or truncated the caller's copy
// cannot go unnoticed.
func debugPayload(size int) []byte {
	var buf bytes.Buffer
	for i := 0; buf.Len() < size; i++ {
		fmt.Fprintf(&buf, "{\"api_key\":\"secret-%d\",\"n\":%d}\n", i, i)
		buf.WriteByte(byte(i))
	}

	return buf.Bytes()
}

func TestDebugHTTPTransportRestoresResponseBodyByteForByte(t *testing.T) {
	t.Parallel()

	payload := debugPayload(3 * DefaultMaxBodyBytes)
	srv, _ := debugServer(t, http.Header{"Content-Length": {fmt.Sprint(len(payload))}}, payload)

	logger := &recordingLogger{debugEnabled: true}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	client := &http.Client{Transport: NewDebugHTTPTransport(srv.Client().Transport)}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/export", nil)
	require.NoError(t, err)

	rsp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = rsp.Body.Close() }()
	body, err := io.ReadAll(rsp.Body)
	require.NoError(t, err)
	require.True(t, bytes.Equal(payload, body), "caller read %d bytes, want the exact %d-byte payload", len(body), len(payload))
	require.Equal(t, int64(len(payload)), rsp.ContentLength)

	logs := strings.Join(logger.debugMessages, "\n")
	require.Contains(t, logs, `{"api_key":"[REDACTED]","n":0}`)
	require.NotContains(t, logs, "secret-0")
	require.Contains(t, logs, "...[truncated]")
}

// debugCountingBody serves size 'b' bytes and records how many were consumed.
type debugCountingBody struct {
	size, read int
	closed     bool
}

func (b *debugCountingBody) Read(p []byte) (int, error) {
	if b.read >= b.size {
		return 0, io.EOF
	}
	n := min(len(p), b.size-b.read)
	for i := range n {
		p[i] = 'b'
	}
	b.read += n

	return n, nil
}

func (b *debugCountingBody) Close() error {
	b.closed = true
	return nil
}

func TestDebugHTTPTransportNeverBuffersLargeResponseBody(t *testing.T) {
	t.Parallel()

	const size = 8 << 20
	source := &debugCountingBody{size: size}
	logger := &recordingLogger{debugEnabled: true}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	transport := NewDebugHTTPTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: source, ContentLength: size}, nil
	}))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/large", nil)
	require.NoError(t, err)

	rsp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.LessOrEqual(t, source.read, DefaultMaxBodyBytes+1, "the preview consumed more than its cap from the body")

	logs := strings.Join(logger.debugMessages, "\n")
	require.LessOrEqual(t, len(logs), DefaultMaxBodyBytes+512, "a log entry must stay bounded whatever the body size")
	require.Equal(t, DefaultMaxBodyBytes, strings.Count(logs, "b"))
	require.Contains(t, logs, "...[truncated]")

	n, err := io.Copy(io.Discard, rsp.Body)
	require.NoError(t, err)
	require.Equal(t, int64(size), n)
	require.Equal(t, int64(size), rsp.ContentLength)
	require.NoError(t, rsp.Body.Close())
	require.True(t, source.closed, "closing the restored body must close the original")
}

func TestDebugHTTPTransportBoundsReplayableRequestPreview(t *testing.T) {
	t.Parallel()

	payload := bytes.Repeat([]byte("q"), 8*DefaultMaxBodyBytes)
	srv, received := debugServer(t, nil, []byte("ok"))

	logger := &recordingLogger{debugEnabled: true}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	client := &http.Client{Transport: NewDebugHTTPTransport(srv.Client().Transport)}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, srv.URL+"/upload", bytes.NewReader(payload))
	require.NoError(t, err)
	require.NotNil(t, req.GetBody)

	rsp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, rsp.Body.Close())

	require.True(t, bytes.Equal(payload, (<-received).body), "the server must receive the whole request body")
	logs := strings.Join(logger.debugMessages, "\n")
	require.Equal(t, DefaultMaxBodyBytes, strings.Count(logs, "q"))
	require.Contains(t, logs, "...[truncated]")
}

// debugStreamBody hides its concrete reader so http.NewRequest cannot set
// GetBody, the shape of a streamed upload.
type debugStreamBody struct {
	io.Reader
	closed bool
}

func (b *debugStreamBody) Close() error {
	b.closed = true
	return nil
}

func TestDebugHTTPTransportStreamsRequestBodyWithoutGetBody(t *testing.T) {
	t.Parallel()

	payload := debugPayload(8 * DefaultMaxBodyBytes)
	srv, received := debugServer(t, nil, []byte("ok"))

	logger := &recordingLogger{debugEnabled: true}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	client := &http.Client{Transport: NewDebugHTTPTransport(srv.Client().Transport)}
	stream := &debugStreamBody{Reader: bytes.NewReader(payload)}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/stream", stream)
	require.NoError(t, err)
	require.Nil(t, req.GetBody)

	rsp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, rsp.Body.Close())

	require.True(t, bytes.Equal(payload, (<-received).body), "the server must receive head and tail in order")
	require.Same(t, stream, req.Body, "the caller's request must not be modified")
	require.True(t, stream.closed, "the transport must still close the caller's body")

	logs := strings.Join(logger.debugMessages, "\n")
	require.Contains(t, logs, `{"api_key":"[REDACTED]","n":0}`)
	require.NotContains(t, logs, "secret-0")
	require.Contains(t, logs, "...[truncated]")
	require.LessOrEqual(t, len(logs), 2*DefaultMaxBodyBytes+1024)
}

func TestDebugHTTPTransportGetBodyFailureOnlyCostsThePreview(t *testing.T) {
	t.Parallel()

	logger := &recordingLogger{debugEnabled: true}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	var sent string
	transport := NewDebugHTTPTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		data, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		sent = string(data)
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
	}))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.com/x", strings.NewReader("payload"))
	require.NoError(t, err)
	req.GetBody = func() (io.ReadCloser, error) { return nil, errors.New("no replay") }

	rsp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, rsp.StatusCode)
	require.Equal(t, "payload", sent)
	require.Contains(t, strings.Join(logger.debugMessages, "\n"), "POST /x HTTP/1.1")
}

func debugRoundTrip(t *testing.T, transport http.RoundTripper, body string) string {
	t.Helper()

	logger := &recordingLogger{debugEnabled: true}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/x", nil)
	require.NoError(t, err)

	rsp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	read, err := io.ReadAll(rsp.Body)
	require.NoError(t, err)
	require.Equal(t, body, string(read))

	return strings.Join(logger.debugMessages, "\n")
}

func TestDebugHTTPTransportHonoursMaxBodyBytes(t *testing.T) {
	t.Parallel()

	serve := func(body string) roundTripFunc {
		return func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		}
	}

	t.Run("custom cap", func(t *testing.T) {
		t.Parallel()

		const body = "0123456789abcdefGHIJ-rest-of-body"
		logs := debugRoundTrip(t, NewDebugHTTPTransportWithConfig(serve(body), DebugConfig{MaxBodyBytes: 16}), body)
		require.Contains(t, logs, "0123456789abcdef...[truncated]")
		require.NotContains(t, logs, "GHIJ")
	})

	t.Run("zero selects the default", func(t *testing.T) {
		t.Parallel()

		body := strings.Repeat("z", DefaultMaxBodyBytes+100)
		logs := debugRoundTrip(t, NewDebugHTTPTransportWithConfig(serve(body), DebugConfig{}), body)
		require.Equal(t, DefaultMaxBodyBytes, strings.Count(logs, "z"))
	})

	t.Run("negative omits bodies", func(t *testing.T) {
		t.Parallel()

		const body = "response-body-text"
		logs := debugRoundTrip(t, NewDebugHTTPTransportWithConfig(serve(body), DebugConfig{MaxBodyBytes: -1}), body)
		require.Contains(t, logs, "HTTP/1.1 200 OK")
		require.NotContains(t, logs, body)
	})
}

func TestDebugHTTPTransportLeavesBodiesUntouchedWhenDebugDisabled(t *testing.T) {
	t.Parallel()

	logger := &recordingLogger{}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	responseBody := errorReadCloser{err: errors.New("response body should not be read")}
	requestBody := &debugStreamBody{Reader: errorReadCloser{err: errors.New("request body should not be read")}}
	var sentBody io.ReadCloser
	transport := NewDebugHTTPTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		sentBody = req.Body
		return &http.Response{StatusCode: http.StatusOK, Body: responseBody}, nil
	}))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.com", requestBody)
	require.NoError(t, err)

	rsp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Same(t, requestBody, sentBody)
	require.Equal(t, responseBody, rsp.Body)
	require.Empty(t, logger.debugMessages)
}

// debugUpgradeBody stands in for the io.ReadWriteCloser a 101 response carries.
type debugUpgradeBody struct {
	errorReadCloser
}

func (debugUpgradeBody) Write(p []byte) (int, error) { return len(p), nil }

func TestDebugHTTPTransportLeavesSwitchingProtocolsBodyAlone(t *testing.T) {
	t.Parallel()

	logger := &recordingLogger{debugEnabled: true}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	upgraded := &debugUpgradeBody{errorReadCloser{err: errors.New("upgraded connection must not be read")}}
	transport := NewDebugHTTPTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusSwitchingProtocols, Body: upgraded}, nil
	}))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/ws", nil)
	require.NoError(t, err)

	rsp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Same(t, upgraded, rsp.Body)
	_, ok := rsp.Body.(io.ReadWriteCloser)
	require.True(t, ok)
	require.Contains(t, strings.Join(logger.debugMessages, "\n"), "101 Switching Protocols")
}

func TestDebugHTTPTransportReplaysHeadBeforeReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("connection reset")
	logger := &recordingLogger{debugEnabled: true}
	ctx := logging.ContextWithLogger(context.Background(), logger)
	transport := NewDebugHTTPTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		body := io.MultiReader(strings.NewReader("partial"), errorReadCloser{err: readErr})
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Etag": {"e1"}}, Body: io.NopCloser(body)}, nil
	}))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	rsp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	data, err := io.ReadAll(rsp.Body)
	require.ErrorIs(t, err, readErr)
	require.Equal(t, "partial", string(data))

	logs := strings.Join(logger.debugMessages, "\n")
	require.Contains(t, logs, "Etag: e1", "the header portion is still logged")
	require.Contains(t, logs, "failed to dump HTTP response body: connection reset")
}
