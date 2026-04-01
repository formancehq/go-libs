package flocitesting

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/testing/docker"
	"github.com/formancehq/go-libs/v5/pkg/testing/utils"
)

var srv *FlociServer

func TestMain(m *testing.M) {
	utils.WithTestMain(func(t *utils.TestingTForMain) int {
		pool := docker.NewPool(t, logging.Testing())
		srv = CreateFlociServer(t, pool)
		return m.Run()
	})
}

func TestFloci(t *testing.T) {
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
