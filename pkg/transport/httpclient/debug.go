package httpclient

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sort"
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
	var builder strings.Builder
	uri := req.URL.RequestURI()
	if uri == "" {
		uri = "/"
	}
	fmt.Fprintf(&builder, "%s %s %s\r\n", req.Method, uri, req.Proto)
	if req.Host != "" {
		fmt.Fprintf(&builder, "Host: %s\r\n", req.Host)
	}
	writeSanitizedHeaders(&builder, req.Header)
	builder.WriteString("\r\n")

	if !includeBody {
		return []byte(builder.String()), nil
	}

	if req.Body == nil || req.Body == http.NoBody {
		return []byte(builder.String()), nil
	}

	var data []byte
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		data, err = io.ReadAll(body)
		closeErr := body.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	} else {
		var readErr error
		data, readErr = io.ReadAll(req.Body)
		closeErr := req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(data))
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}

	builder.Write(data)
	return []byte(builder.String()), nil
}

func dumpResponse(rsp *http.Response, includeBody bool) ([]byte, error) {
	var builder strings.Builder
	proto := rsp.Proto
	if proto == "" {
		proto = "HTTP/1.1"
	}
	status := rsp.Status
	if status == "" {
		status = fmt.Sprintf("%03d %s", rsp.StatusCode, http.StatusText(rsp.StatusCode))
	}
	fmt.Fprintf(&builder, "%s %s\r\n", proto, status)
	writeSanitizedHeaders(&builder, rsp.Header)
	builder.WriteString("\r\n")

	if !includeBody || rsp.Body == nil || rsp.Body == http.NoBody {
		return []byte(builder.String()), nil
	}

	contentLength := rsp.ContentLength
	data, err := io.ReadAll(rsp.Body)
	closeErr := rsp.Body.Close()
	rsp.Body = newReplayReadCloser(data, err, closeErr)
	rsp.ContentLength = contentLength
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}

	builder.Write(data)
	return []byte(builder.String()), nil
}

func newReplayReadCloser(data []byte, readErr, closeErr error) io.ReadCloser {
	if readErr == nil && closeErr == nil {
		return io.NopCloser(bytes.NewReader(data))
	}
	return &replayReadCloser{
		reader:   bytes.NewReader(data),
		readErr:  readErr,
		closeErr: closeErr,
	}
}

type replayReadCloser struct {
	reader   *bytes.Reader
	readErr  error
	closeErr error
}

func (r *replayReadCloser) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err == io.EOF && r.readErr != nil {
		err = r.readErr
	}
	return n, err
}

func (r *replayReadCloser) Close() error {
	return r.closeErr
}

func writeSanitizedHeaders(builder *strings.Builder, headers http.Header) {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		// Debug logs only expose header presence; trace instrumentation can use
		// different sanitization because it goes through controlled backends.
		value := "[present]"
		if isSensitiveHeader(name) {
			value = "[REDACTED]"
		}
		fmt.Fprintf(builder, "%s: %s\r\n", name, value)
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
