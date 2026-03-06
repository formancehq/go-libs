package migrate

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	bunconnect "github.com/formancehq/go-libs/v5/pkg/storage/bun/connect"
	"github.com/formancehq/go-libs/v5/pkg/testing/docker"
	"github.com/formancehq/go-libs/v5/pkg/testing/platform/pgtesting"
)

func TestRunMigrate(t *testing.T) {
	t.Parallel()
	dockerPool := docker.NewPool(t, logging.Testing())
	srv := pgtesting.CreatePostgresServer(t, dockerPool)

	connectionOptions := &bunconnect.ConnectionOptions{
		DatabaseSourceName: srv.GetDatabaseDSN("testing"),
	}
	executor := func(args []string, db *bun.DB) error {
		return nil
	}

	err := run(logging.TestingContext(), os.Stdout, []string{}, connectionOptions, executor)
	require.NoError(t, err)

	// Must be idempotent
	err = run(logging.TestingContext(), os.Stdout, []string{}, connectionOptions, executor)
	require.NoError(t, err)
}
