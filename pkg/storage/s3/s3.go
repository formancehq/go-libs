package s3

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func NewAWSClient(config aws.Config, optFns []func(*s3.Options)) *s3.Client {
	return s3.NewFromConfig(config, optFns...)
}
