package migrations

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/formancehq/go-libs/v2/pointer"
)

func TestMigrations(ctx context.Context, _fs embed.FS, migrator *Migrator) error {
	_, err := WalkMigrations(_fs, func(entry fs.DirEntry) (*struct{}, error) {
		before, err := TemplateSQLFile(_fs, migrator.GetSchema(), entry.Name(), "up_tests_before.sql", nil)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if err == nil {
			_, err = migrator.db.ExecContext(ctx, before)
			if err != nil {
				return nil, fmt.Errorf("executing pre migration script: %s", entry.Name())
			}
		}

		if err := migrator.UpByOne(ctx); err != nil {
			switch {
			case errors.Is(err, ErrAlreadyUpToDate):
				return nil, nil
			default:
				return nil, err
			}
		}

		after, err := TemplateSQLFile(_fs, migrator.GetSchema(), entry.Name(), "up_tests_after.sql", nil)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if err == nil {
			_, err = migrator.db.ExecContext(ctx, after)
			if err != nil {
				return nil, fmt.Errorf("executing post migration script: %s", entry.Name())
			}
		}

		return pointer.For(struct{}{}), nil
	})
	return err
}
