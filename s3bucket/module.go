package s3bucket

import (
	"github.com/formancehq/go-libs/v3/aws/iam"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.uber.org/fx"
)

const (
	S3BucketAWSEnabledFlag       = "s3-bucket-aws-enabled"
	S3BucketEndpointOverrideFlag = "s3-bucket-endpoint-override"
)

func AddFlags(flags *pflag.FlagSet) {
	flags.Bool(S3BucketAWSEnabledFlag, true, "Use AWS S3") // default is true until we add support for more s3 compatible providers
	flags.String(S3BucketEndpointOverrideFlag, "", "Connect to S3 using a custom endpoint (eg. localstack)")
}

func FXModuleFromFlags(cmd *cobra.Command, debug bool) fx.Option {
	options := make([]fx.Option, 0)

	awsEnabled, _ := cmd.Flags().GetBool(S3BucketAWSEnabledFlag)
	endpointOverride, _ := cmd.Flags().GetString(S3BucketEndpointOverrideFlag)
	if awsEnabled {
		options = append(options,
			fx.Supply(fx.Annotate(iam.LoadOptionFromCommand(cmd), fx.ResultTags(`name:"s3-bucket-aws-enabled"`))),
			awsModule(cmd, endpointOverride),
		)
	}
	return fx.Options(options...)
}
