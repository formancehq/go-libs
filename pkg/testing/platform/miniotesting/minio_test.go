package miniotesting

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/formancehq/go-libs/v5/pkg/cloud/aws/iam"
	"github.com/formancehq/go-libs/v5/pkg/fx/storagefx"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	s3bucket "github.com/formancehq/go-libs/v5/pkg/storage/s3"
	"github.com/formancehq/go-libs/v5/pkg/testing/docker"
	"github.com/formancehq/go-libs/v5/pkg/testing/utils"
)

var srv *MinioServer

func TestMain(m *testing.M) {
	utils.WithTestMain(func(t *utils.TestingTForMain) int {
		pool := docker.NewPool(t, logging.Testing())
		srv = CreateMinioServer(t, pool)
		return m.Run()
	})
}

func TestDefaultConfig(t *testing.T) {
	cfg := &config{
		accessKey: DefaultAccessKey,
		secretKey: DefaultSecretKey,
		tag:       DefaultTag,
	}

	assert.Equal(t, "minioadmin", cfg.accessKey)
	assert.Equal(t, "minioadmin", cfg.secretKey)
	assert.Equal(t, DefaultTag, cfg.tag)
}

func TestWithCredentials(t *testing.T) {
	cfg := &config{
		accessKey: DefaultAccessKey,
		secretKey: DefaultSecretKey,
		tag:       "latest",
	}

	WithCredentials("custom-key", "custom-secret")(cfg)
	assert.Equal(t, "custom-key", cfg.accessKey)
	assert.Equal(t, "custom-secret", cfg.secretKey)
}

func TestWithTag(t *testing.T) {
	cfg := &config{
		accessKey: DefaultAccessKey,
		secretKey: DefaultSecretKey,
		tag:       "latest",
	}

	WithTag("RELEASE.2024-01-01T00-00-00Z")(cfg)
	assert.Equal(t, "RELEASE.2024-01-01T00-00-00Z", cfg.tag)
}

func TestMinioServer_GetAWSConfig(t *testing.T) {
	cfg := srv.GetAWSConfig()
	assert.Equal(t, "us-east-1", cfg.Region)
	require.NotNil(t, cfg.Credentials)

	creds, err := cfg.Credentials.Retrieve(t.Context())
	require.NoError(t, err)
	assert.Equal(t, DefaultAccessKey, creds.AccessKeyID)
	assert.Equal(t, DefaultSecretKey, creds.SecretAccessKey)
}

func TestMinioServer_NewS3Client(t *testing.T) {
	client := srv.NewS3Client()
	require.NotNil(t, client)
	assert.Equal(t, srv.Endpoint, *client.Options().BaseEndpoint)
}

func TestMinioServer_CreateBucketAndListBuckets(t *testing.T) {
	ctx := context.Background()

	srv.CreateBucket(ctx, t, "test-bucket")

	client := srv.NewS3Client()
	result, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	require.NoError(t, err)

	var found bool
	for _, b := range result.Buckets {
		if aws.ToString(b.Name) == "test-bucket" {
			found = true
			break
		}
	}
	assert.True(t, found, "bucket 'test-bucket' should exist")
}

func TestMinioServer_PutAndGetObject(t *testing.T) {
	ctx := context.Background()
	bucket := "put-get-test"

	srv.CreateBucket(ctx, t, bucket)

	client := srv.NewS3Client()

	body := []byte("hello minio")
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String("test-key"),
		Body:   bytes.NewReader(body),
	})
	require.NoError(t, err)

	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String("test-key"),
	})
	require.NoError(t, err)
	defer out.Body.Close()

	got, err := io.ReadAll(out.Body)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

func TestS3ModuleFromFlags_WithMinioCredentials(t *testing.T) {
	ctx := context.Background()
	bucket := "fx-module-test"

	srv.CreateBucket(ctx, t, bucket)

	cmd := &cobra.Command{
		Use: "test",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	s3bucket.AddFlags(cmd.Flags())
	iam.AddFlags(cmd.Flags())
	cmd.SetContext(ctx)

	require.NoError(t, cmd.Flags().Set("s3-bucket-aws-enabled", "true"))
	require.NoError(t, cmd.Flags().Set("s3-bucket-endpoint-override", srv.Endpoint))
	require.NoError(t, cmd.Flags().Set("aws-access-key-id", srv.AccessKey))
	require.NoError(t, cmd.Flags().Set("aws-secret-access-key", srv.SecretKey))
	require.NoError(t, cmd.Flags().Set("aws-region", srv.Region))

	var client *s3.Client
	app := fxtest.New(t,
		storagefx.S3ModuleFromFlags(cmd),
		fx.Populate(&client),
	)
	app.RequireStart()
	defer app.RequireStop()

	require.NotNil(t, client)

	body := []byte("fx module test data")
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String("fx-test-key"),
		Body:   bytes.NewReader(body),
	})
	require.NoError(t, err)

	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String("fx-test-key"),
	})
	require.NoError(t, err)
	defer out.Body.Close()

	got, err := io.ReadAll(out.Body)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}
