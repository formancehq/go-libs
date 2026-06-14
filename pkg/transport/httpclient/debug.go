package httpclient

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"strings"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

type httpTransport struct {
	underlying http.RoundTripper
}

func (h httpTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	logger := logging.FromContext(request.Context())
	debugEnabled := logger.Enabled(logging.DebugLevel)
	if debugEnabled {
		data, err := dumpRequest(request, true)
		if err != nil {
			return nil, fmt.Errorf("dump http request: %w", err)
		}
		logger.Debug(string(data))
	}

	rsp, err := h.underlying.RoundTrip(request)
	if err != nil {
		return nil, err
	}

	if debugEnabled {
		data, err := dumpResponse(rsp, true)
		if err != nil {
			logger.Debugf("failed to dump HTTP response: %v", err)
			return rsp, nil
		}
		logger.Debug(string(data))
	}

	return rsp, nil
}

var _ http.RoundTripper = &httpTransport{}

func NewDebugHTTPTransport(underlying http.RoundTripper) *httpTransport {
	return &httpTransport{
		underlying: underlying,
	}
}

func dumpRequest(req *http.Request, includeBody bool) ([]byte, error) {
	cloned := req.Clone(req.Context())
	cloned.Header = req.Header.Clone()
	redactSensitiveHeaders(cloned.Header)

	if !includeBody {
		return httputil.DumpRequestOut(cloned, false)
	}

	if req.Body == nil || req.Body == http.NoBody {
		cloned.Body = req.Body
		return httputil.DumpRequestOut(cloned, true)
	}

	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		cloned.Body = body
		return httputil.DumpRequestOut(cloned, true)
	}

	data, readErr := io.ReadAll(req.Body)
	closeErr := req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(data))
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}

	cloned.Body = io.NopCloser(bytes.NewReader(data))
	return httputil.DumpRequestOut(cloned, true)
}

func dumpResponse(rsp *http.Response, includeBody bool) ([]byte, error) {
	cloned := new(http.Response)
	*cloned = *rsp
	cloned.Header = rsp.Header.Clone()
	redactSensitiveHeaders(cloned.Header)

	data, err := httputil.DumpResponse(cloned, includeBody)
	rsp.Body = cloned.Body
	rsp.ContentLength = cloned.ContentLength
	return data, err
}

func redactSensitiveHeaders(headers http.Header) {
	for name := range headers {
		if isSensitiveHeader(name) {
			headers.Set(name, "[REDACTED]")
		}
	}
}

func isSensitiveHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "cookie", "proxy-authorization", "set-cookie":
		return true
	default:
		return false
	}
}
