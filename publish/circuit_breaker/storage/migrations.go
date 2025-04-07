package storage

import (
	"context"
	"database/sql"

	"github.com/formancehq/go-libs/v3/migrations"
	"github.com/uptrace/bun"
)

func registerMigrations(migrator *migrations.Migrator, schema string) {
	migrator.RegisterMigrations(
		migrations.Migration{
			Up: func(ctx context.Context, db bun.IDB) error {
				return db.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
					_, err := tx.Exec("set search_path = ?", schema)
					if err != nil {
						return err
					}
					_, err = tx.Exec(initialSchema)
					return err
				})
			},
		},
	)
}

func Migrate(ctx context.Context, schema string, db *bun.DB) error {
	migrator := migrations.NewMigrator(
		db,
		migrations.WithTableName("circuit_breaker_migrations"),
	)

	registerMigrations(migrator, schema)

	return migrator.Up(ctx)
}

const initialSchema = `
CREATE TABLE IF NOT EXISTS "circuit_breaker" (
	id bigserial NOT NULL,
	created_at timestamp with time zone NOT NULL,
	topic text NOT NULL,
	data bytea NOT NULL,
	metadata jsonb,
	PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "circuit_breaker_creation_date_idx" ON "circuit_breaker" ("created_at" ASC);
`
