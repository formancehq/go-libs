package audit

import (
	"errors"
	"time"
)

// Config holds the HTTP audit configuration
type Config struct {
	// Core settings
	Enabled   bool   `json:"enabled"`
	AppName   string `json:"app_name"`
	TopicName string `json:"topic_name"`

	// Body capture
	MaxBodySize int64 `json:"max_body_size"`

	// Filtering
	ExcludedPaths []string `json:"excluded_paths"`

	// Security
	SensitiveHeaders      []string `json:"sensitive_headers"`
	SensitiveResponsePaths []string `json:"sensitive_response_paths"` // Paths where response body should be redacted

	// DisableIdentityExtraction disables JWT identity extraction for audit logs.
	// Set to true if you don't want to parse JWTs or if you have security concerns.
	DisableIdentityExtraction bool `json:"disable_identity_extraction"`

	// Publisher (Kafka or NATS, but not both)
	Kafka *KafkaConfig `json:"kafka,omitempty"`
	NATS  *NATSConfig  `json:"nats,omitempty"`
}

type KafkaConfig struct {
	Broker           string `json:"broker"`
	TLSEnabled       bool   `json:"tls_enabled"`
	SASLEnabled      bool   `json:"sasl_enabled"`
	SASLUsername     string `json:"sasl_username"`
	SASLPassword     string `json:"sasl_password"`
	SASLMechanism    string `json:"sasl_mechanism"`
	SASLScramSHASize int    `json:"sasl_scram_sha_size"` // 256 or 512
}

type NATSConfig struct {
	URL               string        `json:"url"`
	ClientID          string        `json:"client_id"`
	MaxReconnects     int           `json:"max_reconnects"`
	MaxReconnectsWait time.Duration `json:"max_reconnects_wait"`
}

// DefaultConfig returns sensible defaults
func DefaultConfig(appName string) Config {
	return Config{
		Enabled:     true,
		AppName:     appName,
		TopicName:   appName + "-audit",
		MaxBodySize: 1024 * 1024, // 1MB
		SensitiveHeaders: []string{
			"Authorization",
			"Cookie",
			"Set-Cookie",
			"X-API-Key",
			"X-Auth-Token",
			"Proxy-Authorization",
		},
	}
}

// Validate checks the configuration for common errors
func (c Config) Validate() error {
	// If audit is disabled, no further validation needed
	if !c.Enabled {
		return nil
	}

	// Check for mutual exclusivity: Kafka and NATS cannot both be configured
	if c.Kafka != nil && c.NATS != nil {
		return errors.New("cannot configure both Kafka and NATS publishers simultaneously")
	}

	// At least one publisher must be configured when audit is enabled
	if c.Kafka == nil && c.NATS == nil {
		return errors.New("audit is enabled but no publisher is configured (kafka or nats)")
	}

	return nil
}
