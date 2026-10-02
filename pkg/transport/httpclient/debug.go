package httpclient

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/observe/redact"
)

// DefaultMaxBodyBytes is the body preview cap used when
// DebugConfig.MaxBodyBytes is zero.
const DefaultMaxBodyBytes = 4096

// DebugConfig bounds what the debug transport writes into a log entry.
type DebugConfig struct {
	// MaxBodyBytes caps each request and response body preview: at most
	// MaxBodyBytes+1 bytes are read to build it, and at most MaxBodyBytes
	// bytes of redacted text are logged, followed by "...[truncated]" when
	// the body is longer. Zero selects DefaultMaxBodyBytes; a negative value
	// omits body previews and logs only the request line, status line and
	// headers.
	MaxBodyBytes int
}

// httpTransport logs each request and response at debug level, when the
// context logger has debug enabled. Dumps are display-only and redacted with
// the shared redact policy: URL userinfo is dropped, credential query values,
// opaque path segments and credential header values are masked, and bodies
// are bounded, redacted previews. The bytes sent to the server and handed back
// to the caller are never altered or truncated.
type httpTransport struct {
	underlying   http.RoundTripper
	maxBodyBytes int
}

func (h httpTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	logger := logging.FromContext(request.Context())
	if !logger.Enabled(logging.DebugLevel) {
		return h.underlying.RoundTrip(request)
	}

	dump, outgoing := h.dumpRequest(request)
	logger.Debug(dump)

	rsp, err := h.underlying.RoundTrip(outgoing)
	if err != nil {
		return nil, err
	}

	dump, err = h.dumpResponse(rsp)
	logger.Debug(dump)
	if err != nil {
		logger.Debugf("failed to dump HTTP response body: %v", err)
	}

	return rsp, nil
}

var _ http.RoundTripper = &httpTransport{}

// NewDebugHTTPTransport wraps underlying with debug dumps using the default
// DebugConfig.
func NewDebugHTTPTransport(underlying http.RoundTripper) *httpTransport {
	return newDebugHTTPTransport(underlying, DebugConfig{})
}

// NewDebugHTTPTransportWithConfig wraps underlying with debug dumps bounded by
// cfg.
func NewDebugHTTPTransportWithConfig(underlying http.RoundTripper, cfg DebugConfig) http.RoundTripper {
	return newDebugHTTPTransport(underlying, cfg)
}

func newDebugHTTPTransport(underlying http.RoundTripper, cfg DebugConfig) *httpTransport {
	maxBodyBytes := cfg.MaxBodyBytes
	if maxBodyBytes == 0 {
		maxBodyBytes = DefaultMaxBodyBytes
	}

	return &httpTransport{
		underlying:   underlying,
		maxBodyBytes: maxBodyBytes,
	}
}

// dumpRequest renders the redacted request line, headers and body preview,
// and returns the request to send. That is req itself unless req has a body
// without GetBody: the preview then consumes a bounded head of the body, and a
// shallow copy carrying the head followed by the unread tail is sent instead,
// so the caller's request is never modified.
func (h httpTransport) dumpRequest(req *http.Request) (string, *http.Request) {
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s %s %s\r\n", req.Method, redact.RequestURI(req.URL), req.Proto)
	if req.Host != "" {
		fmt.Fprintf(&builder, "Host: %s\r\n", req.Host)
	}
	writeRedactedHeaders(&builder, req.Header)
	builder.WriteString("\r\n")

	if h.maxBodyBytes < 0 || req.Body == nil || req.Body == http.NoBody {
		return builder.String(), req
	}

	if req.GetBody != nil {
		// A fresh copy of the body: previewing it leaves req.Body untouched.
		// A GetBody failure only costs the preview, never the request.
		body, err := req.GetBody()
		if err != nil {
			return builder.String(), req
		}
		builder.WriteString(redact.Reader(body, h.maxBodyBytes))
		_ = body.Close()

		return builder.String(), req
	}

	head, err := io.ReadAll(io.LimitReader(req.Body, int64(h.maxBodyBytes)+1))
	outgoing := req.WithContext(req.Context())
	outgoing.Body = &prefixReadCloser{head: head, err: err, tail: req.Body}
	if err == nil {
		builder.WriteString(redact.Reader(bytes.NewReader(head), h.maxBodyBytes))
	}

	return builder.String(), outgoing
}

// dumpResponse renders the status line, redacted headers and a bounded,
// redacted body preview. It reads at most maxBodyBytes+1 bytes and re-wraps
// rsp.Body so the caller still reads every byte, in order, from the start. A
// read error ends the preview and is returned; the caller hits the same error
// once it has read the bytes buffered before it.
func (h httpTransport) dumpResponse(rsp *http.Response) (string, error) {
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
	writeRedactedHeaders(&builder, rsp.Header)
	builder.WriteString("\r\n")

	// A 101 body is the upgraded connection (an io.ReadWriteCloser): wrapping
	// it would break the upgrade, and reading it would wait on the peer.
	if h.maxBodyBytes < 0 || rsp.Body == nil || rsp.Body == http.NoBody ||
		rsp.StatusCode == http.StatusSwitchingProtocols {
		return builder.String(), nil
	}

	head, err := io.ReadAll(io.LimitReader(rsp.Body, int64(h.maxBodyBytes)+1))
	rsp.Body = &prefixReadCloser{head: head, err: err, tail: rsp.Body}
	if err != nil {
		return builder.String(), err
	}
	builder.WriteString(redact.Reader(bytes.NewReader(head), h.maxBodyBytes))

	return builder.String(), nil
}

// prefixReadCloser serves the head buffered for a preview, then err if
// buffering stopped on one, then the unread tail. Close closes the tail.
type prefixReadCloser struct {
	head []byte
	err  error
	tail io.ReadCloser
}

func (p *prefixReadCloser) Read(b []byte) (int, error) {
	if len(p.head) > 0 {
		n := copy(b, p.head)
		p.head = p.head[n:]

		return n, nil
	}
	if p.err != nil {
		return 0, p.err
	}

	return p.tail.Read(b)
}

func (p *prefixReadCloser) Close() error {
	return p.tail.Close()
}

// writeRedactedHeaders writes one "Name: value" line per header, sorted by
// name, with each value rendered by redact.HeaderValues (credentials masked,
// URL-valued headers redacted as URLs) and joined by ", ".
func writeRedactedHeaders(builder *strings.Builder, headers http.Header) {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		fmt.Fprintf(builder, "%s: %s\r\n", name, strings.Join(redact.HeaderValues(name, headers[name]), ", "))
	}
}
