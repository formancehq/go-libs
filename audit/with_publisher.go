package audit

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/formancehq/go-libs/v3/logging"
	"github.com/formancehq/go-libs/v3/publish"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// PublisherClient wraps an existing message.Publisher to send audit logs
// This avoids creating a separate Kafka/NATS connection and reuses the application's publisher
type PublisherClient struct {
	publisher        message.Publisher
	topic            string
	appName          string
	maxBodySize      int64
	excludedPaths    []string
	sensitiveHeaders []string
	logger           logging.Logger
	bufPool          *sync.Pool
}

// NewClientWithPublisher creates an audit client using an existing publisher
// This is the recommended way to use audit in Formance services, as it reuses
// the existing publisher infrastructure (NATS, Kafka, etc.)
func NewClientWithPublisher(
	publisher message.Publisher,
	topic string,
	appName string,
	maxBodySize int64,
	excludedPaths []string,
	sensitiveHeaders []string,
	logger logging.Logger,
) *PublisherClient {
	return &PublisherClient{
		publisher:        publisher,
		topic:            topic,
		appName:          appName,
		maxBodySize:      maxBodySize,
		excludedPaths:    excludedPaths,
		sensitiveHeaders: sensitiveHeaders,
		logger:           logger,
		bufPool: &sync.Pool{
			New: func() any {
				return new(bytes.Buffer)
			},
		},
	}
}

// AuditHTTPRequest audits an HTTP request/response
func (c *PublisherClient) AuditHTTPRequest(w http.ResponseWriter, r *http.Request, next http.Handler) {
	// Check if path is excluded
	for _, excludedPath := range c.excludedPaths {
		if r.URL.Path == excludedPath {
			next.ServeHTTP(w, r)
			return
		}
	}

	// Capture request
	request := HTTPRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Host:   r.Host,
		Header: r.Header,
		Body:   "",
	}

	// Read body with size limit
	var body []byte
	var err error
	if c.maxBodySize > 0 {
		limitedReader := io.LimitReader(r.Body, c.maxBodySize)
		body, err = io.ReadAll(limitedReader)
	} else {
		body, err = io.ReadAll(r.Body)
	}

	if err != nil && err != io.EOF {
		c.logger.Errorf("failed to read request body: %v", err)
	}

	if len(body) > 0 {
		request.Body = string(body)
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewBuffer(body))
	}

	// Capture response
	buf := c.bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer c.bufPool.Put(buf)

	rww := NewResponseWriterWrapper(w, buf)
	next.ServeHTTP(rww, r)

	response := HTTPResponse{
		StatusCode: *rww.StatusCode,
		Headers:    rww.Header(),
		Body:       rww.Body.String(),
	}

	// Publish audit event
	c.publishAuditEvent(r.Context(), request, response)
}

func (c *PublisherClient) publishAuditEvent(ctx context.Context, req HTTPRequest, resp HTTPResponse) {
	// Extract identity from context (set by auth middleware)
	// Note: ExtractIdentity needs a *zap.Logger, using noop since it's only for debug
	identity := ExtractIdentity(ctx, zap.NewNop())

	// Sanitize headers
	if req.Header != nil {
		req.Header = SanitizeHeaders(req.Header, c.sensitiveHeaders)
	}
	if resp.Headers != nil {
		resp.Headers = SanitizeHeaders(resp.Headers, c.sensitiveHeaders)
	}

	// Create payload
	payload := struct {
		ID       string       `json:"id"`
		Identity string       `json:"identity"`
		Request  HTTPRequest  `json:"request"`
		Response HTTPResponse `json:"response"`
	}{
		ID:       uuid.New().String(),
		Identity: identity,
		Request:  req,
		Response: resp,
	}

	// Create event message (using same format as other service events)
	eventMessage := publish.EventMessage{
		Date:    time.Now().UTC(),
		App:     c.appName,
		Version: "v1",
		Type:    "AUDIT",
		Payload: payload,
	}

	msg := publish.NewMessage(ctx, eventMessage)

	// Publish to audit topic
	if err := c.publisher.Publish(c.topic, msg); err != nil {
		c.logger.Errorf("failed to publish audit message: %v (method=%s, path=%s, status=%d)",
			err, req.Method, req.Path, resp.StatusCode)
	}
}

// Close is a no-op since we don't own the publisher
// The publisher will be closed by the application's lifecycle management
func (c *PublisherClient) Close() error {
	return nil
}

// ResponseWriterWrapper wraps http.ResponseWriter to capture response
type ResponseWriterWrapper struct {
	http.ResponseWriter
	Body       *bytes.Buffer
	StatusCode *int
}

// NewResponseWriterWrapper creates a new ResponseWriterWrapper
func NewResponseWriterWrapper(w http.ResponseWriter, buf *bytes.Buffer) *ResponseWriterWrapper {
	defaultStatus := http.StatusOK
	return &ResponseWriterWrapper{
		ResponseWriter: w,
		Body:           buf,
		StatusCode:     &defaultStatus,
	}
}

// Write writes the data to the connection and captures it
func (w *ResponseWriterWrapper) Write(b []byte) (int, error) {
	w.Body.Write(b)
	return w.ResponseWriter.Write(b)
}

// WriteHeader sends an HTTP response header with the provided status code
func (w *ResponseWriterWrapper) WriteHeader(statusCode int) {
	*w.StatusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// HTTPMiddlewareWithPublisher returns an HTTP middleware that audits all requests
// using the provided PublisherClient
func HTTPMiddlewareWithPublisher(client *PublisherClient) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			client.AuditHTTPRequest(w, r, next)
		})
	}
}
