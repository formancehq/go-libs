package httpclient

import (
	"net/http"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

type httpTransport struct {
	underlying http.RoundTripper
}

func (h httpTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	logger := logging.FromContext(request.Context())
	if !logger.Enabled(logging.DebugLevel) {
		return h.underlying.RoundTrip(request)
	}
	logger.Debug("Sending HTTP request")
	response, err := h.underlying.RoundTrip(request)
	if err != nil {
		logger.Debug("HTTP request failed")
	} else if response != nil {
		logger.Debugf("HTTP response status: %d", response.StatusCode)
	}
	return response, err
}

var _ http.RoundTripper = &httpTransport{}

// NewDebugHTTPTransport logs fixed events and numeric response status codes.
// It does not inspect request or response bodies, headers, URLs or error text.
func NewDebugHTTPTransport(underlying http.RoundTripper) *httpTransport {
	return &httpTransport{underlying: underlying}
}
