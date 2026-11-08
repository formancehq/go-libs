//go:build it

package migrations

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestGetMigrationsLimitsToRegisteredMigrations(t *testing.T) {
	t.Parallel()
	schema := uuid.NewString()[:8]
	migration := Migration{Up: func(context.Context, bun.IDB) error { return nil }}
	writer := NewMigrator(bunDB, WithSchema(schema))
	writer.RegisterMigrations(migration, migration, migration)
	require.NoError(t, writer.Up(t.Context()))
	reader := NewMigrator(bunDB, WithSchema(schema))
	reader.RegisterMigrations(migration, migration)
	infos, err := reader.GetMigrations(t.Context())
	require.NoError(t, err)
	require.Len(t, infos, 2)
}
