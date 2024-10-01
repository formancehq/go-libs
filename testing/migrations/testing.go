package migrations

import (
	"context"
	"github.com/formancehq/go-libs/logging"
	"github.com/formancehq/go-libs/migrations"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"testing"
)

type HookFn func(ctx context.Context, t *testing.T, db bun.IDB)

type Hook struct {
	Before HookFn
	After  HookFn
}

type MigrationTest struct {
	migrator *migrations.Migrator
	hooks    map[int][]Hook
	db       bun.IDB
	t        *testing.T
}

func (mt *MigrationTest) Run() {
	ctx := logging.TestingContext()
	i := 0
	for {
		for _, hook := range mt.hooks[i] {
			if hook.Before != nil {
				hook.Before(ctx, mt.t, mt.db)
			}
		}

		more, err := mt.migrator.UpByOne(ctx, mt.db)
		require.NoError(mt.t, err)

		for _, hook := range mt.hooks[i] {
			if hook.After != nil {
				hook.After(ctx, mt.t, mt.db)
			}
		}

		i++

		if !more {
			break
		}
	}
}

func (mt *MigrationTest) Append(i int, hook Hook) {
	mt.hooks[i] = append(mt.hooks[i], hook)
}

func NewMigrationTest(t *testing.T, migrator *migrations.Migrator, db bun.IDB) *MigrationTest {
	return &MigrationTest{
		migrator: migrator,
		hooks:    map[int][]Hook{},
		t:        t,
		db:       db,
	}
}
