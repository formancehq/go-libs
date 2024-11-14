package temporaltesting

import (
	"context"
	"io"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
)

var defaultNamespace = "temporaltesting-default-namespace"

type TemporalT interface {
	require.TestingT
	Cleanup(func())
}

type TemporalServer struct {
	*testsuite.DevServer `json:"-"`
	defaultNamespace     string
}

func CreateTemporalServer(t TemporalT, w io.Writer) *TemporalServer {
	srv, err := testsuite.StartDevServer(context.Background(), testsuite.DevServerOptions{
		ClientOptions: &client.Options{Namespace: defaultNamespace},
		Stdout:        w,
		Stderr:        w,
	})
	if err != nil {
		require.Failf(t, "failed to start temporal dev server: %s", err.Error())
	}
	return &TemporalServer{DevServer: srv, defaultNamespace: defaultNamespace}
}

func (s *TemporalServer) Client(ctx context.Context) client.Client {
	return s.Client(ctx)
}

func (s *TemporalServer) Address() string {
	return s.FrontendHostPort()
}

func (s *TemporalServer) DefaultNamespace() string {
	return s.defaultNamespace
}
