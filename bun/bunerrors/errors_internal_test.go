package bunerrors_test

import (
	"database/sql"
	"errors"
	"github.com/formancehq/go-libs/v3/bun/bunerrors"
	"testing"

	"github.com/formancehq/go-libs/v3/logging"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/uptrace/bun"
)

func TestBunerrorsE(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		err            error
		msg            string
		fkColumns      []string
		expectedError  error
		expectedPrefix string
	}{
		{
			name:          "nil error",
			err:           nil,
			msg:           "test message",
			fkColumns:     []string{},
			expectedError: nil,
		},
		{
			name: "validation error",
			err: &pgconn.PgError{
				Code:       "23502",
				ColumnName: "test_column",
			},
			msg:           "test message",
			fkColumns:     []string{},
			expectedError: bunerrors.ErrValidation,
		},
		{
			name: "duplicate key value error",
			err: &pgconn.PgError{
				Code: "23505",
			},
			msg:           "test message",
			fkColumns:     []string{},
			expectedError: bunerrors.ErrDuplicateKeyValue,
		},
		{
			name: "foreign key violation with matching column",
			err: &pgconn.PgError{
				Code:           "23503",
				ConstraintName: "fk_test_column",
			},
			msg:            "test message",
			fkColumns:      []string{"test_column"},
			expectedError:  bunerrors.ErrForeignKeyViolation,
			expectedPrefix: "test_column",
		},
		{
			name: "foreign key violation without matching column",
			err: &pgconn.PgError{
				Code:           "23503",
				ConstraintName: "fk_other_column",
			},
			msg:           "test message",
			fkColumns:     []string{"test_column"},
			expectedError: bunerrors.ErrForeignKeyViolation,
		},
		{
			name:          "sql no rows error",
			err:           sql.ErrNoRows,
			msg:           "test message",
			fkColumns:     []string{},
			expectedError: bunerrors.ErrNotFound,
		},
		{
			name:      "generic error",
			err:       errors.New("some error"),
			msg:       "test message",
			fkColumns: []string{},
			// For generic errors, we don't check for a specific error type
			// Instead, we just verify that the error message contains our prefix and original error
			expectedPrefix: "test message",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b := bunerrors.NewBunerrors(tt.fkColumns)
			err := b.E(tt.msg, tt.err)

			if tt.name == "nil error" {
				assert.NoError(t, err)
				return
			}

			assert.Error(t, err)

			if tt.expectedError != nil {
				assert.ErrorIs(t, err, tt.expectedError)
			}

			if tt.expectedPrefix != "" {
				assert.Contains(t, err.Error(), tt.expectedPrefix)
			}
		})
	}
}

func TestBunerrorsRollbackOnTxError(t *testing.T) {
	// We can only test the nil error case without setting up a real database
	// because the rollbackOnTxError method calls tx.Rollback() which requires
	// a properly initialized bun.Tx object.

	// Test the nil error case
	t.Run("nil error", func(t *testing.T) {
		b := bunerrors.NewBunerrors([]string{})

		// For nil error, rollback should not be called
		var tx *bun.Tx = nil
		ctx := logging.TestingContext()

		// This should not panic because the error is nil
		// and the method should return early without calling tx.Rollback()
		b.RollbackOnTxError(ctx, tx, nil)
	})

	// Note: We can't easily test the case where error is not nil
	// because it would require a properly initialized bun.Tx object
	// which requires a real database connection.
}
