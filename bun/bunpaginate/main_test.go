package bunpaginate_test

import (
	"testing"

	"github.com/formancehq/go-libs/v3/testing/docker"
	"github.com/formancehq/go-libs/v3/testing/utils"

	"github.com/formancehq/go-libs/v3/testing/platform/pgtesting"

	"github.com/formancehq/go-libs/v3/logging"
)

var srv *pgtesting.PostgresServer

func TestMain(m *testing.M) {
	utils.WithTestMain(func(t *utils.TestingTForMain) int {
		srv = pgtesting.CreatePostgresServer(t, docker.NewPool(t, logging.Testing()))

		return m.Run()
	})
}
