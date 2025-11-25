package metrictesting

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/template"

	"github.com/formancehq/go-libs/v3/httpclient"
	"github.com/formancehq/go-libs/v3/logging"
	dockertesting "github.com/formancehq/go-libs/v3/testing/docker"
	"github.com/google/uuid"

	"github.com/ory/dockertest/v3"
	"github.com/stretchr/testify/require"
)

type T interface {
	require.TestingT
	TempDir() string
	Cleanup(func())
}

type PushGatewayServer struct {
	Name string
	Port string
}

func CreatePushGateway(logger logging.Logger, t T, pool *dockertesting.Pool) *PushGatewayServer {
	client := &http.Client{
		Transport: httpclient.NewDebugHTTPTransport(http.DefaultTransport),
	}
	resource := pool.Run(dockertesting.Configuration{
		RunOptions: &dockertest.RunOptions{
			Name:       uuid.NewString()[:8],
			Repository: "prom/pushgateway",
			Tag:        "v1.11.2",
		},
		CheckFn: func(ctx context.Context, resource *dockertest.Resource) error {
			port := resource.GetPort("9091/tcp")
			address := fmt.Sprintf("http://127.0.0.1:%s/-/ready", port)
			req, err := http.NewRequestWithContext(logging.ContextWithLogger(ctx, logger), "GET", address, nil)
			if err != nil {
				return err
			}

			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer func() {
				if err := resp.Body.Close(); err != nil {
					logger.Errorf("error closing response body: %s", err)
				}
			}()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
			}
			return nil
		},
	})

	return &PushGatewayServer{
		Name: resource.Container.Name[1:],
		Port: resource.GetPort("9091/tcp"),
	}
}

type PrometheusServerOption func(srv *PrometheusServer)

func WithScrapeConfig(name, port string) func(srv *PrometheusServer) {
	return func(srv *PrometheusServer) {
		srv.ScrapeConfigs = append(srv.ScrapeConfigs, ScrapeConfig{
			Name: name,
			Port: port,
		})
	}
}

type ScrapeConfig struct {
	Name string
	Port string
}

type PrometheusServer struct {
	ScrapeConfigs []ScrapeConfig
	Name          string
	Port          string
}

var defaultCfg = `
global:
  scrape_interval: 1s

{{ if .ScrapeConfigs }}
scrape_configs:
{{ range .ScrapeConfigs }}
- job_name: "{{ .Name }}"
  static_configs:
  - targets: [ "{{ .Name }}:{{ .Port }}" ]
{{ end }}
{{ end }}
`

func CreatePrometheusServer(logger logging.Logger, t T, pool *dockertesting.Pool, opts ...PrometheusServerOption) *PrometheusServer {
	client := &http.Client{
		Transport: httpclient.NewDebugHTTPTransport(http.DefaultTransport),
	}

	srv := &PrometheusServer{
		ScrapeConfigs: []ScrapeConfig{},
	}
	for _, opt := range opts {
		opt(srv)
	}

	tmpl, err := template.New("placeholder").Parse(defaultCfg)
	require.NoError(t, err)
	buf := bytes.NewBuffer([]byte{})
	require.NoError(t, tmpl.Execute(buf, srv))

	tmp := t.TempDir()
	path := fmt.Sprintf("%s/prometheus.yml", strings.TrimSuffix(tmp, "/"))

	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))

	cfg := dockertesting.Configuration{
		RunOptions: &dockertest.RunOptions{
			Repository: "prom/prometheus",
			Name:       uuid.NewString()[:8],
			Tag:        "v3.7.3",
			Cmd: []string{
				"--config.file=/etc/prometheus/prometheus.yml",
				"--web.enable-lifecycle",
			},
			Mounts: []string{
				fmt.Sprintf("%s:/etc/prometheus/prometheus.yml", path),
			},
		},
		CheckFn: func(ctx context.Context, resource *dockertest.Resource) error {
			port := resource.GetPort("9090/tcp")
			address := fmt.Sprintf("http://127.0.0.1:%s/-/ready", port)
			req, err := http.NewRequestWithContext(logging.ContextWithLogger(ctx, logger), "GET", address, nil)
			if err != nil {
				return err
			}

			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer func() {
				if err := resp.Body.Close(); err != nil {
					logger.Errorf("error closing response body: %s", err)
				}
			}()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
			}
			return nil
		},
	}
	resource := pool.Run(cfg)
	srv.Name = resource.Container.Name[1:]
	srv.Port = resource.GetPort("9090/tcp")
	return srv
}
