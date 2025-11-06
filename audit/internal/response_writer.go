package internal

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
)

// ResponseWriterWrapper captures the response body and status code for audit logging
type ResponseWriterWrapper struct {
	http.ResponseWriter
	Body       *bytes.Buffer
	StatusCode *int
}

// NewResponseWriterWrapper creates a wrapper for http.ResponseWriter
func NewResponseWriterWrapper(w http.ResponseWriter, buf *bytes.Buffer) *ResponseWriterWrapper {
	statusCode := 200
	return &ResponseWriterWrapper{
		ResponseWriter: w,
		Body:           buf,
		StatusCode:     &statusCode,
	}
}

func (rww *ResponseWriterWrapper) Write(buf []byte) (int, error) {
	rww.Body.Write(buf)
	return rww.ResponseWriter.Write(buf)
}

func (rww *ResponseWriterWrapper) Header() http.Header {
	return rww.ResponseWriter.Header()
}

func (rww *ResponseWriterWrapper) WriteHeader(statusCode int) {
	*rww.StatusCode = statusCode
	rww.ResponseWriter.WriteHeader(statusCode)
}

// Flush implements http.Flusher interface (for streaming responses)
func (rww *ResponseWriterWrapper) Flush() {
	if flusher, ok := rww.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack implements http.Hijacker interface (for WebSockets)
func (rww *ResponseWriterWrapper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := rww.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Push implements http.Pusher interface (for HTTP/2 server push)
func (rww *ResponseWriterWrapper) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := rww.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}
