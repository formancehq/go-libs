package publish

import (
	"context"
	"fmt"
	"net/url"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-aws/sns"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	snsservice "github.com/aws/aws-sdk-go-v2/service/sns"
	transport "github.com/aws/smithy-go/endpoints"
	"github.com/formancehq/go-libs/v3/service"
	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

func NewSnsPublisher(cmd *cobra.Command, logger watermill.LoggerAdapter, config aws.Config, optFns []func(*snsservice.Options)) (*sns.Publisher, error) {
	credentials, err := config.Credentials.Retrieve(cmd.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to fetch credentials: %w", err)
	}

	topicResolver, err := sns.NewGenerateArnTopicResolver(credentials.AccountID, config.Region)
	if err != nil {
		return nil, fmt.Errorf("failed to create topic resolver for sns: %w", err)
	}

	return sns.NewPublisher(sns.PublisherConfig{
		DoNotCreateTopicIfNotExists: !service.IsDebug(cmd),
		TopicResolver:               topicResolver,
		AWSConfig:                   config,
		OptFns:                      optFns,
	}, logger)
}

func snsModule(cmd *cobra.Command, snsEndpointOverride string) fx.Option {
	return fx.Options(
		fx.Provide(
			fx.Annotate(func(optFn func(*config.LoadOptions) error) []func(*config.LoadOptions) error {
				loadOptions := []func(*config.LoadOptions) error{optFn}
				if snsEndpointOverride != "" {
					// if we are overriding the endpoint assume we are in a dev context
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
			fx.Annotate(func(lc fx.Lifecycle, logger watermill.LoggerAdapter, config aws.Config, optFns []func(*snsservice.Options)) (*sns.Publisher, error) {
				ret, err := NewSnsPublisher(cmd, logger, config, optFns)
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
