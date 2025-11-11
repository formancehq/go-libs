package pgtesting

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v3/logging"
	"github.com/formancehq/go-libs/v3/testing/docker"
	"github.com/formancehq/go-libs/v3/testing/utils"
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
