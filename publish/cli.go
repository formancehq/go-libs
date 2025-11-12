package publish

import (
	"fmt"
	"strings"
	"time"

	"github.com/IBM/sarama"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/xdg-go/scram"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v3/aws/iam"
	circuitbreaker "github.com/formancehq/go-libs/v3/publish/circuit_breaker"
	topicmapper "github.com/formancehq/go-libs/v3/publish/topic_mapper"
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

var (
	defaultConfigValues = ConfigDefault{
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
)

func AddFlags(serviceName string, flags *pflag.FlagSet, options ...func(*ConfigDefault)) {
	values := defaultConfigValues
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
	values := defaultConfigValues
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

func FXModuleFromFlags(cmd *cobra.Command, debug bool) fx.Option {
	options := make([]fx.Option, 0)

	topics, _ := cmd.Flags().GetStringSlice(PublisherTopicMappingFlag)
	queueGroup, _ := cmd.Flags().GetString(PublisherQueueGroupFlag)

	mapping := make(map[string]string)
	for _, topic := range topics {
		parts := strings.SplitN(topic, ":", 2)
		if len(parts) != 2 {
			panic(fmt.Sprintf("unable to parse topic '%s', must be two parts, separated by a colon", topic))
		}
		mapping[parts[0]] = parts[1]
	}

	options = append(options, Module(mapping))

	circuitBreakerEnabled, _ := cmd.Flags().GetBool(PublisherCircuitBreakerEnabledFlag)
	if circuitBreakerEnabled {

		scheme, _ := cmd.Flags().GetString(PublisherCircuitBreakerSchemaFlag)
		intervalDuration, _ := cmd.Flags().GetDuration(PublisherCircuitBreakerOpenIntervalDurationFlag)
		storageLimit, _ := cmd.Flags().GetInt(PublisherCircuitBreakerListStorageLimitFlag)

		options = append(options,
			circuitbreaker.Module(scheme, intervalDuration, storageLimit, debug),
			fx.Decorate(func(cb *circuitbreaker.CircuitBreaker) message.Publisher {
				return cb
			}),
		)
	} else {
		options = append(options,
			fx.Decorate(func(topicMapper *topicmapper.TopicMapperPublisherDecorator) message.Publisher {
				return topicMapper
			}),
		)
	}

	httpEnabled, _ := cmd.Flags().GetBool(PublisherHttpEnabledFlag)
	natsEnabled, _ := cmd.Flags().GetBool(PublisherNatsEnabledFlag)
	kafkaEnabled, _ := cmd.Flags().GetBool(PublisherKafkaEnabledFlag)
	sqsSubscriberEnabled, _ := cmd.Flags().GetBool(SubscriberSqsEnabledFlag)
	snsPublisherEnabled, _ := cmd.Flags().GetBool(PublisherSnsEnabledFlag)

	switch {
	case httpEnabled:
		options = append(options, httpModule())
	case natsEnabled:
		natsConnName := queueGroup
		clientId, _ := cmd.Flags().GetString(PublisherNatsClientIDFlag)
		if clientId != "" {
			natsConnName = clientId
		}
		natsUrl, _ := cmd.Flags().GetString(PublisherNatsURLFlag)
		autoProvision, _ := cmd.Flags().GetBool(PublisherNatsAutoProvisionFlag)
		maxReconnect, _ := cmd.Flags().GetInt(PublisherNatsMaxReconnectFlag)
		maxReconnectWait, _ := cmd.Flags().GetDuration(PublisherNatsReconnectWaitFlag)
		nkeyFiles, _ := cmd.Flags().GetStringArray(PublisherNatsNkeyFileFlag)

		natsOptions := []nats.Option{
			nats.Name(natsConnName),
			nats.MaxReconnects(maxReconnect),
			nats.ReconnectWait(maxReconnectWait),
		}

		for _, file := range nkeyFiles {
			option, err := nats.NkeyOptionFromSeed(file)
			if err != nil {
				panic(fmt.Sprintf("unable to parse nkey file '%s': %v", file, err))
			}
			natsOptions = append(natsOptions, option)
		}

		options = append(options, NatsModule(
			natsUrl,
			queueGroup,
			autoProvision,
			natsOptions...,
		))

	// SNS & SQS are often used in conjunction to each other, so we set them up in the same block
	// Currently it's only possible to setup a SNS publisher and a SQS subscriber
	// but if we want different combinations in the future (like SQS only) we can ensure that only the desired publisher is set up
	case sqsSubscriberEnabled, snsPublisherEnabled:
		if sqsSubscriberEnabled {
			sqsEndpointOverride, _ := cmd.Flags().GetString(SubscriberSqsEndpointOverrideFlag)

			options = append(options,
				fx.Supply(fx.Annotate(iam.LoadOptionFromCommand(cmd), fx.ResultTags(`name:"publish-sqs-enabled"`))),
				sqsModule(cmd, sqsEndpointOverride),
			)
		}
		if snsPublisherEnabled {
			snsEndpointOverride, _ := cmd.Flags().GetString(PublisherSnsEndpointOverrideFlag)

			options = append(options,
				fx.Supply(fx.Annotate(iam.LoadOptionFromCommand(cmd), fx.ResultTags(`name:"publish-sns-enabled"`))),
				snsModule(cmd, snsEndpointOverride),
			)
		}
	case kafkaEnabled:
		brokers, _ := cmd.Flags().GetStringSlice(PublisherKafkaBrokerFlag)

		options = append(options,
			kafkaModule(clientId(queueGroup), queueGroup, brokers...),
			ProvideSaramaOption(
				WithConsumerReturnErrors(),
				WithProducerReturnSuccess(),
			),
		)
		if tlsEnabled, _ := cmd.Flags().GetBool(PublisherKafkaTLSEnabledFlag); tlsEnabled {
			options = append(options, ProvideSaramaOption(WithTLS()))
		}
		if saslEnabled, _ := cmd.Flags().GetBool(PublisherKafkaSASLEnabledFlag); saslEnabled {
			mechanism, _ := cmd.Flags().GetString(PublisherKafkaSASLMechanismFlag)
			saslUsername, _ := cmd.Flags().GetString(PublisherKafkaSASLUsernameFlag)
			saslPassword, _ := cmd.Flags().GetString(PublisherKafkaSASLPasswordFlag)
			saslScramShaSize, _ := cmd.Flags().GetInt(PublisherKafkaSASLScramSHASizeFlag)

			saramaOptions := []SaramaOption{
				WithSASLEnabled(),
				WithSASLMechanism(sarama.SASLMechanism(mechanism)),
				WithSASLCredentials(saslUsername, saslPassword),
				WithSASLScramClient(func() sarama.SCRAMClient {
					var fn scram.HashGeneratorFcn
					switch saslScramShaSize {
					case 512:
						fn = SHA512
					case 256:
						fn = SHA256
					default:
						panic("sha size not handled")
					}
					return &XDGSCRAMClient{
						HashGeneratorFcn: fn,
					}
				}),
			}

			if awsEnabled, _ := cmd.Flags().GetBool(PublisherKafkaSASLIAMEnabledFlag); awsEnabled {

				region, _ := cmd.Flags().GetString(iam.AWSRegionFlag)
				roleArn, _ := cmd.Flags().GetString(iam.AWSRoleArnFlag)
				sessionName, _ := cmd.Flags().GetString(PublisherKafkaSASLIAMSessionNameFlag)

				saramaOptions = append(saramaOptions,
					WithTokenProvider(&MSKAccessTokenProvider{
						region:      region,
						roleArn:     roleArn,
						sessionName: sessionName,
					}),
				)
			}

			options = append(options, ProvideSaramaOption(saramaOptions...))
		}
	default:
		options = append(options, GoChannelModule())
	}
	return fx.Options(options...)
}
