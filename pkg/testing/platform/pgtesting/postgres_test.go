package pgtesting

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	bunconnect "github.com/formancehq/go-libs/v5/pkg/storage/bun/connect"
	"github.com/formancehq/go-libs/v5/pkg/testing/docker"
	"github.com/formancehq/go-libs/v5/pkg/testing/utils"
)

var srv *PostgresServer

func TestMain(m *testing.M) {
	utils.WithTestMain(func(t *utils.TestingTForMain) int {
		srv = CreatePostgresServer(t, docker.NewPool(t, logging.Testing()))

		return m.Run()
	})
}

func TestPostgres(t *testing.T) {
	for i := 0; i < 10; i++ {
		t.Run(fmt.Sprintf("test%d", i), func(t *testing.T) {
			t.Parallel()
			database := srv.NewDatabase(t)
			conn, err := pgx.Connect(context.Background(), database.ConnString())
			require.NoError(t, err)
			require.NoError(t, conn.Close(context.Background()))
		})
	}
}

func TestConnectRejectsReadOnlySessions(t *testing.T) {
	t.Parallel()
	database := srv.NewDatabase(t)
	ctx := logging.TestingContext()

	t.Run("reset_session_discards_read_only_connections_before_reuse", func(t *testing.T) {
		t.Parallel()

		db, err := bunconnect.OpenSQLDB(ctx, bunconnect.ConnectionOptions{
			DatabaseSourceName: database.ConnString(),
			MaxIdleConns:       1,
			MaxOpenConns:       1,
		})
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, db.Close())
		})

		_, err = db.ExecContext(ctx, "set default_transaction_read_only=on")
		require.NoError(t, err)

		_, err = db.ExecContext(ctx, "create temp table reset_session_probe(id int)")
		require.NoError(t, err)

		var readOnly string
		require.NoError(t, db.QueryRowContext(ctx, "show transaction_read_only").Scan(&readOnly))
		require.Equal(t, "off", readOnly)
	})
}

func TestReadOnlySQLState25006(t *testing.T) {
	t.Parallel()
	database := srv.NewDatabase(t)
	ctx := logging.TestingContext()

	db, err := sql.Open("pgx", database.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = tx.Rollback()
	})

	_, err = tx.ExecContext(ctx, "create temp table read_only_sqlstate_probe(id int)")
	require.Error(t, err)
	require.Contains(t, err.Error(), "SQLSTATE 25006")
}
