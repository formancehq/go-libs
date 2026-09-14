package httpclient

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
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
		logger.Debug(dumpRequest(request))
	}

	rsp, err := h.underlying.RoundTrip(request)
	if err != nil {
		return nil, err
	}

	if debugEnabled {
		logger.Debug(dumpResponse(rsp))
	}

	return rsp, nil
}

var _ http.RoundTripper = &httpTransport{}

func NewDebugHTTPTransport(underlying http.RoundTripper) *httpTransport {
	return &httpTransport{
		underlying: underlying,
	}
}

// dumpRequest renders one request as an HTTP-shaped dump: the request line with
// path and query redacted, the headers with credential-bearing values masked, and
// a bounded, secret-redacted body.
//
// It never fails the request. A dump is a diagnostic, so a body that cannot be
// read is recorded in the dump rather than turned into a RoundTrip error.
func dumpRequest(req *http.Request) string {
	var builder strings.Builder
	uri := redactRequestURI(req.URL)
	if uri == "" {
		uri = "/"
	}
	fmt.Fprintf(&builder, "%s %s %s\r\n", req.Method, uri, req.Proto)
	if req.Host != "" {
		fmt.Fprintf(&builder, "Host: %s\r\n", req.Host)
	}
	writeRedactedHeaders(&builder, req.Header)
	builder.WriteString("\r\n")
	builder.WriteString(dumpRequestBody(req))

	return builder.String()
}

// dumpResponse renders one response the same way, and leaves rsp.Body readable in
// full: only the dump is truncated.
func dumpResponse(rsp *http.Response) string {
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
	builder.WriteString(dumpResponseBody(rsp))

	return builder.String()
}

// dumpRequestBody renders a bounded, redacted view of the request body while
// leaving the body the transport is about to send intact.
//
// GetBody is the clean path: it hands back an independent reader. Without it the
// body is read once, bounded, and re-wrapped so the remaining bytes still stream
// to the server.
func dumpRequestBody(req *http.Request) string {
	if req.Body == nil || req.Body == http.NoBody {
		return ""
	}

	if req.GetBody != nil {
		rc, err := req.GetBody()
		if err != nil {
			return fmt.Sprintf("[failed to dump HTTP request body: %v]", err)
		}
		defer func() { _ = rc.Close() }()

		return RedactBody(rc, DefaultMaxDumpBody)
	}

	head, err := io.ReadAll(io.LimitReader(req.Body, DefaultMaxDumpBody+1))
	req.Body = &joinReadCloser{head: head, tail: req.Body}
	if err != nil {
		return fmt.Sprintf("[failed to dump HTTP request body: %v]", err)
	}

	// Redact a copy: the bytes handed to the transport must be untouched.
	return RedactBody(bytes.NewReader(head), DefaultMaxDumpBody)
}

// dumpResponseBody renders a bounded, redacted view of the response body and
// re-wraps rsp.Body so the caller still reads every byte. That is the part a naive
// dump gets wrong: io.ReadAll into a log line both copies an unbounded payload and
// forces the whole body to be buffered before the caller sees any of it.
func dumpResponseBody(rsp *http.Response) string {
	if rsp.Body == nil || rsp.Body == http.NoBody {
		return ""
	}

	head, err := io.ReadAll(io.LimitReader(rsp.Body, DefaultMaxDumpBody+1))
	rsp.Body = &joinReadCloser{head: head, tail: rsp.Body}
	if err != nil {
		return fmt.Sprintf("[failed to dump HTTP response body: %v]", err)
	}

	// Redact a copy: the bytes handed back to the caller must be untouched.
	return RedactBody(bytes.NewReader(head), DefaultMaxDumpBody)
}

// writeRedactedHeaders writes headers as sorted "Name: value" lines with
// credential-bearing values masked, keeping the dump wire-shaped. RedactHeaders is
// the single-line equivalent for callers logging structured attributes.
func writeRedactedHeaders(builder *strings.Builder, headers http.Header) {
	for _, name := range sortedHeaderNames(headers) {
		fmt.Fprintf(builder, "%s: %s\r\n", name, redactedHeaderValue(headers, name))
	}
}

// joinReadCloser serves a buffered head then the remaining tail, closing the
// tail. It lets the dump inspect the head of a body while the caller still reads
// it whole.
type joinReadCloser struct {
	head []byte
	off  int
	tail io.ReadCloser
}

func (j *joinReadCloser) Read(p []byte) (int, error) {
	if j.off < len(j.head) {
		n := copy(p, j.head[j.off:])
		j.off += n

		return n, nil
	}

	return j.tail.Read(p)
}

func (j *joinReadCloser) Close() error { return j.tail.Close() }
