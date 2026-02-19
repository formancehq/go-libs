package localstacktesting

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v4/logging"
	"github.com/formancehq/go-libs/v4/testing/docker"
	"github.com/formancehq/go-libs/v4/testing/utils"
)

var srv *LocalstackServer

func TestMain(m *testing.M) {
	utils.WithTestMain(func(t *utils.TestingTForMain) int {
		pool := docker.NewPool(t, logging.Testing())
		srv = CreateLocalstackServer(t, pool)
		return m.Run()
	})
}

func TestLocalstack(t *testing.T) {
	u, err := url.Parse(srv.Endpoint())
	require.NoError(t, err)
	assert.Equal(t, u.Port(), srv.GetPort())

	for i := 0; i < 10; i++ {
		t.Run(fmt.Sprintf("test%d", i), func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			cfg, err := srv.GetAWSConfig(ctx)
			require.NoError(t, err)

			s3Opts := append([]func(o *s3.Options){}, func(o *s3.Options) {
				o.UsePathStyle = true
				o.BaseEndpoint = aws.String(srv.Endpoint())
			})
			s3Client := s3.NewFromConfig(cfg, s3Opts...)
			result, err := s3Client.ListBuckets(ctx, &s3.ListBucketsInput{})
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Len(t, result.Buckets, 0)
		})
	}
}
