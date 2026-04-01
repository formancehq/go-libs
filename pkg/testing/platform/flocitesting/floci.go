package flocitesting

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/ory/dockertest/v3"
	oryDocker "github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/testing/docker"
)

var (
	defaultHostIP   = "0.0.0.0"
	defaultBindPort = 4566

	defaultVersion = "latest"
	defaultOptions = []Option{
		WithDefaultRegion("us-east-1"),
		WithVersionFromEnv(),
	}
)

type Option func(opts *Config)

type Config struct {
	DefaultRegion string
	Version       string
}

type FlociT interface {
	require.TestingT
	Cleanup(func())
}

type FlociServer struct {
	Port          string
	Config        Config
	defaultRegion string
	resource      *dockertest.Resource
	internalPort  string
}

func CreateFlociServer(t FlociT, pool *docker.Pool, opts ...Option) *FlociServer {
	cfg := Config{}
	for _, opt := range append(defaultOptions, opts...) {
		opt(&cfg)
	}

	if cfg.Version == "" {
		cfg.Version = defaultVersion
	}

	env := []string{
		fmt.Sprintf("FLOCI_DEFAULT_REGION=%s", cfg.DefaultRegion),
	}

	bindPortString := fmt.Sprintf("%d/tcp", defaultBindPort)
	resource := pool.Run(docker.Configuration{
		RunOptions: &dockertest.RunOptions{
			Repository: "hectorvent/floci",
			Tag:        cfg.Version,
			Env:        env,
			PortBindings: map[oryDocker.Port][]oryDocker.PortBinding{
				oryDocker.Port(bindPortString): {{HostIP: defaultHostIP}},
			},
		},
		CheckFn: func(ctx context.Context, resource *dockertest.Resource) error {
			// Floci has no dedicated healthcheck endpoint.
			// Use S3 ListBuckets (GET /) which returns 200 when the server is ready.
			client := &http.Client{Timeout: time.Second}

			endpoint := fmt.Sprintf("http://localhost:%s/", resource.GetPort(bindPortString))
			req, err := http.NewRequest("GET", endpoint, nil)
			assert.NoError(t, err)

			res, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("request to container with state %q & health status %q: %w",
					resource.Container.State.Status,
					resource.Container.State.Health.Status,
					err,
				)
			}
			defer res.Body.Close()

			// Any successful HTTP response means Floci is up.
			// S3 ListBuckets returns 200, other paths may return 4xx — all prove the server is alive.
			if res.StatusCode >= 500 {
				return fmt.Errorf("floci not ready, got status %d", res.StatusCode)
			}
			return nil
		},
	})
	return &FlociServer{
		Config:        cfg,
		Port:          resource.GetPort(bindPortString),
		defaultRegion: cfg.DefaultRegion,
		resource:      resource,
		internalPort:  bindPortString,
	}
}

func (s *FlociServer) GetPort() string {
	return s.Port
}

func (s *FlociServer) GetHostPort() string {
	return s.resource.GetHostPort(s.internalPort)
}

func (s *FlociServer) Endpoint() string {
	return fmt.Sprintf("http://%s", s.GetHostPort())
}

func (s *FlociServer) DefaultRegion() string {
	return s.defaultRegion
}

func (s *FlociServer) GetAWSConfig(ctx context.Context) (aws.Config, error) {
	cfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithRegion(s.DefaultRegion()),
		config.WithCredentialsProvider(aws.AnonymousCredentials{}),
	)
	return cfg, err
}

func WithVersionFromEnv() Option {
	return func(opts *Config) {
		if v := os.Getenv("FLOCI_VERSION"); v != "" {
			opts.Version = v
		}
	}
}

func WithVersion(version string) Option {
	return func(opts *Config) {
		opts.Version = version
	}
}

func WithDefaultRegion(name string) Option {
	return func(opts *Config) {
		opts.DefaultRegion = name
	}
}
