package publish

import (
	"crypto/tls"

	"github.com/IBM/sarama"
	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-kafka/v3/pkg/kafka"
)

type SaramaOption interface {
	Apply(config *sarama.Config)
}
type SaramaOptionFn func(config *sarama.Config)

func (fn SaramaOptionFn) Apply(config *sarama.Config) {
	fn(config)
}

func WithConsumerOffsetsInitial(v int64) SaramaOptionFn {
	return func(config *sarama.Config) {
		config.Consumer.Offsets.Initial = v
	}
}

func WithConsumerReturnErrors() SaramaOptionFn {
	return func(config *sarama.Config) {
		config.Consumer.Return.Errors = true
	}
}

func WithProducerReturnSuccess() SaramaOptionFn {
	return func(config *sarama.Config) {
		config.Producer.Return.Successes = true
	}
}

func WithSASLEnabled() SaramaOptionFn {
	return func(config *sarama.Config) {
		config.Net.SASL.Enable = true
	}
}

func WithSASLMechanism(mechanism sarama.SASLMechanism) SaramaOptionFn {
	return func(config *sarama.Config) {
		config.Net.SASL.Mechanism = mechanism
	}
}

func WithSASLScramClient(fn func() sarama.SCRAMClient) SaramaOptionFn {
	return func(config *sarama.Config) {
		config.Net.SASL.SCRAMClientGeneratorFunc = fn
	}
}

func WithSASLCredentials(user, pwd string) SaramaOptionFn {
	return func(config *sarama.Config) {
		config.Net.SASL.User = user
		config.Net.SASL.Password = pwd
	}
}

func WithTokenProvider(provider sarama.AccessTokenProvider) SaramaOptionFn {
	return func(config *sarama.Config) {
		config.Net.SASL.TokenProvider = provider
	}
}

func WithTLS() SaramaOptionFn {
	return func(config *sarama.Config) {
		config.Net.TLS = struct {
			Enable bool
			Config *tls.Config
		}{
			Enable: true,
			Config: &tls.Config{},
		}
	}
}

type ClientID string

func NewSaramaConfig(clientId ClientID, version sarama.KafkaVersion, options ...SaramaOption) *sarama.Config {
	config := sarama.NewConfig()
	config.ClientID = string(clientId)
	config.Version = version

	for _, opt := range options {
		opt.Apply(config)
	}

	return config
}

func NewKafkaPublisher(logger watermill.LoggerAdapter, config *sarama.Config, marshaller kafka.Marshaler, brokers ...string) (*kafka.Publisher, error) {
	return kafka.NewPublisher(kafka.PublisherConfig{
		Brokers:               brokers,
		Marshaler:             marshaller,
		OverwriteSaramaConfig: config,
		OTELEnabled:           true,
	}, logger)
}

func NewKafkaSubscriber(logger watermill.LoggerAdapter, config *sarama.Config,
	unmarshaler kafka.Unmarshaler, consumerGroup string, brokers ...string) (*kafka.Subscriber, error) {
	return kafka.NewSubscriber(kafka.SubscriberConfig{
		Brokers:               brokers,
		OverwriteSaramaConfig: config,
		Unmarshaler:           unmarshaler,
		OTELEnabled:           true,
		ConsumerGroup:         consumerGroup,
	}, logger)
}
