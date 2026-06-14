package observe

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"strings"

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
	if t.debug || err != nil {
		span := trace.SpanFromContext(req.Context())
		if t.debug {
			span.SetAttributes(attribute.String("raw-request", string(rawRequest)))
		}
		if err != nil {
			span.SetAttributes(attribute.String("http-error", err.Error()))
		}
		if t.debug && rsp != nil {
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
