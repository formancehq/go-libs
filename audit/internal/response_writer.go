package internal

import (
	"bytes"
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
