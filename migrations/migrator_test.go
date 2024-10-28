package migrations

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/formancehq/go-libs/v2/platform/postgres"
	"github.com/formancehq/go-libs/v2/testing/utils"
	"github.com/google/uuid"

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

func TestMigrationsConcurrently(t *testing.T) {
	t.Parallel()

	ctx := logging.TestingContext()

	type testCase struct {
		name        string
		options     []Option
		expectError error
	}

	testCases := []testCase{
		{
			name: "default",
		},
		{
			name: "with schema and create",
			options: []Option{
				WithSchema(uuid.NewString()[:8], true),
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			migrationStarted := make(chan struct{})
			terminatedMigration := make(chan struct{})

			migrator1 := NewMigrator(bunDB, testCase.options...)
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

			ctx, cancel := context.WithTimeout(ctx, time.Second)
			t.Cleanup(cancel)

			migrator1Err := make(chan error, 1)
			go func() {
				migrator1Err <- migrator1.UpByOne(ctx)
			}()

			<-migrationStarted

			migrator2 := NewMigrator(bunDB, testCase.options...)
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
				if testCase.expectError != nil {
					require.True(t, errors.Is(err, testCase.expectError))
				} else {
					require.NoError(t, err)
				}

				select {
				case err := <-migrator2Err:
					if testCase.expectError != nil {
						require.True(t, errors.Is(err, testCase.expectError))
					} else {
						require.True(t, errors.Is(err, ErrAlreadyUpToDate))
					}
				case <-time.After(time.Second):
					t.Fatal("migrator2 did not finish")
				}
			case <-time.After(time.Second):
				t.Fatal("migrator1 did not finish")
			}
		})
	}
}

func TestMigrationsMissingSchema(t *testing.T) {
	t.Parallel()

	ctx := logging.TestingContext()

	migrator1 := NewMigrator(bunDB, WithSchema("foo", false))
	migrator1.RegisterMigrations(Migration{
		Up: func(ctx context.Context, db bun.IDB) error {
			return nil
		},
	})

	err := migrator1.UpByOne(ctx)
	require.True(t, errors.Is(err, postgres.ErrMissingSchema))
}
