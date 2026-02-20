package localstacktesting

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/ory/dockertest/v3"
	oryDocker "github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v4/testing/docker"
)

var (
	defaultHostIP   = "0.0.0.0"
	defaultBindPort = 4566

	defaultVersion = "latest"
	defaultOptions = []Option{
		WithService("s3"),
		WithDefaultRegion("us-east-1"),
		WithVersionFromEnv(),
	}
)

type Option func(opts *Config)

type Config struct {
	Services      []string
	DefaultRegion string
	Version       string
	Debug         bool
}

func (c Config) validate() error {
	if len(c.Services) == 0 {
		return errors.New("must have at least 1 service")
	}
	return nil
}

type LocalstackT interface {
	require.TestingT
	Cleanup(func())
}

type LocalstackServer struct {
	Port          string
	Config        Config
	defaultRegion string
	resource      *dockertest.Resource
	internalPort  string
}

func CreateLocalstackServer(t LocalstackT, pool *docker.Pool, opts ...Option) *LocalstackServer {
	cfg := Config{}
	for _, opt := range append(defaultOptions, opts...) {
		opt(&cfg)
	}
	require.NoError(t, cfg.validate())

	if cfg.Version == "" {
		cfg.Version = defaultVersion
	}

	env := []string{
		"PERSISTENCE=0",
		"SKIP_SSL_CERT_DOWNLOAD=1",
		fmt.Sprintf("SERVICES=%s", strings.Join(cfg.Services, ",")),
		fmt.Sprintf("AWS_DEFAULT_REGION=%s", cfg.DefaultRegion),
		fmt.Sprintf("GATEWAY_LISTEN=%s:%d", defaultHostIP, defaultBindPort),
	}
	if cfg.Debug {
		env = append(env, "DEBUG=1")
	}

	bindPortString := fmt.Sprintf("%d/tcp", defaultBindPort)
	resource := pool.Run(docker.Configuration{
		RetryCheckInterval: time.Second, // localstack container creation is a bit slow
		RunOptions: &dockertest.RunOptions{
			Repository: "localstack/localstack",
			Tag:        cfg.Version,
			Env:        env,
			PortBindings: map[oryDocker.Port][]oryDocker.PortBinding{
				oryDocker.Port(bindPortString): {{HostIP: defaultHostIP, HostPort: fmt.Sprintf("%d", defaultBindPort)}},
			},
		},
		CheckFn: func(ctx context.Context, resource *dockertest.Resource) error {
			client := &http.Client{Timeout: time.Second}

			endpoint := fmt.Sprintf("http://localhost:%s/_localstack/init/ready", resource.GetPort(bindPortString))
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
			assert.Equal(t, http.StatusOK, res.StatusCode)
			return nil
		},
	})
	return &LocalstackServer{
		Config:        cfg,
		Port:          resource.GetPort(bindPortString),
		defaultRegion: cfg.DefaultRegion,
		resource:      resource,
		internalPort:  bindPortString,
	}
}

func (s *LocalstackServer) GetPort() string {
	return s.Port
}

func (s *LocalstackServer) GetHostPort() string {
	return s.resource.GetHostPort(s.internalPort)
}

func (s *LocalstackServer) Endpoint() string {
	return fmt.Sprintf("http://%s", s.GetHostPort())
}

func (s *LocalstackServer) DefaultRegion() string {
	return s.defaultRegion
}

func (s *LocalstackServer) GetAWSConfig(ctx context.Context) (aws.Config, error) {
	cfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithRegion(s.DefaultRegion()),
		config.WithCredentialsProvider(aws.AnonymousCredentials{}), // LocalStack doesn't require real credentials
	)
	return cfg, err
}

func WithVersionFromEnv() Option {
	return func(opts *Config) {
		opts.Version = os.Getenv("LOCALSTACK_VERSION")
	}
}

func WithService(name string) Option {
	return func(opts *Config) {
		if opts.Services == nil {
			opts.Services = make([]string, 0)
		}
		opts.Services = append(opts.Services, name)
	}
}

func WithDefaultRegion(name string) Option {
	return func(opts *Config) {
		opts.DefaultRegion = name
	}
}
