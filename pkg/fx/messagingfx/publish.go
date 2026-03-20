package messagingfx

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/IBM/sarama"
	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-aws/sns"
	"github.com/ThreeDotsLabs/watermill-aws/sqs"
	wHttp "github.com/ThreeDotsLabs/watermill-http/v2/pkg/http"
	"github.com/ThreeDotsLabs/watermill-kafka/v3/pkg/kafka"
	wNats "github.com/ThreeDotsLabs/watermill-nats/v2/pkg/nats"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	snsservice "github.com/aws/aws-sdk-go-v2/service/sns"
	sqsservice "github.com/aws/aws-sdk-go-v2/service/sqs"
	transport "github.com/aws/smithy-go/endpoints"
	"github.com/nats-io/nats.go"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/uptrace/bun"
	"github.com/xdg-go/scram"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v5/pkg/cloud/aws/iam"
	"github.com/formancehq/go-libs/v5/pkg/messaging/publish"
	circuitbreaker "github.com/formancehq/go-libs/v5/pkg/messaging/publish/circuit"
	circuitstorage "github.com/formancehq/go-libs/v5/pkg/messaging/publish/circuit/storage"
	topicmapper "github.com/formancehq/go-libs/v5/pkg/messaging/publish/topicmap"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/service"
	bunconnect "github.com/formancehq/go-libs/v5/pkg/storage/bun/connect"
	bundebug "github.com/formancehq/go-libs/v5/pkg/storage/bun/debug"
)

func defaultLoggingModule() fx.Option {
	return fx.Provide(func(logger logging.Logger) watermill.LoggerAdapter {
		return publish.NewWatermillLoggerAdapter(logger, false)
	})
}

func GoChannelModule() fx.Option {
	return fx.Options(
		fx.Provide(publish.NewGoChannel),
		fx.Provide(func(ch *gochannel.GoChannel) message.Subscriber {
			return ch
		}),
		fx.Provide(func(ch *gochannel.GoChannel) message.Publisher {
			return ch
		}),
		fx.Invoke(func(lc fx.Lifecycle, channel *gochannel.GoChannel) {
			lc.Append(fx.Hook{
				OnStop: func(ctx context.Context) error {
					return channel.Close()
				},
			})
		}),
	)
}

func Module(topics map[string]string) fx.Option {
	options := fx.Options(
		defaultLoggingModule(),
		fx.Supply(message.RouterConfig{}),
		fx.Provide(message.NewRouter),
		fx.Invoke(func(router *message.Router, lc fx.Lifecycle) error {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					go func() {
						if err := router.Run(context.Background()); err != nil {
							panic(err)
						}
					}()
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-router.Running():
					}
					return nil
				},
				OnStop: func(ctx context.Context) error {
					logging.FromContext(ctx).Infof("Stopping router...")
					if err := router.Close(); err != nil {
						return errors.Wrap(err, "stopping router")
					}
					logging.FromContext(ctx).Infof("Router stopped...")

					return nil
				},
			})
			return nil
		}),
		fx.Provide(func(publisher message.Publisher) *topicmapper.TopicMapperPublisherDecorator {
			return topicmapper.NewPublisherDecorator(publisher, topics)
		}),
	)
	return options
}

func ProvideSaramaOption(options ...publish.SaramaOption) fx.Option {
	fxOptions := make([]fx.Option, 0)
	for _, opt := range options {
		opt := opt
		fxOptions = append(fxOptions, fx.Provide(fx.Annotate(func() publish.SaramaOption {
			return opt
		}, fx.ResultTags(`group:"saramaOptions"`), fx.As(new(publish.SaramaOption)))))
	}
	return fx.Options(fxOptions...)
}

func KafkaModule(clientId string, consumerGroup string, brokers ...string) fx.Option {
	return fx.Options(
		fx.Supply(publish.ClientID(clientId)),
		fx.Supply(sarama.V1_0_0_0),
		fx.Supply(fx.Annotate(kafka.DefaultMarshaler{}, fx.As(new(kafka.Marshaler)))),
		fx.Supply(fx.Annotate(kafka.DefaultMarshaler{}, fx.As(new(kafka.Unmarshaler)))),
		fx.Provide(fx.Annotate(publish.NewSaramaConfig, fx.ParamTags(``, ``, `group:"saramaOptions"`))),
		fx.Provide(func(lc fx.Lifecycle, logger watermill.LoggerAdapter, marshaller kafka.Marshaler, config *sarama.Config) (*kafka.Publisher, error) {
			ret, err := publish.NewKafkaPublisher(logger, config, marshaller, brokers...)
			if err != nil {
				return nil, err
			}
			lc.Append(fx.Hook{
				OnStop: func(ctx context.Context) error {
					return ret.Close()
				},
			})
			return ret, nil
		}),
		fx.Provide(func(lc fx.Lifecycle, logger watermill.LoggerAdapter, unmarshaler kafka.Unmarshaler, config *sarama.Config) (*kafka.Subscriber, error) {
			ret, err := publish.NewKafkaSubscriber(logger, config, unmarshaler, consumerGroup, brokers...)
			if err != nil {
				return nil, err
			}
			lc.Append(fx.Hook{
				OnStop: func(ctx context.Context) error {
					return ret.Close()
				},
			})
			return ret, nil
		}),
		fx.Provide(func(kafkaPublisher *kafka.Publisher) message.Publisher {
			return kafkaPublisher
		}),
		fx.Provide(func(kafkaSubscriber *kafka.Subscriber) message.Subscriber {
			return kafkaSubscriber
		}),
	)
}

func NatsModule(url, group string, autoProvision bool, natsOptions ...nats.Option) fx.Option {
	jetStreamConfig := wNats.JetStreamConfig{
		AutoProvision:    autoProvision,
		SubscribeOptions: []nats.SubOpt{nats.ManualAck()},
	}
	return fx.Options(
		fx.Provide(publish.NewNatsConn),
		fx.Invoke(func(lc fx.Lifecycle, conn *nats.Conn) {
			lc.Append(fx.Hook{
				OnStop: func(ctx context.Context) error {
					logging.FromContext(ctx).Infof("stopping nats connection")
					conn.Close()
					return nil
				},
			})
		}),
		fx.Provide(func(logger logging.Logger, shutdowner fx.Shutdowner) publish.NATSCallbacks {
			return publish.NewNatsDefaultCallbacks(logger, func() error {
				return shutdowner.Shutdown()
			})
		}),
		fx.Provide(publish.NewNatsPublisherWithConn),
		fx.Provide(publish.NewNatsSubscriberWithConn),
		fx.Provide(func(natsCallbacks publish.NATSCallbacks) wNats.PublisherConfig {
			natsOptions = publish.AppendNatsCallBacks(natsOptions, natsCallbacks)
			return wNats.PublisherConfig{
				NatsOptions:       natsOptions,
				URL:               url,
				Marshaler:         &wNats.NATSMarshaler{},
				JetStream:         jetStreamConfig,
				SubjectCalculator: wNats.DefaultSubjectCalculator,
			}
		}),
		fx.Provide(func(natsCallbacks publish.NATSCallbacks) wNats.SubscriberConfig {
			natsOptions = publish.AppendNatsCallBacks(natsOptions, natsCallbacks)
			return wNats.SubscriberConfig{
				NatsOptions:       natsOptions,
				Unmarshaler:       &wNats.NATSMarshaler{},
				URL:               url,
				QueueGroupPrefix:  group,
				JetStream:         jetStreamConfig,
				SubjectCalculator: wNats.DefaultSubjectCalculator,
				SubscribersCount:  100,
				NakDelay:          wNats.NewStaticDelay(time.Second),
			}
		}),
		fx.Provide(func(publisher *wNats.Publisher) message.Publisher {
			return publisher
		}),
		fx.Provide(func(subscriber *wNats.Subscriber, lc fx.Lifecycle) message.Subscriber {
			lc.Append(fx.Hook{
				OnStop: func(ctx context.Context) error {
					return subscriber.Close()
				},
			})
			return subscriber
		}),
	)
}

func HTTPModule() fx.Option {
	return fx.Options(
		fx.Provide(publish.NewHTTPPublisher),
		fx.Provide(publish.NewHTTPPublisherConfig),
		fx.Provide(publish.DefaultHTTPMarshalMessageFunc),
		fx.Supply(http.DefaultClient),
		fx.Provide(func(p *wHttp.Publisher) message.Publisher {
			return p
		}),
	)
}

func SNSModule(cmd *cobra.Command, snsEndpointOverride string) fx.Option {
	return fx.Options(
		fx.Provide(
			fx.Annotate(func(optFn func(*config.LoadOptions) error) []func(*config.LoadOptions) error {
				loadOptions := []func(*config.LoadOptions) error{optFn}
				if snsEndpointOverride != "" {
					loadOptions = append(loadOptions, config.WithCredentialsProvider(
						credentials.NewStaticCredentialsProvider("dummy", "dummy", "dummy"),
					))
				}
				return loadOptions
			}, fx.ParamTags(`name:"publish-sns-enabled"`), fx.ResultTags(`name:"publish-sns-load-opts"`)),
		),
		fx.Provide(
			fx.Annotate(func(loadOpts []func(*config.LoadOptions) error) (aws.Config, error) {
				cfg, err := config.LoadDefaultConfig(cmd.Context(), loadOpts...)
				if err != nil {
					return cfg, fmt.Errorf("unable to load aws config %w", err)
				}
				return cfg, nil
			}, fx.ParamTags(`name:"publish-sns-load-opts"`), fx.ResultTags(`name:"publish-sns-cfg"`, ``)),
		),
		fx.Provide(
			fx.Annotate(func() ([]func(*snsservice.Options), error) {
				snsOpts := []func(*snsservice.Options){}
				if snsEndpointOverride == "" {
					return snsOpts, nil
				}

				snsUrl, err := url.Parse(snsEndpointOverride)
				if err != nil {
					return snsOpts, fmt.Errorf("unable to parse sns url %q", snsEndpointOverride)
				}
				snsOpts = append(snsOpts, snsservice.WithEndpointResolverV2(sns.OverrideEndpointResolver{
					Endpoint: transport.Endpoint{
						URI: *snsUrl,
					},
				}))
				return snsOpts, nil
			}, fx.ResultTags(`name:"publish-sns-opts"`, ``)),
		),
		fx.Provide(
			fx.Annotate(func(lc fx.Lifecycle, logger watermill.LoggerAdapter, cfg aws.Config, optFns []func(*snsservice.Options)) (*sns.Publisher, error) {
				ret, err := publish.NewSnsPublisher(cmd.Context(), logger, cfg, optFns, service.IsDebug(cmd))
				if err != nil {
					return nil, err
				}
				lc.Append(fx.Hook{
					OnStop: func(ctx context.Context) error {
						return ret.Close()
					},
				})
				return ret, nil
			}, fx.ParamTags(``, ``, `name:"publish-sns-cfg"`, `name:"publish-sns-opts"`)),
		),
		fx.Provide(func(snsPublisher *sns.Publisher) message.Publisher {
			return snsPublisher
		}),
	)
}

func SQSModule(cmd *cobra.Command, sqsEndpointOverride string) fx.Option {
	return fx.Options(
		fx.Provide(
			fx.Annotate(func(optFn func(*config.LoadOptions) error) []func(*config.LoadOptions) error {
				loadOptions := []func(*config.LoadOptions) error{optFn}
				if sqsEndpointOverride != "" {
					loadOptions = append(loadOptions, config.WithCredentialsProvider(
						credentials.NewStaticCredentialsProvider("dummy", "dummy", "dummy"),
					))
				}
				return loadOptions
			}, fx.ParamTags(`name:"publish-sqs-enabled"`), fx.ResultTags(`name:"publish-subscriber-sqs-load-opts"`)),
		),
		fx.Provide(
			fx.Annotate(func(loadOpts []func(*config.LoadOptions) error) (aws.Config, error) {
				cfg, err := config.LoadDefaultConfig(cmd.Context(), loadOpts...)
				if err != nil {
					return cfg, fmt.Errorf("unable to load aws config %w", err)
				}
				return cfg, nil
			}, fx.ParamTags(`name:"publish-subscriber-sqs-load-opts"`), fx.ResultTags(`name:"publish-subscriber-sqs-cfg"`, ``)),
		),
		fx.Provide(
			fx.Annotate(func() ([]func(*sqsservice.Options), error) {
				sqsOpts := []func(*sqsservice.Options){}
				if sqsEndpointOverride == "" {
					return sqsOpts, nil
				}

				sqsUrl, err := url.Parse(sqsEndpointOverride)
				if err != nil {
					return sqsOpts, fmt.Errorf("unable to parse sqs url %q", sqsEndpointOverride)
				}
				sqsOpts = append(sqsOpts, sqsservice.WithEndpointResolverV2(sqs.OverrideEndpointResolver{
					Endpoint: transport.Endpoint{
						URI: *sqsUrl,
					},
				}))
				return sqsOpts, nil
			}, fx.ResultTags(`name:"publish-subscriber-sqs-opts"`, ``)),
		),
		fx.Provide(
			fx.Annotate(func(lc fx.Lifecycle, logger watermill.LoggerAdapter, cfg aws.Config, optFns []func(*sqsservice.Options)) (*sqs.Subscriber, error) {
				ret, err := publish.NewSqsSubscriber(logger, cfg, optFns, service.IsDebug(cmd))
				if err != nil {
					return nil, err
				}
				lc.Append(fx.Hook{
					OnStop: func(ctx context.Context) error {
						return ret.Close()
					},
				})
				return ret, nil
			}, fx.ParamTags(``, ``, `name:"publish-subscriber-sqs-cfg"`, `name:"publish-subscriber-sqs-opts"`)),
		),
		fx.Provide(func(sqsSubscriber *sqs.Subscriber) message.Subscriber {
			return sqsSubscriber
		}),
	)
}

func CircuitBreakerStorageModule(schema string, storageLimit int, debug bool) fx.Option {
	return fx.Provide(func(connectionOptions *bunconnect.ConnectionOptions, lc fx.Lifecycle) (circuitstorage.Store, error) {
		hooks := make([]bun.QueryHook, 0)
		if debug {
			hooks = append(hooks, bundebug.NewQueryHook())
		}

		db, err := bunconnect.OpenDBWithSchema(context.Background(), *connectionOptions, schema, hooks...)
		if err != nil {
			return nil, err
		}

		lc.Append(fx.Hook{
			OnStart: func(ctx context.Context) error {
				return circuitstorage.Migrate(ctx, schema, db)
			},
		})

		return circuitstorage.New(schema, db, storageLimit), nil
	})
}

func CircuitBreakerModule(schema string, openIntervalDuration time.Duration, storageLimit int, debug bool) fx.Option {
	return fx.Options(
		fx.Provide(func(
			logger logging.Logger,
			topicMapper *topicmapper.TopicMapperPublisherDecorator,
			store circuitstorage.Store,
			lc fx.Lifecycle,
		) *circuitbreaker.CircuitBreaker {
			cb := circuitbreaker.NewCircuitBreaker(logger, topicMapper, store, openIntervalDuration)

			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					go cb.Loop(context.WithoutCancel(ctx))
					return nil
				},
				OnStop: func(ctx context.Context) error {
					return cb.Close()
				},
			})
			return cb
		}),
		CircuitBreakerStorageModule(schema, storageLimit, debug),
	)
}

func PublishModuleFromFlags(cmd *cobra.Command, debug bool) fx.Option {
	options := make([]fx.Option, 0)

	topics, _ := cmd.Flags().GetStringSlice(publish.PublisherTopicMappingFlag)
	queueGroup, _ := cmd.Flags().GetString(publish.PublisherQueueGroupFlag)

	mapping := make(map[string]string)
	for _, topic := range topics {
		parts := strings.SplitN(topic, ":", 2)
		if len(parts) != 2 {
			panic(fmt.Sprintf("unable to parse topic '%s', must be two parts, separated by a colon", topic))
		}
		mapping[parts[0]] = parts[1]
	}

	options = append(options, Module(mapping))

	circuitBreakerEnabled, _ := cmd.Flags().GetBool(publish.PublisherCircuitBreakerEnabledFlag)
	if circuitBreakerEnabled {
		scheme, _ := cmd.Flags().GetString(publish.PublisherCircuitBreakerSchemaFlag)
		intervalDuration, _ := cmd.Flags().GetDuration(publish.PublisherCircuitBreakerOpenIntervalDurationFlag)
		storageLimit, _ := cmd.Flags().GetInt(publish.PublisherCircuitBreakerListStorageLimitFlag)

		options = append(options,
			CircuitBreakerModule(scheme, intervalDuration, storageLimit, debug),
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

	httpEnabled, _ := cmd.Flags().GetBool(publish.PublisherHttpEnabledFlag)
	natsEnabled, _ := cmd.Flags().GetBool(publish.PublisherNatsEnabledFlag)
	kafkaEnabled, _ := cmd.Flags().GetBool(publish.PublisherKafkaEnabledFlag)
	sqsSubscriberEnabled, _ := cmd.Flags().GetBool(publish.SubscriberSqsEnabledFlag)
	snsPublisherEnabled, _ := cmd.Flags().GetBool(publish.PublisherSnsEnabledFlag)

	switch {
	case httpEnabled:
		options = append(options, HTTPModule())
	case natsEnabled:
		natsConnName := queueGroup
		clientId, _ := cmd.Flags().GetString(publish.PublisherNatsClientIDFlag)
		if clientId != "" {
			natsConnName = clientId
		}
		natsUrl, _ := cmd.Flags().GetString(publish.PublisherNatsURLFlag)
		autoProvision, _ := cmd.Flags().GetBool(publish.PublisherNatsAutoProvisionFlag)
		maxReconnect, _ := cmd.Flags().GetInt(publish.PublisherNatsMaxReconnectFlag)
		maxReconnectWait, _ := cmd.Flags().GetDuration(publish.PublisherNatsReconnectWaitFlag)
		nkeyFiles, _ := cmd.Flags().GetStringArray(publish.PublisherNatsNkeyFileFlag)

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

	case sqsSubscriberEnabled, snsPublisherEnabled:
		if sqsSubscriberEnabled {
			sqsEndpointOverride, _ := cmd.Flags().GetString(publish.SubscriberSqsEndpointOverrideFlag)

			options = append(options,
				fx.Supply(fx.Annotate(iam.LoadOptionFromFlags(cmd.Flags()), fx.ResultTags(`name:"publish-sqs-enabled"`))),
				SQSModule(cmd, sqsEndpointOverride),
			)
		}
		if snsPublisherEnabled {
			snsEndpointOverride, _ := cmd.Flags().GetString(publish.PublisherSnsEndpointOverrideFlag)

			options = append(options,
				fx.Supply(fx.Annotate(iam.LoadOptionFromFlags(cmd.Flags()), fx.ResultTags(`name:"publish-sns-enabled"`))),
				SNSModule(cmd, snsEndpointOverride),
			)
		}
	case kafkaEnabled:
		brokers, _ := cmd.Flags().GetStringSlice(publish.PublisherKafkaBrokerFlag)

		options = append(options,
			KafkaModule(queueGroup, queueGroup, brokers...),
			ProvideSaramaOption(
				publish.WithConsumerReturnErrors(),
				publish.WithProducerReturnSuccess(),
			),
		)
		if tlsEnabled, _ := cmd.Flags().GetBool(publish.PublisherKafkaTLSEnabledFlag); tlsEnabled {
			options = append(options, ProvideSaramaOption(publish.WithTLS()))
		}
		if saslEnabled, _ := cmd.Flags().GetBool(publish.PublisherKafkaSASLEnabledFlag); saslEnabled {
			mechanism, _ := cmd.Flags().GetString(publish.PublisherKafkaSASLMechanismFlag)
			saslUsername, _ := cmd.Flags().GetString(publish.PublisherKafkaSASLUsernameFlag)
			saslPassword, _ := cmd.Flags().GetString(publish.PublisherKafkaSASLPasswordFlag)
			saslScramShaSize, _ := cmd.Flags().GetInt(publish.PublisherKafkaSASLScramSHASizeFlag)

			saramaOptions := []publish.SaramaOption{
				publish.WithSASLEnabled(),
				publish.WithSASLMechanism(sarama.SASLMechanism(mechanism)),
				publish.WithSASLCredentials(saslUsername, saslPassword),
				publish.WithSASLScramClient(func() sarama.SCRAMClient {
					var fn scram.HashGeneratorFcn
					switch saslScramShaSize {
					case 512:
						fn = publish.SHA512
					case 256:
						fn = publish.SHA256
					default:
						panic("sha size not handled")
					}
					return &publish.XDGSCRAMClient{
						HashGeneratorFcn: fn,
					}
				}),
			}

			if awsEnabled, _ := cmd.Flags().GetBool(publish.PublisherKafkaSASLIAMEnabledFlag); awsEnabled {
				region, _ := cmd.Flags().GetString(iam.AWSRegionFlag)
				roleArn, _ := cmd.Flags().GetString(iam.AWSRoleArnFlag)
				sessionName, _ := cmd.Flags().GetString(publish.PublisherKafkaSASLIAMSessionNameFlag)

				saramaOptions = append(saramaOptions,
					publish.WithTokenProvider(&publish.MSKAccessTokenProvider{
						Region:      region,
						RoleArn:     roleArn,
						SessionName: sessionName,
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
