package publish

import (
	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-aws/sqs"
	"github.com/aws/aws-sdk-go-v2/aws"
	sqsservice "github.com/aws/aws-sdk-go-v2/service/sqs"
)

func NewSqsSubscriber(logger watermill.LoggerAdapter, config aws.Config, optFns []func(*sqsservice.Options), debug bool) (*sqs.Subscriber, error) {
	return sqs.NewSubscriber(sqs.SubscriberConfig{
		DoNotCreateQueueIfNotExists: !debug,
		AWSConfig:                   config,
		OptFns:                      optFns,
	}, logger)
}
