package observe

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type WithBodiesTracingHTTPTransport struct {
	underlying http.RoundTripper
	debug      bool
}

func (t WithBodiesTracingHTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var rawRequest []byte
	if t.debug {
		var dumpErr error
		rawRequest, dumpErr = dumpRequest(req, true)
		if dumpErr != nil {
			return nil, fmt.Errorf("dump http request: %w", dumpErr)
		}
	}

	rsp, err := t.underlying.RoundTrip(req)
	if t.debug || err != nil || (rsp != nil && rsp.StatusCode >= 400) {
		span := trace.SpanFromContext(req.Context())
		if t.debug {
			span.SetAttributes(attribute.String("raw-request", string(rawRequest)))
		} else {
			rawRequest, dumpErr := dumpRequest(req, false)
			if dumpErr != nil {
				span.SetAttributes(attribute.String("raw-request-error", dumpErr.Error()))
			} else {
				span.SetAttributes(attribute.String("raw-request", string(rawRequest)))
			}
		}
		if err != nil {
			span.SetAttributes(attribute.String("http-error", err.Error()))
		}
		if rsp != nil {
			rawResponse, err := dumpResponse(rsp, true)
			if err != nil {
				span.SetAttributes(attribute.String("raw-response-error", err.Error()))
			} else {
				span.SetAttributes(attribute.String("raw-response", string(rawResponse)))
			}
		}
	}

	return rsp, err
}

func NewRoundTripper(httpTransport http.RoundTripper, debug bool, options ...otelhttp.Option) http.RoundTripper {
	var transport = httpTransport
	transport = WithBodiesTracingHTTPTransport{
		underlying: transport,
		debug:      debug,
	}
	return otelhttp.NewTransport(transport, options...)
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
		bodyCloser := &closeOnceReadCloser{ReadCloser: body}
		cloned.Body = bodyCloser
		data, err := httputil.DumpRequestOut(cloned, true)
		closeErr := bodyCloser.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return data, nil
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

	if !includeBody || rsp.Body == nil || rsp.Body == http.NoBody {
		return httputil.DumpResponse(cloned, includeBody)
	}

	contentLength := rsp.ContentLength
	body, readErr := io.ReadAll(rsp.Body)
	closeErr := rsp.Body.Close()
	rsp.Body = newReplayReadCloser(body, readErr, closeErr)
	rsp.ContentLength = contentLength
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}

	cloned.Body = io.NopCloser(bytes.NewReader(body))
	return httputil.DumpResponse(cloned, true)
}

type closeOnceReadCloser struct {
	io.ReadCloser
	once sync.Once
	err  error
}

func (r *closeOnceReadCloser) Close() error {
	r.once.Do(func() {
		r.err = r.ReadCloser.Close()
	})
	return r.err
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
