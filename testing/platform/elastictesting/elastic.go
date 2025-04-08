package elastictesting

import (
	"context"
	"time"

	"github.com/formancehq/go-libs/v3/testing/docker"
	"github.com/olivere/elastic/v7"
	"github.com/ory/dockertest/v3"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

type Configuration struct {
	Timeout time.Duration
}

type Option func(*Configuration)

func WithTimeout(timeout time.Duration) Option {
	return func(c *Configuration) {
		c.Timeout = timeout
	}
}

var defaultOptions = []Option{
	WithTimeout(30 * time.Second),
}

type Server struct {
	elasticsearchEndpoint string
	t                     docker.T
}

func (s *Server) Endpoint() string {
	return s.elasticsearchEndpoint
}

func (s *Server) NewClient() *elastic.Client {
	ret, err := elastic.NewClient(elastic.SetURL(s.elasticsearchEndpoint))
	require.NoError(s.t, err)
	return ret
}

func CreateServer(pool *docker.Pool, options ...Option) *Server {

	cfg := Configuration{}
	for _, opt := range append(defaultOptions, options...) {
		opt(&cfg)
	}

	resource := pool.Run(docker.Configuration{
		RunOptions: &dockertest.RunOptions{
			Repository: "elasticsearch",
			Tag:        "8.14.3",
			Env: []string{
				"discovery.type=single-node",
				"xpack.security.enabled=false",
				"xpack.security.enrollment.enabled=false",
			},
		},
		CheckFn: func(ctx context.Context, resource *dockertest.Resource) error {
			client, err := elastic.NewClient(elastic.SetURL("http://127.0.0.1:" + resource.GetPort("9200/tcp")))
			if err != nil {
				return errors.Wrap(err, "connecting to server")
			}
			client.Stop()
			return nil
		},
		Timeout: cfg.Timeout,
	})

	return &Server{
		t:                     pool.T(),
		elasticsearchEndpoint: "http://127.0.0.1:" + resource.GetPort("9200/tcp"),
	}
}
