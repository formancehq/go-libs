package publish

import (
	"context"
	"fmt"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-aws/sns"
	"github.com/aws/aws-sdk-go-v2/aws"
	snsservice "github.com/aws/aws-sdk-go-v2/service/sns"
)

func NewSnsPublisher(ctx context.Context, logger watermill.LoggerAdapter, config aws.Config, optFns []func(*snsservice.Options), debug bool) (*sns.Publisher, error) {
	credentials, err := config.Credentials.Retrieve(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch credentials: %w", err)
	}
	accountID := "000000000000" // if we are using a static credentials provider in a dev env, it may be empty
	if credentials.AccountID != "" {
		accountID = credentials.AccountID
	}

	topicResolver, err := sns.NewGenerateArnTopicResolver(accountID, config.Region)
	if err != nil {
		return nil, fmt.Errorf("failed to create topic resolver for sns: %w", err)
	}

	return sns.NewPublisher(sns.PublisherConfig{
		DoNotCreateTopicIfNotExists: !debug,
		TopicResolver:               topicResolver,
		AWSConfig:                   config,
		OptFns:                      optFns,
	}, logger)
}
