package sharedpublishkafka

import (
	"github.com/Shopify/sarama"
	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-kafka/v2/pkg/kafka"
	"github.com/ThreeDotsLabs/watermill/message"
	"go.uber.org/fx"
)

type SaramaOption interface {
	Apply(config *sarama.Config)
}
type SaramaOptionFn func(config *sarama.Config)

func (fn SaramaOptionFn) Apply(config *sarama.Config) {
	fn(config)
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

func WithNetSASLEnabled() SaramaOptionFn {
	return func(config *sarama.Config) {
		config.Net.SASL.Enable = true
	}
}

func WithNetSASLMechanism(mechanism sarama.SASLMechanism) SaramaOptionFn {
	return func(config *sarama.Config) {
		config.Net.SASL.Mechanism = mechanism
	}
}

func WithNetSASLScramClient(fn func() sarama.SCRAMClient) SaramaOptionFn {
	return func(config *sarama.Config) {
		config.Net.SASL.SCRAMClientGeneratorFunc = fn
	}
}

func WithNetSASLCredentials(user, pwd string) SaramaOptionFn {
	return func(config *sarama.Config) {
		config.Net.SASL.User = user
		config.Net.SASL.Password = pwd
	}
}

type ClientId string

func NewSaramaConfig(clientId ClientId, version sarama.KafkaVersion, options ...SaramaOption) *sarama.Config {

	config := sarama.NewConfig()
	config.ClientID = string(clientId)
	config.Version = version

	for _, opt := range options {
		opt.Apply(config)
	}

	return config
}

func NewKafkaPublisher(logger watermill.LoggerAdapter, config *sarama.Config, brokers ...string) (*kafka.Publisher, error) {
	publisherConfig := kafka.PublisherConfig{
		Brokers:               brokers,
		Marshaler:             kafka.DefaultMarshaler{},
		OverwriteSaramaConfig: config,
	}
	return kafka.NewPublisher(publisherConfig, logger)
}

func ProvideSaramaOption(options ...SaramaOption) fx.Option {
	fxOptions := make([]fx.Option, 0)
	for _, opt := range options {
		opt := opt
		fxOptions = append(fxOptions, fx.Provide(fx.Annotate(func() SaramaOption {
			return opt
		}, fx.ResultTags(`group:"saramaOptions"`), fx.As(new(SaramaOption)))))
	}
	return fx.Options(fxOptions...)
}

func Module(clientId ClientId, brokers ...string) fx.Option {
	return fx.Options(
		fx.Supply(clientId),
		fx.Supply(sarama.V1_0_0_0),
		fx.Provide(fx.Annotate(
			NewSaramaConfig,
			fx.ParamTags(``, ``, `group:"saramaOptions"`),
		)),
		fx.Provide(func(logger watermill.LoggerAdapter, config *sarama.Config) (*kafka.Publisher, error) {
			return NewKafkaPublisher(logger, config, brokers...)
		}),
		fx.Decorate(
			func(kafkaPublisher *kafka.Publisher) message.Publisher {
				return kafkaPublisher
			},
		),
	)
}
