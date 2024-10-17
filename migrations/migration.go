package migrations

import (
	"context"

	"github.com/uptrace/bun"
)

type Migration struct {
	Name string
	Up   func(ctx context.Context, db bun.IDB) error
}
