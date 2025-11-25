package oteltesting

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"

	"github.com/formancehq/go-libs/v3/httpclient"
	"github.com/formancehq/go-libs/v3/logging"
	dockertesting "github.com/formancehq/go-libs/v3/testing/docker"
	metrictesting "github.com/formancehq/go-libs/v3/testing/platform/metricstesting"
	"github.com/google/uuid"
	"github.com/ory/dockertest/v3"
	"github.com/stretchr/testify/require"
)

var otelCollectorTmpl = `
receivers:
  otlp:
    protocols:
      grpc:
      http:

exporters:
  debug:
    verbosity: detailed
  prometheus:
    endpoint: "127.0.0.1:{{ .Port }}"

service:
  extensions:
    - health_check
  pipelines:
    metrics:
      receivers: [otlp]
      exporters: [prometheus, debug]
  telemetry:
    logs:
      level: debug
      encoding: console
      output_paths: [stdout]

extensions:
  health_check:
    endpoint: "0.0.0.0:13133"
    path: /health
`

type T interface {
	require.TestingT
	TempDir() string
	Cleanup(func())
}

type OtelCollectorServer struct {
	Port string
}

func CreateOtelCollectorServer(logger logging.Logger, t T, pool *dockertesting.Pool, srv *metrictesting.PrometheusServer) *OtelCollectorServer {
	tmpl, err := template.New(`placeholder`).Parse(otelCollectorTmpl)
	require.NoError(t, err)
	buf := bytes.NewBuffer([]byte{})
	require.NoError(t, tmpl.Execute(buf, struct {
		Name string
		Port string
	}{srv.Name, srv.Port}))
	tmp := t.TempDir()
	path := fmt.Sprintf("%s/otel-config.yaml", strings.TrimSuffix(tmp, "/"))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))

	client := &http.Client{
		Transport: httpclient.NewDebugHTTPTransport(http.DefaultTransport),
	}
	resource := pool.Run(dockertesting.Configuration{
		RunOptions: &dockertest.RunOptions{
			Repository: "otel/opentelemetry-collector-contrib",
			Tag:        "0.112.0",
			Name:       uuid.NewString()[:8],
			Cmd: []string{
				"--config=/etc/otelcol-contrib/config.yaml",
			},
			Mounts: []string{
				fmt.Sprintf("%s:/etc/otelcol-contrib/config.yaml:ro", path),
			},
			ExposedPorts: []string{"4317/tcp", "13133/tcp"},
		},
		CheckFn: func(ctx context.Context, resource *dockertest.Resource) error {
			port := resource.GetPort("13133/tcp")
			address := fmt.Sprintf("http://127.0.0.1:%s/health", port)
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

	return &OtelCollectorServer{
		Port: resource.GetPort("4317/tcp"),
	}
}
