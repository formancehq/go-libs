package metrictesting

import (
	"testing"

	"github.com/formancehq/go-libs/v3/logging"
	"github.com/formancehq/go-libs/v3/testing/docker"
	"github.com/formancehq/go-libs/v3/testing/utils"
	"github.com/stretchr/testify/require"
)

var (
	logger logging.Logger
	pool   *docker.Pool
	pg     *PushGatewayServer
	prom   *PrometheusServer
)

func TestMain(m *testing.M) {
	utils.WithTestMain(func(t *utils.TestingTForMain) int {
		logger = logging.Testing()
		pool = docker.NewPool(t, logger)
		pg = CreatePushGateway(logger, t, pool)
		prom = CreatePrometheusServer(
			logger,
			t,
			pool,
			WithScrapeConfig(pg.Name, pg.Port),
		)

		return m.Run()
	})
}

func TestRunPushGateway(t *testing.T) {
	t.Parallel()
	require.NotEmpty(t, pg.Port)
}

func TestRunPrometheusServer(t *testing.T) {
	t.Parallel()
	require.NotEmpty(t, prom.Port)
}
