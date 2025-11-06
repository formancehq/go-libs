package audit

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/ThreeDotsLabs/watermill-kafka/v3/pkg/kafka"
	wNats "github.com/ThreeDotsLabs/watermill-nats/v2/pkg/nats"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/formancehq/go-libs/v3/audit/internal"
	"github.com/formancehq/go-libs/v3/logging"
	"github.com/formancehq/go-libs/v3/publish"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/pkg/errors"
	"github.com/xdg-go/scram"
	"go.uber.org/zap"
)

// Client is the HTTP audit client
type Client struct {
	config    Config
	logger    *zap.Logger
	publisher message.Publisher
	bufPool   *sync.Pool
}

// NewClient creates a new audit client
func NewClient(cfg Config, logger *zap.Logger) (*Client, error) {
	client := &Client{
		config: cfg,
		logger: logger,
		bufPool: &sync.Pool{
			New: func() any {
				return new(bytes.Buffer)
			},
		},
	}

	// Create publisher based on config
	if cfg.Kafka != nil {
		if err := client.setupKafkaPublisher(); err != nil {
			return nil, err
		}
	} else if cfg.NATS != nil {
		if err := client.setupNATSPublisher(); err != nil {
			return nil, err
		}
	} else if cfg.Enabled {
		logger.Warn("audit is enabled but no publisher is configured (kafka or nats)")
	}

	return client, nil
}

func (c *Client) setupKafkaPublisher() error {
	options := []publish.SaramaOption{
		publish.WithSASLCredentials(
			c.config.Kafka.SASLUsername,
			c.config.Kafka.SASLPassword,
		),
	}

	if c.config.Kafka.TLSEnabled {
		options = append(options, publish.WithTLS())
	}

	if c.config.Kafka.SASLEnabled {
		options = append(options, publish.WithSASLMechanism(sarama.SASLMechanism(c.config.Kafka.SASLMechanism)))

		// Validate SHA size
		if c.config.Kafka.SASLScramSHASize != 256 && c.config.Kafka.SASLScramSHASize != 512 {
			return errors.Errorf("invalid sasl_scram_sha_size: %d (must be 256 or 512)", c.config.Kafka.SASLScramSHASize)
		}

		options = append(options,
			publish.WithSASLScramClient(func() sarama.SCRAMClient {
				var fn scram.HashGeneratorFcn
				if c.config.Kafka.SASLScramSHASize == 512 {
					fn = publish.SHA512
				} else {
					fn = publish.SHA256
				}
				return &publish.XDGSCRAMClient{
					HashGeneratorFcn: fn,
				}
			}),
		)
	}

	// Create Sarama config manually
	saramaConfig := sarama.NewConfig()
	saramaConfig.ClientID = c.config.AppName
	saramaConfig.Version = sarama.V1_0_0_0

	// Apply options
	for _, opt := range options {
		opt.Apply(saramaConfig)
	}

	var err error
	c.publisher, err = publish.NewKafkaPublisher(
		logging.NewZapLoggerAdapter(c.logger),
		saramaConfig,
		kafka.DefaultMarshaler{},
		c.config.Kafka.Broker,
	)

	if err != nil {
		c.logger.Error("failed to create kafka publisher", zap.Error(err))
		return err
	}

	return nil
}

func (c *Client) setupNATSPublisher() error {
	jetStreamConfig := wNats.JetStreamConfig{
		AutoProvision: true,
		DurablePrefix: c.config.AppName,
	}

	natsOptions := []nats.Option{
		nats.Name(c.config.NATS.ClientID),
		nats.MaxReconnects(c.config.NATS.MaxReconnects),
		nats.ReconnectWait(c.config.NATS.MaxReconnectsWait),
		nats.ClosedHandler(func(nc *nats.Conn) {
			c.logger.Error("nats connection closed unexpectedly")
		}),
	}

	publisherConfig := wNats.PublisherConfig{
		URL:               c.config.NATS.URL,
		NatsOptions:       natsOptions,
		JetStream:         jetStreamConfig,
		Marshaler:         &wNats.NATSMarshaler{},
		SubjectCalculator: wNats.DefaultSubjectCalculator,
	}

	conn, err := publish.NewNatsConn(publisherConfig)
	if err != nil {
		c.logger.Error("failed to create nats connection", zap.Error(err))
		return err
	}

	c.publisher, err = publish.NewNatsPublisherWithConn(
		conn,
		logging.NewZapLoggerAdapter(c.logger),
		publisherConfig,
	)

	if err != nil {
		c.logger.Error("failed to create nats publisher", zap.Error(err))
		return err
	}

	return nil
}

// AuditHTTPRequest audits an HTTP request/response
func (c *Client) AuditHTTPRequest(w http.ResponseWriter, r *http.Request, next http.Handler) {
	// Skip if audit is disabled
	if !c.config.Enabled {
		next.ServeHTTP(w, r)
		return
	}

	// Skip if publisher is not configured
	if c.publisher == nil {
		next.ServeHTTP(w, r)
		return
	}

	// Check if path is excluded
	for _, excludedPath := range c.config.ExcludedPaths {
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
	if c.config.MaxBodySize > 0 {
		limitedReader := io.LimitReader(r.Body, c.config.MaxBodySize)
		body, err = io.ReadAll(limitedReader)
	} else {
		body, err = io.ReadAll(r.Body)
	}

	if err != nil && !errors.Is(err, io.EOF) {
		c.logger.Error("failed to read request body", zap.Error(err))
	}

	if len(body) > 0 {
		request.Body = string(body)
		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewBuffer(body))
	}

	// Capture response
	buf := c.bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer c.bufPool.Put(buf)

	rww := internal.NewResponseWriterWrapper(w, buf)
	next.ServeHTTP(rww, r)

	response := HTTPResponse{
		StatusCode: *rww.StatusCode,
		Headers:    rww.Header(),
		Body:       rww.Body.String(),
	}

	// Publish audit event
	c.publishAuditEvent(r.Context(), request, response)
}

func (c *Client) publishAuditEvent(ctx context.Context, req HTTPRequest, resp HTTPResponse) {
	// Extract identity from JWT
	identity := ""
	if req.Header != nil {
		authHeader := req.Header.Get("Authorization")
		if authHeader != "" {
			identity = ExtractJWTIdentity(authHeader, c.logger)
		}

		// Sanitize headers
		req.Header = SanitizeHeaders(req.Header, c.config.SensitiveHeaders)
	}

	// Remove response body for sensitive endpoints
	if req.Path == "/api/auth/oauth/token" {
		resp.Body = ""
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

	// Publish message
	eventMessage := publish.EventMessage{
		Date:    time.Now().UTC(),
		App:     c.config.AppName,
		Version: "v1",
		Type:    "AUDIT",
		Payload: payload,
	}

	msg := publish.NewMessage(ctx, eventMessage)

	if err := c.publisher.Publish(c.config.TopicName, msg); err != nil {
		c.logger.Error("failed to publish audit message",
			zap.Error(err),
			zap.String("method", req.Method),
			zap.String("path", req.Path),
			zap.Int("status", resp.StatusCode),
		)
	}
}

// Close closes the audit client and publisher
func (c *Client) Close() error {
	if c.publisher != nil {
		return c.publisher.Close()
	}
	return nil
}

// HTTPMiddleware returns an HTTP middleware that audits all requests
func HTTPMiddleware(client *Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			client.AuditHTTPRequest(w, r, next)
		})
	}
}
