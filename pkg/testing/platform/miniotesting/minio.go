package miniotesting

import (
	"context"
	"fmt"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/ory/dockertest/v3"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/testing/docker"
)

const (
	DefaultAccessKey = "minioadmin"
	DefaultSecretKey = "minioadmin"
	DefaultRegion    = "us-east-1"
	DefaultTag       = "RELEASE.2025-04-22T22-12-26Z"
)

type T interface {
	require.TestingT
	Cleanup(func())
	Helper()
}

type MinioServer struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Region    string
}

func (s *MinioServer) GetAWSConfig() aws.Config {
	return aws.Config{
		Region:      s.Region,
		Credentials: credentials.NewStaticCredentialsProvider(s.AccessKey, s.SecretKey, ""),
	}
}

func (s *MinioServer) NewS3Client() *s3.Client {
	return s3.NewFromConfig(s.GetAWSConfig(), func(o *s3.Options) {
		o.BaseEndpoint = aws.String(s.Endpoint)
		o.UsePathStyle = true
	})
}

func (s *MinioServer) CreateBucket(ctx context.Context, t T, bucket string) {
	t.Helper()
	client := s.NewS3Client()
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket:                    aws.String(bucket),
		CreateBucketConfiguration: &s3types.CreateBucketConfiguration{LocationConstraint: s3types.BucketLocationConstraint(s.Region)},
	})
	require.NoError(t, err, "creating minio bucket %q", bucket)
}

type Option func(*config)

type config struct {
	accessKey string
	secretKey string
	tag       string
}

func WithCredentials(accessKey, secretKey string) Option {
	return func(c *config) {
		c.accessKey = accessKey
		c.secretKey = secretKey
	}
}

func WithTag(tag string) Option {
	return func(c *config) {
		c.tag = tag
	}
}

func CreateMinioServer(t T, pool *docker.Pool, opts ...Option) *MinioServer {
	t.Helper()

	cfg := &config{
		accessKey: DefaultAccessKey,
		secretKey: DefaultSecretKey,
		tag:       DefaultTag,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	resource := pool.Run(docker.Configuration{
		RunOptions: &dockertest.RunOptions{
			Repository: "minio/minio",
			Tag:        cfg.tag,
			Cmd:        []string{"server", "/data"},
			Env: []string{
				"MINIO_ROOT_USER=" + cfg.accessKey,
				"MINIO_ROOT_PASSWORD=" + cfg.secretKey,
			},
		},
		CheckFn: func(ctx context.Context, resource *dockertest.Resource) error {
			endpoint := fmt.Sprintf("http://127.0.0.1:%s", resource.GetPort("9000/tcp"))
			resp, err := http.Get(endpoint + "/minio/health/live")
			if err != nil {
				return err
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("minio health check returned %d", resp.StatusCode)
			}
			return nil
		},
	})

	return &MinioServer{
		Endpoint:  fmt.Sprintf("http://127.0.0.1:%s", resource.GetPort("9000/tcp")),
		AccessKey: cfg.accessKey,
		SecretKey: cfg.secretKey,
		Region:    DefaultRegion,
	}
}
