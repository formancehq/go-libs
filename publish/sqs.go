package publish

import (
	"context"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-aws/sqs"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/aws/aws-sdk-go-v2/aws"
	sqsservice "github.com/aws/aws-sdk-go-v2/service/sqs"
	"go.uber.org/fx"
)

func NewSqsSubscriber(logger watermill.LoggerAdapter, config aws.Config, optFns []func(*sqsservice.Options)) (*sqs.Subscriber, error) {
	return sqs.NewSubscriber(sqs.SubscriberConfig{
		DoNotCreateQueueIfNotExists: true,
		AWSConfig:                   config,
		OptFns:                      optFns,
	}, logger)
}

func sqsModule(config aws.Config, optFns []func(*sqsservice.Options)) fx.Option {
	return fx.Options(
		fx.Provide(func(lc fx.Lifecycle, logger watermill.LoggerAdapter) (*sqs.Subscriber, error) {
			ret, err := NewSqsSubscriber(logger, config, optFns)
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
		fx.Provide(func(sqsSubscriber *sqs.Subscriber) message.Subscriber {
			return sqsSubscriber
		}),
	)
}
