package s3bucket

import (
	"fmt"
	"net/url"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

func NewAWSClient(config aws.Config, optFns []func(*s3.Options)) *s3.Client {
	return s3.NewFromConfig(config, optFns...)
}

func awsModule(cmd *cobra.Command, s3EndpointOverride string) fx.Option {
	return fx.Options(
		fx.Provide(
			fx.Annotate(func(optFn func(*config.LoadOptions) error) []func(*config.LoadOptions) error {
				loadOptions := []func(*config.LoadOptions) error{optFn}
				if s3EndpointOverride != "" {
					// if we are overriding the endpoint assume we are in a dev context
					loadOptions = append(loadOptions, config.WithCredentialsProvider(
						credentials.NewStaticCredentialsProvider("dummy", "dummy", "dummy"),
					))
				}
				return loadOptions
			}, fx.ParamTags(`name:"s3-bucket-aws-enabled"`), fx.ResultTags(`name:"s3-bucket-load-opts"`)),
		),
		fx.Provide(
			fx.Annotate(func(loadOpts []func(*config.LoadOptions) error) (aws.Config, error) {
				cfg, err := config.LoadDefaultConfig(cmd.Context(), loadOpts...)
				if err != nil {
					return cfg, fmt.Errorf("unable to load aws config %w", err)
				}
				return cfg, nil
			}, fx.ParamTags(`name:"s3-bucket-load-opts"`), fx.ResultTags(`name:"s3-bucket-aws-cfg"`, ``)),
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
				s3Opts = append(s3Opts, func(o *s3.Options) {
					o.UsePathStyle = true
					o.BaseEndpoint = aws.String(s3Url.String())
				})
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
