package migrations

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/formancehq/go-libs/v2/testing/utils"
	"github.com/google/uuid"
	"github.com/spf13/pflag"

	"github.com/formancehq/go-libs/v2/logging"
	"github.com/formancehq/go-libs/v2/testing/docker"

	"github.com/formancehq/go-libs/v2/testing/platform/pgtesting"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/extra/bundebug"
)

var (
	dockerPool *docker.Pool
	srv        *pgtesting.PostgresServer
	db         *pgtesting.Database
	sqlDB      *sql.DB
	bunDB      *bun.DB
)

func TestMain(m *testing.M) {
	utils.WithTestMain(func(t *utils.TestingTForMain) int {
		var err error

		dockerPool = docker.NewPool(t, logging.Testing())
		srv = pgtesting.CreatePostgresServer(t, dockerPool)
		db = srv.NewDatabase(t)
		sqlDB, err = sql.Open("pgx", db.ConnString())
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = sqlDB.Close()
		})

		bunDB = bun.NewDB(sqlDB, pgdialect.New())
		if testing.Verbose() {
			bunDB.AddQueryHook(bundebug.NewQueryHook(
				bundebug.WithVerbose(true),
				bundebug.FromEnv("BUNDEBUG"),
			))
		}

		return m.Run()
	})
}

func TestMigrationsListen(t *testing.T) {
	t.Parallel()

	ctx := logging.TestingContext()

	schema := uuid.NewString()[:8]
	migrator := NewMigrator(bunDB, WithSchema(schema))
	migrator.RegisterMigrations(Migration{
		Up: func(ctx context.Context, db bun.IDB) error {
			_, err := db.ExecContext(ctx, `
				do $$
				begin
					perform pg_notify('migrations-`+schema+`', 'init: 100');
					for ind in 1..10 loop
						perform pg_notify('migrations-`+schema+`', 'continue: 10');
						perform pg_sleep(0.1);
					end loop;
				end
				$$;
			`)
			return err
		},
	})
	require.NoError(t, migrator.Up(ctx))

	// todo: what test at this point?
}

func TestMigrationsConcurrently(t *testing.T) {
	t.Parallel()

	ctx := logging.TestingContext()

	migrationStarted := make(chan struct{})
	terminatedMigration := make(chan struct{})

	options := []Option{
		WithSchema(uuid.NewString()[:8]),
	}
	migrator1 := NewMigrator(bunDB, options...)
	migrator1.RegisterMigrations(Migration{
		Up: func(ctx context.Context, db bun.IDB) error {
			close(migrationStarted)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-terminatedMigration:
				return nil
			}
		},
	})

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	t.Cleanup(cancel)

	migrator1Err := make(chan error, 1)
	go func() {
		migrator1Err <- migrator1.UpByOne(ctx)
	}()

	<-migrationStarted

	migrator2 := NewMigrator(bunDB, options...)
	migrator2.RegisterMigrations(Migration{
		Up: func(ctx context.Context, db bun.IDB) error {
			return errors.New("should not have been called")
		},
	})

	migrator2Err := make(chan error, 1)
	go func() {
		migrator2Err <- migrator2.UpByOne(ctx)
	}()

	close(terminatedMigration)

	select {
	case err := <-migrator1Err:
		require.NoError(t, err)

		select {
		case err := <-migrator2Err:
			require.True(t, errors.Is(err, ErrAlreadyUpToDate))
		case <-time.After(time.Second * 2):
			t.Fatal("migrator2 did not finish")
		}
	case <-time.After(time.Second * 2):
		t.Fatal("migrator1 did not finish")
	}
}

func TestMigrationsNominal(t *testing.T) {
	t.Parallel()

	ctx := logging.TestingContext()

	migrator1 := NewMigrator(bunDB, WithSchema(uuid.NewString()[:8]))
	migrator1.RegisterMigrations(Migration{
		Up: func(ctx context.Context, db bun.IDB) error {
			return nil
		},
	})

	err := migrator1.UpByOne(ctx)
	require.NoError(t, err)
}

// TestTwoMigratorsSameSchema reproduces a bug where two migrators using
// different table names but sharing the same schema could not coexist:
// the version_id unique index name was hardcoded ("idx_version_id"), and
// since index names are scoped per-schema in PostgreSQL, the second
// migrator's CREATE UNIQUE INDEX IF NOT EXISTS was silently skipped,
// leaving its INSERT ... ON CONFLICT (version_id) without a matching
// unique constraint (SQLSTATE 42P10).
func TestTwoMigratorsSameSchema(t *testing.T) {
	t.Parallel()

	ctx := logging.TestingContext()
	schema := uuid.NewString()[:8]

	first := NewMigrator(bunDB, WithSchema(schema), WithTableName("first_migrations"))
	first.RegisterMigrations(Migration{Up: func(ctx context.Context, db bun.IDB) error { return nil }})
	require.NoError(t, first.Up(ctx))

	second := NewMigrator(bunDB, WithSchema(schema), WithTableName("second_migrations"))
	second.RegisterMigrations(Migration{Up: func(ctx context.Context, db bun.IDB) error { return nil }})
	require.NoError(t, second.Up(ctx))
}

func TestAddFlags(t *testing.T) {
	t.Parallel()

	flags := pflag.NewFlagSet("test", pflag.PanicOnError)
	AddFlags(flags)

	table, _ := flags.GetString(MigratorTableFlag)
	require.Equal(t, migrationTable, table)
	schema, _ := flags.GetString(MigratorSchemaFlag)
	require.Equal(t, "public", schema)

	require.NoError(t, flags.Set(MigratorSchemaFlag, "test"))
	table, _ = flags.GetString(MigratorSchemaFlag)
	require.Equal(t, table, "test")
}
