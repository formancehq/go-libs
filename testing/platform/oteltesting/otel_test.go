package oteltesting

import (
	"testing"

	"github.com/formancehq/go-libs/v3/logging"
	"github.com/formancehq/go-libs/v3/testing/docker"
	metrictesting "github.com/formancehq/go-libs/v3/testing/platform/metricstesting"
	"github.com/formancehq/go-libs/v3/testing/utils"
	"github.com/stretchr/testify/require"
)

var (
	logger    logging.Logger
	pool      *docker.Pool
	prom      *metrictesting.PrometheusServer
	collector *OtelCollectorServer
)

func TestMain(m *testing.M) {
	utils.WithTestMain(func(t *utils.TestingTForMain) int {
		logger = logging.Testing()
		pool = docker.NewPool(t, logger)
		prom = metrictesting.CreatePrometheusServer(
			logger,
			t,
			pool,
		)
		collector = CreateOtelCollectorServer(logger, t, pool, prom)

		return m.Run()
	})
}

func TestRunOtelCollectorServer(t *testing.T) {
	t.Parallel()
	require.NotEmpty(t, collector.Port)
}
