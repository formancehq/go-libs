package audit_test

import (
	"testing"

	"github.com/formancehq/go-libs/v3/audit"
)

// TestConfigValidation tests the critical validation logic
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      audit.Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "disabled config should pass validation",
			config: audit.Config{
				Enabled: false,
				Kafka:   nil,
				NATS:    nil,
			},
			expectError: false,
		},
		{
			name: "enabled with kafka only should pass",
			config: audit.Config{
				Enabled: true,
				Kafka: &audit.KafkaConfig{
					Broker: "localhost:9092",
				},
				NATS: nil,
			},
			expectError: false,
		},
		{
			name: "enabled with nats only should pass",
			config: audit.Config{
				Enabled: true,
				Kafka:   nil,
				NATS: &audit.NATSConfig{
					URL:      "nats://localhost:4222",
					ClientID: "test",
				},
			},
			expectError: false,
		},
		{
			name: "enabled with both kafka and nats should fail",
			config: audit.Config{
				Enabled: true,
				Kafka: &audit.KafkaConfig{
					Broker: "localhost:9092",
				},
				NATS: &audit.NATSConfig{
					URL:      "nats://localhost:4222",
					ClientID: "test",
				},
			},
			expectError: true,
			errorMsg:    "cannot configure both Kafka and NATS publishers simultaneously",
		},
		{
			name: "enabled without any publisher should fail",
			config: audit.Config{
				Enabled: true,
				Kafka:   nil,
				NATS:    nil,
			},
			expectError: true,
			errorMsg:    "audit is enabled but no publisher is configured (kafka or nats)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
				} else if err.Error() != tt.errorMsg {
					t.Errorf("expected error '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
				}
			}
		})
	}
}

// TestNewClientValidation ensures NewClient enforces validation
func TestNewClientValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      audit.Config
		expectError bool
	}{
		{
			name: "should reject enabled config without publisher",
			config: audit.Config{
				Enabled: true,
				AppName: "test",
				Kafka:   nil,
				NATS:    nil,
			},
			expectError: true,
		},
		{
			name: "should reject config with both publishers",
			config: audit.Config{
				Enabled: true,
				AppName: "test",
				Kafka: &audit.KafkaConfig{
					Broker: "localhost:9092",
				},
				NATS: &audit.NATSConfig{
					URL:      "nats://localhost:4222",
					ClientID: "test",
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: We can't actually test with real publishers here without infrastructure
			// But we can test that validation is called
			_, err := audit.NewClient(tt.config, nil)

			if tt.expectError && err == nil {
				t.Errorf("expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error but got: %v", err)
			}
		})
	}
}
