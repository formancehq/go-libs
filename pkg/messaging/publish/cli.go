package publish

import (
	"time"

	"github.com/spf13/pflag"
)

const (
	// General configuration
	PublisherTopicMappingFlag = "publisher-topic-mapping"
	PublisherQueueGroupFlag   = "publisher-queue-group"
	// Circuit Breaker configuration
	PublisherCircuitBreakerEnabledFlag              = "publisher-circuit-breaker-enabled"
	PublisherCircuitBreakerOpenIntervalDurationFlag = "publisher-circuit-breaker-open-interval-duration"
	PublisherCircuitBreakerSchemaFlag               = "publisher-circuit-breaker-schema"
	PublisherCircuitBreakerListStorageLimitFlag     = "publisher-circuit-breaker-list-storage-limit"
	// Kafka configuration
	PublisherKafkaEnabledFlag            = "publisher-kafka-enabled"
	PublisherKafkaBrokerFlag             = "publisher-kafka-broker"
	PublisherKafkaSASLEnabledFlag        = "publisher-kafka-sasl-enabled"
	PublisherKafkaSASLIAMEnabledFlag     = "publisher-kafka-sasl-iam-enabled"
	PublisherKafkaSASLIAMSessionNameFlag = "publisher-kafka-sasl-session-name"
	PublisherKafkaSASLUsernameFlag       = "publisher-kafka-sasl-username"
	PublisherKafkaSASLPasswordFlag       = "publisher-kafka-sasl-password"
	PublisherKafkaSASLMechanismFlag      = "publisher-kafka-sasl-mechanism"
	PublisherKafkaSASLScramSHASizeFlag   = "publisher-kafka-sasl-scram-sha-size"
	PublisherKafkaTLSEnabledFlag         = "publisher-kafka-tls-enabled"
	// HTTP configuration
	PublisherHttpEnabledFlag = "publisher-http-enabled"
	// Nats configuration
	PublisherNatsEnabledFlag       = "publisher-nats-enabled"
	PublisherNatsClientIDFlag      = "publisher-nats-client-id"
	PublisherNatsURLFlag           = "publisher-nats-url"
	PublisherNatsMaxReconnectFlag  = "publisher-nats-max-reconnect"
	PublisherNatsReconnectWaitFlag = "publisher-nats-reconnect-wait"
	PublisherNatsAutoProvisionFlag = "publisher-nats-auto-provision"
	PublisherNatsNkeyFileFlag      = "publisher-nats-nkey-file"
	// SQS Listener configuration
	SubscriberSqsEnabledFlag          = "subscriber-sqs-enabled"
	SubscriberSqsEndpointOverrideFlag = "subscriber-sqs-endpoint-override"
	// SNS configuration
	PublisherSnsEnabledFlag          = "publisher-sns-enabled"
	PublisherSnsEndpointOverrideFlag = "publisher-sns-endpoint-override"
)

type ConfigDefault struct {
	PublisherTopicMapping []string
	PublisherQueueGroup   string
	// Circuit Breaker configuration
	PublisherCircuitBreakerEnabled              bool
	PublisherCircuitBreakerOpenIntervalDuration time.Duration
	PublisherCircuitBreakerSchema               string
	PublisherCircuitBreakerListStorageLimit     int
	// Kafka configuration
	PublisherKafkaEnabled            bool
	PublisherKafkaBroker             []string
	PublisherKafkaSASLEnabled        bool
	PublisherKafkaSASLIAMEnabled     bool
	PublisherKafkaSASLIAMSessionName string
	PublisherKafkaSASLUsername       string
	PublisherKafkaSASLPassword       string
	PublisherKafkaSASLMechanism      string
	PublisherKafkaSASLScramSHASize   int
	PublisherKafkaTLSEnabled         bool
	// HTTP configuration
	PublisherHttpEnabled bool
	// Nats configuration
	PublisherNatsEnabled       bool
	PublisherNatsClientID      string
	PublisherNatsURL           string
	PublisherNatsMaxReconnect  int
	PublisherNatsReconnectWait time.Duration
	PublisherNatsAutoProvision bool
	// SQS configuration
	SubscriberSqsEnabled          bool
	SubscriberSqsEndpointOverride string
	// SNS configuration
	PublisherSnsEnabled          bool
	PublisherSnsEndpointOverride string
}

var DefaultConfigValues = ConfigDefault{
	PublisherTopicMapping:                       []string{},
	PublisherCircuitBreakerEnabled:              false,
	PublisherCircuitBreakerOpenIntervalDuration: 5 * time.Second,
	PublisherCircuitBreakerSchema:               "public",
	PublisherCircuitBreakerListStorageLimit:     100,
	PublisherKafkaEnabled:                       false,
	PublisherKafkaBroker:                        []string{"localhost:9092"},
	PublisherKafkaSASLEnabled:                   false,
	PublisherKafkaSASLIAMEnabled:                false,
	PublisherKafkaSASLIAMSessionName:            "",
	PublisherKafkaSASLUsername:                  "",
	PublisherKafkaSASLPassword:                  "",
	PublisherKafkaSASLMechanism:                 "",
	PublisherKafkaSASLScramSHASize:              512,
	PublisherKafkaTLSEnabled:                    false,
	PublisherHttpEnabled:                        false,
	PublisherNatsEnabled:                        false,
	PublisherNatsClientID:                       "",
	PublisherNatsURL:                            "",
	PublisherNatsMaxReconnect:                   -1, // We want to reconnect forever
	PublisherNatsReconnectWait:                  2 * time.Second,
	PublisherNatsAutoProvision:                  true,
	SubscriberSqsEnabled:                        false,
	PublisherSnsEnabled:                         false,
}

func AddFlags(serviceName string, flags *pflag.FlagSet, options ...func(*ConfigDefault)) {
	values := DefaultConfigValues
	for _, option := range options {
		option(&values)
	}
	flags.StringSlice(PublisherTopicMappingFlag, values.PublisherTopicMapping, "Define mapping between internal event types and topics")

	// Circuit Breaker
	flags.Bool(PublisherCircuitBreakerEnabledFlag, values.PublisherCircuitBreakerEnabled, "Enable circuit breaker for publisher")
	flags.Duration(PublisherCircuitBreakerOpenIntervalDurationFlag, values.PublisherCircuitBreakerOpenIntervalDuration, "Circuit breaker open interval duration")
	flags.String(PublisherCircuitBreakerSchemaFlag, values.PublisherCircuitBreakerSchema, "Circuit breaker schema")
	flags.Int(PublisherCircuitBreakerListStorageLimitFlag, values.PublisherCircuitBreakerListStorageLimit, "Circuit breaker list storage limit")

	// HTTP
	flags.Bool(PublisherHttpEnabledFlag, values.PublisherHttpEnabled, "Sent write event to http endpoint")

	// KAFKA
	flags.Bool(PublisherKafkaEnabledFlag, values.PublisherKafkaEnabled, "Publish write events to kafka")
	flags.StringSlice(PublisherKafkaBrokerFlag, values.PublisherKafkaBroker, "Kafka address if kafka enabled")
	flags.Bool(PublisherKafkaSASLEnabledFlag, values.PublisherKafkaSASLEnabled, "Enable SASL authentication on kafka publisher")
	flags.Bool(PublisherKafkaSASLIAMEnabledFlag, values.PublisherKafkaSASLIAMEnabled, "Enable IAM authentication on kafka publisher")
	flags.String(PublisherKafkaSASLIAMSessionNameFlag, values.PublisherKafkaSASLIAMSessionName, "IAM session name")
	flags.String(PublisherKafkaSASLUsernameFlag, values.PublisherKafkaSASLUsername, "SASL username")
	flags.String(PublisherKafkaSASLPasswordFlag, values.PublisherKafkaSASLPassword, "SASL password")
	flags.String(PublisherKafkaSASLMechanismFlag, values.PublisherKafkaSASLMechanism, "SASL authentication mechanism")
	flags.Int(PublisherKafkaSASLScramSHASizeFlag, values.PublisherKafkaSASLScramSHASize, "SASL SCRAM SHA size")
	flags.Bool(PublisherKafkaTLSEnabledFlag, values.PublisherKafkaTLSEnabled, "Enable TLS to connect on kafka")

	// NATS
	InitNatsCLIFlags(flags, serviceName, options...)

	// SQS
	flags.Bool(SubscriberSqsEnabledFlag, values.SubscriberSqsEnabled, "Subscribe to events on SQS")
	flags.String(SubscriberSqsEndpointOverrideFlag, values.SubscriberSqsEndpointOverride, "Connect to SQS using a custom endpoint (eg. localstack)")

	// SNS
	flags.Bool(PublisherSnsEnabledFlag, values.PublisherSnsEnabled, "Publish events to SNS")
	flags.String(PublisherSnsEndpointOverrideFlag, values.PublisherSnsEndpointOverride, "Connect to SNS using a custom endpoint (eg. localstack)")
}

// Used by membership
func InitNatsCLIFlags(flags *pflag.FlagSet, serviceName string, options ...func(*ConfigDefault)) {
	values := DefaultConfigValues
	for _, option := range options {
		option(&values)
	}

	flags.String(PublisherQueueGroupFlag, serviceName, "Define queue group for consumers")
	flags.Bool(PublisherNatsEnabledFlag, values.PublisherNatsEnabled, "Publish write events to nats")
	flags.String(PublisherNatsClientIDFlag, values.PublisherNatsClientID, "Nats client ID")
	flags.Int(PublisherNatsMaxReconnectFlag, values.PublisherNatsMaxReconnect, "Nats: set the maximum number of reconnect attempts.")
	flags.Duration(PublisherNatsReconnectWaitFlag, values.PublisherNatsReconnectWait, "Nats: the wait time between reconnect attempts.")
	flags.String(PublisherNatsURLFlag, values.PublisherNatsURL, "Nats url")
	flags.Bool(PublisherNatsAutoProvisionFlag, true, "Auto create streams")
	flags.StringArray(PublisherNatsNkeyFileFlag, []string{}, "Nats: nkey file (can be used multiple times)")
}
