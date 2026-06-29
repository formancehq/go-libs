package connect_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/storage/bun/connect"
	"github.com/formancehq/go-libs/v5/pkg/testing/docker"
	"github.com/formancehq/go-libs/v5/pkg/testing/platform/pgtesting"
)

// This test lives in package connect_test (external) to break the import
// cycle: pgtesting imports connect, so a connect-internal test cannot import
// pgtesting. The integration covers the contract pgxpool offers to the IAM
// helper -- BeforeConnect fires on every freshly created connection -- using
// the public WithPgxPoolBeforeConnect entry point. The IAM-specific wiring
// (token mint, password override, endpoint/user propagation) is covered by
// the offline unit tests in pgxpool_test.go.
func TestPgxPoolBeforeConnectFiresPerFreshConnection(t *testing.T) {
	t.Parallel()

	srv := pgtesting.CreatePostgresServer(t, docker.NewPool(t, logging.Testing()))
	db := srv.NewDatabase(t)

	var hookCount atomic.Int64
	ctx := context.Background()

	cfg, err := connect.BuildPgxPoolConfig(ctx, db.ConnString(),
		connect.WithPgxPoolBeforeConnect(func(_ context.Context, cc *pgx.ConnConfig) error {
			hookCount.Add(1)
			// Validate the hook sees a fully parsed ConnConfig -- this is what
			// the IAM hook relies on to derive the token's endpoint/user.
			require.Equal(t, srv.GetHost(), cc.Host)
			require.Equal(t, srv.GetUsername(), cc.User)
			return nil
		}),
	)
	require.NoError(t, err)

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	defer pool.Close()

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	var v int
	require.NoError(t, conn.QueryRow(ctx, "select 1").Scan(&v))
	require.Equal(t, 1, v)
	conn.Release()

	firstRound := hookCount.Load()
	require.GreaterOrEqual(t, firstRound, int64(1),
		"BeforeConnect must fire at least once for the first connection")

	// Pool.Reset destroys all existing connections; the next Acquire is
	// guaranteed to open a fresh socket and re-fire BeforeConnect. This is
	// the contract the IAM helper relies on for per-acquire token refresh.
	pool.Reset()

	conn2, err := pool.Acquire(ctx)
	require.NoError(t, err)
	require.NoError(t, conn2.QueryRow(ctx, "select 1").Scan(&v))
	require.Equal(t, 1, v)
	conn2.Release()

	require.Greater(t, hookCount.Load(), firstRound,
		"BeforeConnect must re-fire on a freshly created connection (post-Reset)")
}
