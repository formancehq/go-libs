package bunmigrate

import (
	"context"
	"os"
	"testing"

	"github.com/formancehq/go-libs/v2/testing/docker"

	"github.com/formancehq/go-libs/v2/testing/platform/pgtesting"

	"github.com/formancehq/go-libs/v2/bun/bunconnect"
	"github.com/formancehq/go-libs/v2/logging"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
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

func TestIsDatabaseExists(t *testing.T) {
	t.Parallel()
	dockerPool := docker.NewPool(t, logging.Testing())
	srv := pgtesting.CreatePostgresServer(t, dockerPool)

	// Connect to postgres database
	connectionOptions := bunconnect.ConnectionOptions{
		DatabaseSourceName: srv.GetDatabaseDSN("postgres"),
	}
	db, err := bunconnect.OpenSQLDB(logging.TestingContext(), connectionOptions)
	require.NoError(t, err)
	defer db.Close()

	// Test with non-existent database
	exists, err := isDatabaseExists(logging.TestingContext(), db, "nonexistentdb")
	require.NoError(t, err)
	require.False(t, exists)

	// Create a database
	_, err = db.ExecContext(logging.TestingContext(), `CREATE DATABASE testdb`)
	require.NoError(t, err)

	// Test with existing database
	exists, err = isDatabaseExists(logging.TestingContext(), db, "testdb")
	require.NoError(t, err)
	require.True(t, exists)
}

func TestOnPostgresDB(t *testing.T) {
	t.Parallel()
	dockerPool := docker.NewPool(t, logging.Testing())
	srv := pgtesting.CreatePostgresServer(t, dockerPool)

	connectionOptions := bunconnect.ConnectionOptions{
		DatabaseSourceName: srv.GetDatabaseDSN("testing"),
	}

	// Test with valid callback
	err := onPostgresDB(logging.TestingContext(), connectionOptions, func(db *bun.DB) error {
		require.NotNil(t, db)
		return nil
	})
	require.NoError(t, err)

	// Test with callback that returns error
	err = onPostgresDB(logging.TestingContext(), connectionOptions, func(db *bun.DB) error {
		return context.Canceled
	})
	require.Error(t, err)
	require.Equal(t, context.Canceled, err)

	// Test with invalid DSN
	invalidOptions := bunconnect.ConnectionOptions{
		DatabaseSourceName: "invalid://localhost:5432/testing",
	}
	err = onPostgresDB(logging.TestingContext(), invalidOptions, func(db *bun.DB) error {
		return nil
	})
	require.Error(t, err)
}

func TestEnsureDatabaseExists(t *testing.T) {
	t.Parallel()
	dockerPool := docker.NewPool(t, logging.Testing())
	srv := pgtesting.CreatePostgresServer(t, dockerPool)

	// Test creating a new database
	connectionOptions := bunconnect.ConnectionOptions{
		DatabaseSourceName: srv.GetDatabaseDSN("newdb"),
	}
	err := EnsureDatabaseExists(logging.TestingContext(), connectionOptions)
	require.NoError(t, err)

	// Test with existing database (idempotent)
	err = EnsureDatabaseExists(logging.TestingContext(), connectionOptions)
	require.NoError(t, err)

	// Test with invalid DSN
	invalidOptions := bunconnect.ConnectionOptions{
		DatabaseSourceName: "invalid://localhost:5432/testing",
	}
	err = EnsureDatabaseExists(logging.TestingContext(), invalidOptions)
	require.Error(t, err)
}

func TestEnsureDatabaseNotExists(t *testing.T) {
	t.Parallel()
	dockerPool := docker.NewPool(t, logging.Testing())
	srv := pgtesting.CreatePostgresServer(t, dockerPool)

	// Create a database first
	connectionOptions := bunconnect.ConnectionOptions{
		DatabaseSourceName: srv.GetDatabaseDSN("tempdb"),
	}
	
	// Connect to postgres database
	pgConnectionOptions := bunconnect.ConnectionOptions{
		DatabaseSourceName: srv.GetDatabaseDSN("postgres"),
	}
	db, err := bunconnect.OpenSQLDB(logging.TestingContext(), pgConnectionOptions)
	require.NoError(t, err)
	defer db.Close()
	
	// Create the database
	_, err = db.ExecContext(logging.TestingContext(), `CREATE DATABASE tempdb`)
	require.NoError(t, err)

	// Test dropping an existing database
	err = EnsureDatabaseNotExists(logging.TestingContext(), connectionOptions)
	require.NoError(t, err)

	// Test with non-existent database (idempotent)
	err = EnsureDatabaseNotExists(logging.TestingContext(), connectionOptions)
	require.NoError(t, err)

	// Test with invalid DSN
	invalidOptions := bunconnect.ConnectionOptions{
		DatabaseSourceName: "invalid://localhost:5432/testing",
	}
	err = EnsureDatabaseNotExists(logging.TestingContext(), invalidOptions)
	require.Error(t, err)
}

func TestRun(t *testing.T) {
	t.Parallel()
	dockerPool := docker.NewPool(t, logging.Testing())
	srv := pgtesting.CreatePostgresServer(t, dockerPool)

	// Create a test command
	cmd := &cobra.Command{
		Use: "test",
	}
	cmd.Flags().String(bunconnect.PostgresURIFlag, srv.GetDatabaseDSN("runtest"), "")
	
	// Set context
	ctx := logging.TestingContext()
	cmd.SetContext(ctx)
	
	// Test with valid executor
	err := Run(cmd, []string{}, func(cmd *cobra.Command, args []string, db *bun.DB) error {
		require.NotNil(t, db)
		return nil
	})
	require.NoError(t, err)

	// Test with executor that returns error
	err = Run(cmd, []string{}, func(cmd *cobra.Command, args []string, db *bun.DB) error {
		return context.Canceled
	})
	require.Error(t, err)
}
