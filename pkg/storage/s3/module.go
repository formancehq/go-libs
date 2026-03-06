package s3

import (
	"github.com/spf13/pflag"
)

const (
	S3BucketAWSEnabledFlag       = "s3-bucket-aws-enabled"
	S3BucketEndpointOverrideFlag = "s3-bucket-endpoint-override"
)

func AddFlags(flags *pflag.FlagSet) {
	flags.Bool(S3BucketAWSEnabledFlag, true, "Use AWS S3")
	flags.String(S3BucketEndpointOverrideFlag, "", "Connect to S3 using a custom endpoint (eg. localstack)")
}
