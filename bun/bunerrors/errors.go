package bunerrors

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/formancehq/go-libs/v3/logging"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pkg/errors"
	"github.com/uptrace/bun"
	"strings"
)

type Bunerrors interface {
	E(msg string, err error) error
	RollbackOnTxError(ctx context.Context, tx *bun.Tx, err error)
}

var (
	ErrValidation        = errors.New("validation error")
	ErrNotFound          = errors.New("not found")
	ErrDuplicateKeyValue = errors.New("object already exists")
	// ErrForeignKeyViolation Don't want to expose the internal that is a foreign key violation to the
	// client through the API
	ErrForeignKeyViolation = errors.New("value not found")
)

type bunerrors struct {
	fKViolationColumns []string
}

func NewBunerrors(fkViolationColumns []string) Bunerrors {
	return &bunerrors{
		fKViolationColumns: fkViolationColumns,
	}
}

func (b *bunerrors) E(msg string, err error) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError

	if errors.As(err, &pgErr) && pgErr.Code == "23502" {
		return fmt.Errorf("%w on %q for %s", ErrValidation, pgErr.ColumnName, msg)
	}

	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrDuplicateKeyValue
	}

	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		for _, column := range b.fKViolationColumns {
			if strings.Contains(pgErr.ConstraintName, column) {
				return fmt.Errorf("%s: %w", column, ErrForeignKeyViolation)
			}
		}

		return ErrForeignKeyViolation
	}

	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}

	return fmt.Errorf("%s: %w", msg, err)
}

func (b *bunerrors) RollbackOnTxError(ctx context.Context, tx *bun.Tx, err error) {
	if err == nil {
		return
	}

	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		logging.FromContext(ctx).WithField("original_error", err.Error()).Errorf("failed to rollback transaction: %s", rollbackErr)
	}
}
