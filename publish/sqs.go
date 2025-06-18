package publish

import (
	"context"
	"fmt"
	"net/url"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-aws/sqs"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	sqsservice "github.com/aws/aws-sdk-go-v2/service/sqs"
	transport "github.com/aws/smithy-go/endpoints"
	"github.com/formancehq/go-libs/v3/service"
	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

func NewSqsSubscriber(cmd *cobra.Command, logger watermill.LoggerAdapter, config aws.Config, optFns []func(*sqsservice.Options)) (*sqs.Subscriber, error) {
	return sqs.NewSubscriber(sqs.SubscriberConfig{
		DoNotCreateQueueIfNotExists: !service.IsDebug(cmd),
		AWSConfig:                   config,
		OptFns:                      optFns,
	}, logger)
}

func sqsModule(cmd *cobra.Command, sqsEndpointOverride string, loadOptions []func(*config.LoadOptions) error) fx.Option {
	return fx.Options(
		fx.Provide(
			fx.Annotate(func() (aws.Config, error) {
				cfg, err := config.LoadDefaultConfig(cmd.Context(), loadOptions...)
				if err != nil {
					return cfg, fmt.Errorf("unable load aws config %w", err)
				}
				return cfg, nil
			}, fx.ResultTags(`name:"publish-subscriber-sqs-cfg"`, ``)),
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
			fx.Annotate(func(lc fx.Lifecycle, logger watermill.LoggerAdapter, config aws.Config, optFns []func(*sqsservice.Options)) (*sqs.Subscriber, error) {
				ret, err := NewSqsSubscriber(cmd, logger, config, optFns)
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
