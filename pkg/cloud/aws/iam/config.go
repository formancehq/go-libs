package iam

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

// Options composes AWS configuration independently of CLI flag identities.
// Empty credentials retain the SDK default chain, including workload identity.
type Options struct {
	Region          string
	Profile         string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// LoadConfig rejects partial static credentials before consulting the SDK.
// Additional SDK options permit explicit test transports and configuration files.
func LoadConfig(ctx context.Context, opts Options, extra ...func(*config.LoadOptions) error) (aws.Config, error) {
	key, secret := strings.TrimSpace(opts.AccessKeyID), strings.TrimSpace(opts.SecretAccessKey)
	if (key == "") != (secret == "") || (key == "" && strings.TrimSpace(opts.SessionToken) != "") {
		return aws.Config{}, errors.New("iam: static credentials require both access key and secret")
	}
	options := append([]func(*config.LoadOptions) error{}, extra...)
	if region := strings.TrimSpace(opts.Region); region != "" {
		options = append(options, config.WithRegion(region))
	}
	if profile := strings.TrimSpace(opts.Profile); profile != "" {
		options = append(options, config.WithSharedConfigProfile(profile))
	}
	if key != "" {
		options = append(options, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(key, secret, opts.SessionToken)))
	}
	return config.LoadDefaultConfig(ctx, options...)
}
