package s3bucket

import (
	"context"
	"fmt"
	"net/url"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	transport "github.com/aws/smithy-go/endpoints"
	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

func NewAWSClient(config aws.Config, optFns []func(*s3.Options)) *s3.Client {
	return s3.NewFromConfig(config, optFns...)
}

func awsModule(cmd *cobra.Command, s3EndpointOverride string) fx.Option {
	return fx.Options(
		fx.Provide(func(optFn func(*config.LoadOptions) error) []func(*config.LoadOptions) error {
			loadOptions := []func(*config.LoadOptions) error{optFn}
			if s3EndpointOverride != "" {
				// if we are overriding the endpoint assume we are in a dev context
				loadOptions = append(loadOptions, config.WithCredentialsProvider(
					credentials.NewStaticCredentialsProvider("dummy", "dummy", "dummy"),
				))
			}
			return loadOptions
		}),
		fx.Provide(
			fx.Annotate(func(loadOpts []func(*config.LoadOptions) error) (aws.Config, error) {
				cfg, err := config.LoadDefaultConfig(cmd.Context(), loadOpts...)
				if err != nil {
					return cfg, fmt.Errorf("unable to load aws config %w", err)
				}
				return cfg, nil
			}, fx.ResultTags(`name:"s3-bucket-aws-cfg"`, ``)),
		),
		fx.Provide(
			fx.Annotate(func() ([]func(*s3.Options), error) {
				s3Opts := []func(*s3.Options){}
				if s3EndpointOverride == "" {
					return s3Opts, nil
				}

				s3Url, err := url.Parse(s3EndpointOverride)
				if err != nil {
					return s3Opts, fmt.Errorf("unable to parse s3 url %q", s3EndpointOverride)
				}
				resolver := &CustomEndpointResolver{url: *s3Url}
				s3Opts = append(s3Opts, s3.WithEndpointResolverV2(resolver))
				return s3Opts, nil
			}, fx.ResultTags(`name:"s3-bucket-aws-opts"`, ``)),
		),
		fx.Provide(
			fx.Annotate(func(config aws.Config, optFns []func(*s3.Options)) *s3.Client {
				return NewAWSClient(config, optFns)
			}, fx.ParamTags(`name:"s3-bucket-aws-cfg"`, `name:"s3-bucket-aws-opts"`)),
		),
	)
}

type CustomEndpointResolver struct {
	url url.URL
}

func (r *CustomEndpointResolver) ResolveEndpoint(_ context.Context, _ s3.EndpointParameters) (transport.Endpoint, error) {
	return transport.Endpoint{
		URI: r.url,
	}, nil
}
