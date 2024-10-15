package bunmigrate

import (
	"os"
	"testing"

	"github.com/formancehq/go-libs/v2/testing/docker"

	"github.com/formancehq/go-libs/v2/testing/platform/pgtesting"

	"github.com/formancehq/go-libs/v2/bun/bunconnect"
	"github.com/formancehq/go-libs/v2/logging"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestRunMigrate(t *testing.T) {
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
