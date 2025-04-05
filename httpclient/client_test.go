package httpclient

import (
	"context"
	"net/http"
	"testing"

	"github.com/formancehq/go-libs/v2/logging"
	"github.com/stretchr/testify/require"
)

func TestNewDebugHTTPTransport(t *testing.T) {
	underlying := http.DefaultTransport
	
	transport := NewDebugHTTPTransport(underlying)
	
	require.NotNil(t, transport, "Le transport ne devrait pas être nil")
	require.Equal(t, underlying, transport.underlying, "Le transport sous-jacent devrait être correctement défini")
}

func TestHttpTransport_RoundTrip(t *testing.T) {
	mockTransport := &mockRoundTripper{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       http.NoBody,
		},
	}
	
	transport := NewDebugHTTPTransport(mockTransport)
	
	logger := logging.Testing()
	ctx := logging.ContextWithLogger(context.Background(), logger)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
	require.NoError(t, err, "La création de la requête ne devrait pas échouer")
	
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err, "Le RoundTrip ne devrait pas échouer")
	require.Equal(t, http.StatusOK, resp.StatusCode, "Le code de statut devrait être OK")
	require.True(t, mockTransport.called, "Le transport sous-jacent devrait être appelé")
}

type mockRoundTripper struct {
	response *http.Response
	err      error
	called   bool
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	m.called = true
	return m.response, m.err
}
