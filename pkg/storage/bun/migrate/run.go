package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/uptrace/bun"
	"github.com/xo/dburl"

	sharedlogging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	bunconnect "github.com/formancehq/go-libs/v5/pkg/storage/bun/connect"
	"github.com/formancehq/go-libs/v5/pkg/types/pointer"
)

func isDatabaseExists(ctx context.Context, db *bun.DB, name string) (bool, error) {
	row := db.QueryRowContext(ctx, `SELECT datname FROM pg_database WHERE datname = ?`, name)
	if row.Err() != nil {
		return false, fmt.Errorf("checking database list: %w", row.Err())
	}

	if err := row.Scan(pointer.For("")); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("scanning database row: %w", err)
		}

		return false, nil
	}

	return true, nil
}

func onPostgresDB(ctx context.Context, connectionOptions bunconnect.ConnectionOptions, callback func(db *bun.DB) error) error {
	url, err := dburl.Parse(connectionOptions.DatabaseSourceName)
	if err != nil {
		return fmt.Errorf("parsing dsn: %s: %w", connectionOptions.DatabaseSourceName, err)
	}

	url.Path = "postgres" // notes(gfyrag): default "postgres" database (most of the time?)
	connectionOptions.DatabaseSourceName = url.String()

	db, err := bunconnect.OpenSQLDB(ctx, connectionOptions)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer func() {
		err := db.Close()
		if err != nil {
			sharedlogging.FromContext(ctx).Errorf("Closing database: %s", err)
		}
	}()

	return callback(db)
}

func EnsureDatabaseNotExists(ctx context.Context, connectionOptions bunconnect.ConnectionOptions) error {
	return onPostgresDB(ctx, connectionOptions, func(db *bun.DB) error {

		url, err := dburl.Parse(connectionOptions.DatabaseSourceName)
		if err != nil {
			return fmt.Errorf("parsing dsn: %s: %w", connectionOptions.DatabaseSourceName, err)
		}

		databaseExists, err := isDatabaseExists(ctx, db, url.Path[1:])
		if err != nil {
			return fmt.Errorf("checking if database exists: %w", err)
		}

		if databaseExists {
			_, err = db.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE "%s"`, url.Path[1:]))
			if err != nil {
				return fmt.Errorf("dropping database: %w", err)
			}
		}

		return nil
	})
}

func EnsureDatabaseExists(ctx context.Context, connectionOptions bunconnect.ConnectionOptions) error {
	return onPostgresDB(ctx, connectionOptions, func(db *bun.DB) error {

		url, err := dburl.Parse(connectionOptions.DatabaseSourceName)
		if err != nil {
			return fmt.Errorf("parsing dsn: %s: %w", connectionOptions.DatabaseSourceName, err)
		}

		databaseExists, err := isDatabaseExists(ctx, db, url.Path[1:])
		if err != nil {
			return fmt.Errorf("checking if database exists: %w", err)
		}

		if !databaseExists {
			_, err = db.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, url.Path[1:]))
			if err != nil {
				return fmt.Errorf("creating database: %w", err)
			}
		}

		return nil
	})
}

func run(ctx context.Context, output io.Writer, args []string, connectionOptions *bunconnect.ConnectionOptions,
	executor func(args []string, db *bun.DB) error) error {

	if err := EnsureDatabaseExists(ctx, *connectionOptions); err != nil {
		return err
	}

	db, err := bunconnect.OpenSQLDB(ctx, *connectionOptions)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()

	if err := executor(args, db); err != nil {
		return fmt.Errorf("executing migration: %w", err)
	}
	return nil
}

func Run(cmd *cobra.Command, args []string, executor Executor) error {
	connectionOptions, err := bunconnect.ConnectionOptionsFromFlags(cmd.Flags(), cmd.Context())
	if err != nil {
		return fmt.Errorf("evaluating connection options: %w", err)
	}
	return run(cmd.Context(), cmd.OutOrStdout(), args, connectionOptions, func(args []string, db *bun.DB) error {
		return executor(cmd, args, db)
	})
}
