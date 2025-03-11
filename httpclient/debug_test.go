package httpclient_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/formancehq/go-libs/v2/httpclient"
	"github.com/formancehq/go-libs/v2/logging"
	"github.com/stretchr/testify/require"
)

func TestDebugHTTPTransport(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name           string
		request        *http.Request
		responseStatus int
		responseBody   string
	}{
		{
			name: "GET request",
			request: func() *http.Request {
				req, _ := http.NewRequest("GET", "https://example.com/api", nil)
				req.Header.Set("Authorization", "Bearer token")
				return req
			}(),
			responseStatus: 200,
			responseBody:   `{"success": true}`,
		},
		{
			name: "POST request with body",
			request: func() *http.Request {
				body := strings.NewReader(`{"name": "test"}`)
				req, _ := http.NewRequest("POST", "https://example.com/api", body)
				req.Header.Set("Content-Type", "application/json")
				return req
			}(),
			responseStatus: 201,
			responseBody:   `{"id": 123}`,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Create a logger that doesn't output to avoid test noise
			logger := logging.NewDefaultLogger(io.Discard, true, false, false)
			ctx := logging.ContextWithLogger(context.Background(), logger)

			// Add logger to request context
			tc.request = tc.request.WithContext(ctx)

			// Create a debug transport with a custom RoundTripper
			transport := httpclient.NewDebugHTTPTransport(
				&mockRoundTripper{
					response: &http.Response{
						StatusCode: tc.responseStatus,
						Status:     http.StatusText(tc.responseStatus),
						Body:       io.NopCloser(strings.NewReader(tc.responseBody)),
						Header:     make(http.Header),
						Proto:      "HTTP/1.1",
						ProtoMajor: 1,
						ProtoMinor: 1,
					},
				},
			)

			// Execute the request
			resp, err := transport.RoundTrip(tc.request)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, tc.responseStatus, resp.StatusCode)

			// Verify response body is still readable
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, tc.responseBody, string(body))
		})
	}
}

// Mock RoundTripper for testing
type mockRoundTripper struct {
	response *http.Response
	err      error
}

func (m *mockRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return m.response, m.err
}
