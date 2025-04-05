package otlp

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func TestWithBodiesTracingHTTPTransport_RoundTrip(t *testing.T) {
	baseTransport := &mockRoundTripper{
		response: &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("response body")),
			Header:     make(http.Header),
		},
	}

	transport := WithBodiesTracingHTTPTransport{
		Base:  baseTransport,
		Debug: true,
	}

	body := bytes.NewBufferString("request body")
	req := httptest.NewRequest(http.MethodPost, "https://example.com", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "response body", string(respBody))
}

func TestNewRoundTripper(t *testing.T) {
	baseTransport := &http.Transport{}

	rt := NewRoundTripper(baseTransport, false)
	require.NotNil(t, rt)
	_, ok := rt.(*otelhttp.Transport)
	require.True(t, ok)

	rt = NewRoundTripper(baseTransport, true)
	require.NotNil(t, rt)
	_, ok = rt.(WithBodiesTracingHTTPTransport)
	require.True(t, ok)

	rt = NewRoundTripper(baseTransport, false, otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
		return "test-span"
	}))
	require.NotNil(t, rt)
}

type mockRoundTripper struct {
	response *http.Response
	err      error
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.response, m.err
}
