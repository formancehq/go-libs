//go:build it

package storage_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/messaging/publish/circuit/storage"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	bunconnect "github.com/formancehq/go-libs/v5/pkg/storage/bun/connect"
	"github.com/formancehq/go-libs/v5/pkg/testing/docker"
	"github.com/formancehq/go-libs/v5/pkg/testing/platform/pgtesting"
)

func TestListRespectsStorageLimit(t *testing.T) {
	t.Parallel()
	srv := pgtesting.CreatePostgresServer(t, docker.NewPool(t, logging.Testing()))
	database := srv.NewDatabase(t)
	db, err := bunconnect.OpenSQLDB(t.Context(), database.ConnectionOptions())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, storage.Migrate(t.Context(), "public", db))
	store := storage.New("public", db, 2)
	for _, topic := range []string{"first", "second", "third"} {
		require.NoError(t, store.Insert(t.Context(), topic, []byte(topic), nil))
	}
	rows, err := store.List(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "first", rows[0].Topic)
	require.Equal(t, "second", rows[1].Topic)
	for _, limit := range []int{0, -1} {
		rows, err := storage.New("public", db, limit).List(t.Context())
		require.NoError(t, err)
		require.Len(t, rows, 3)
	}
}
