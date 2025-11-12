package postgres_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v3/platform/postgres"
)

func TestResolveError(t *testing.T) {
	t.Parallel()

	t.Run("NilError", func(t *testing.T) {
		t.Parallel()
		err := postgres.ResolveError(nil)
		require.NoError(t, err)
	})

	t.Run("NotFoundError", func(t *testing.T) {
		t.Parallel()
		err := postgres.ResolveError(sql.ErrNoRows)
		require.ErrorIs(t, err, postgres.ErrNotFound)
	})

	t.Run("ValidationFailedError", func(t *testing.T) {
		t.Parallel()
		tests := map[string]*pgconn.PgError{
			"not-null constraint violation": {
				Code:    "23502",
				Message: "not-null constraint violation",
			},
			"invalid input syntax for type": {
				Code:    "22P02",
				Message: "invalid input syntax for type",
			},
		}

		for name, tt := range tests {
			name := name
			tt := tt
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				err := postgres.ResolveError(tt)
				require.Error(t, err)

				var validationErr postgres.ErrValidationFailed
				require.ErrorAs(t, err, &validationErr)
				require.Equal(t, tt.Error(), validationErr.Error())
				require.Equal(t, tt, validationErr.Unwrap())
			})
		}
	})

	t.Run("FKConstraintFailedError", func(t *testing.T) {
		t.Parallel()
		pgErr := &pgconn.PgError{
			Code:           "23503",
			Message:        "foreign key constraint violation",
			ConstraintName: "users_role_id_fkey",
		}
		err := postgres.ResolveError(pgErr)
		require.Error(t, err)

		var fkErr postgres.ErrFKConstraintFailed
		require.ErrorAs(t, err, &fkErr)
		require.Equal(t, "users_role_id_fkey", fkErr.GetConstraint())
		require.Equal(t, pgErr.Error(), fkErr.Error())
		require.Equal(t, pgErr, fkErr.Unwrap())
	})

	t.Run("ConstraintsFailedError", func(t *testing.T) {
		t.Parallel()
		pgErr := &pgconn.PgError{
			Code:           "23505",
			Message:        "unique constraint violation",
			ConstraintName: "users_email_key",
		}
		err := postgres.ResolveError(pgErr)
		require.Error(t, err)

		var constraintsErr postgres.ErrConstraintsFailed
		require.ErrorAs(t, err, &constraintsErr)
		require.Equal(t, "users_email_key", constraintsErr.GetConstraint())
		require.Equal(t, pgErr.Error(), constraintsErr.Error())
		require.Equal(t, pgErr, constraintsErr.Unwrap())
	})

	t.Run("TooManyClientError", func(t *testing.T) {
		t.Parallel()
		pgErr := &pgconn.PgError{
			Code:    "53300",
			Message: "too many connections",
		}
		err := postgres.ResolveError(pgErr)
		require.Error(t, err)

		var tooManyClientErr postgres.ErrTooManyClient
		require.ErrorAs(t, err, &tooManyClientErr)
		require.Equal(t, pgErr.Error(), tooManyClientErr.Error())
	})

	t.Run("DeadlockDetectedError", func(t *testing.T) {
		t.Parallel()
		pgErr := &pgconn.PgError{
			Code:    "40P01",
			Message: "deadlock detected",
		}
		err := postgres.ResolveError(pgErr)
		require.ErrorIs(t, err, postgres.ErrDeadlockDetected)
	})

	t.Run("SerializationError", func(t *testing.T) {
		t.Parallel()
		pgErr := &pgconn.PgError{
			Code:    "40001",
			Message: "serialization failure",
		}
		err := postgres.ResolveError(pgErr)
		require.ErrorIs(t, err, postgres.ErrSerialization)
	})

	t.Run("MissingTableError", func(t *testing.T) {
		t.Parallel()
		pgErr := &pgconn.PgError{
			Code:    "42P01",
			Message: "relation does not exist",
		}
		err := postgres.ResolveError(pgErr)
		require.ErrorIs(t, err, postgres.ErrMissingTable)
	})

	t.Run("MissingSchemaError", func(t *testing.T) {
		t.Parallel()
		pgErr := &pgconn.PgError{
			Code:    "3F000",
			Message: "schema does not exist",
		}
		err := postgres.ResolveError(pgErr)
		require.ErrorIs(t, err, postgres.ErrMissingSchema)
	})

	t.Run("RaisedExceptionError", func(t *testing.T) {
		t.Parallel()
		pgErr := &pgconn.PgError{
			Code:    "P0001",
			Message: "custom exception message",
		}
		err := postgres.ResolveError(pgErr)
		require.Error(t, err)

		var raisedErr postgres.ErrRaisedException
		require.ErrorAs(t, err, &raisedErr)
		require.Equal(t, "custom exception message", raisedErr.GetMessage())
		require.Equal(t, pgErr.Error(), raisedErr.Error())
		require.Equal(t, pgErr, raisedErr.Unwrap())
	})

	t.Run("OtherError", func(t *testing.T) {
		t.Parallel()
		originalErr := errors.New("some other error")
		err := postgres.ResolveError(originalErr)
		require.Equal(t, originalErr, err)
	})
}

func TestIsNotFoundError(t *testing.T) {
	t.Parallel()

	t.Run("WithNotFoundError", func(t *testing.T) {
		t.Parallel()
		require.True(t, postgres.IsNotFoundError(postgres.ErrNotFound))
	})

	t.Run("WithWrappedNotFoundError", func(t *testing.T) {
		t.Parallel()
		wrappedErr := errors.New("wrapped not found error")
		err := errors.Join(wrappedErr, postgres.ErrNotFound)
		require.True(t, postgres.IsNotFoundError(err))
	})

	t.Run("WithOtherError", func(t *testing.T) {
		t.Parallel()
		err := errors.New("some other error")
		require.False(t, postgres.IsNotFoundError(err))
	})

	t.Run("WithNilError", func(t *testing.T) {
		t.Parallel()
		require.False(t, postgres.IsNotFoundError(nil))
	})
}

func TestErrConstraintsFailed(t *testing.T) {
	t.Parallel()

	t.Run("Is", func(t *testing.T) {
		t.Parallel()
		pgErr := &pgconn.PgError{
			Code:           "23505",
			Message:        "unique constraint violation",
			ConstraintName: "users_email_key",
		}
		constraintsErr := postgres.ErrConstraintsFailed{} // Create a zero value for comparison
		err := postgres.ResolveError(pgErr)

		require.True(t, errors.Is(err, constraintsErr))
	})
}

func TestErrRaisedException(t *testing.T) {
	t.Parallel()

	t.Run("Is", func(t *testing.T) {
		t.Parallel()
		pgErr := &pgconn.PgError{
			Code:    "P0001",
			Message: "custom exception message",
		}
		raisedErr := postgres.ErrRaisedException{} // Create a zero value for comparison
		err := postgres.ResolveError(pgErr)

		require.True(t, errors.Is(err, raisedErr))
	})
}

func TestErrTooManyClient(t *testing.T) {
	t.Parallel()

	t.Run("Unwrap", func(t *testing.T) {
		t.Parallel()
		originalErr := &pgconn.PgError{
			Code:    "53300",
			Message: "too many connections",
		}
		err := postgres.ResolveError(originalErr)

		var tooManyClientErr postgres.ErrTooManyClient
		require.ErrorAs(t, err, &tooManyClientErr)

		// Since ErrTooManyClient doesn't have an Unwrap method, we can't test it directly
		// But we can verify the Error method returns the original error message
		require.Equal(t, originalErr.Error(), tooManyClientErr.Error())
	})
}

func TestErrFKConstraintFailed(t *testing.T) {
	t.Parallel()

	t.Run("Is", func(t *testing.T) {
		t.Parallel()
		pgErr := &pgconn.PgError{
			Code:           "23503",
			Message:        "foreign key constraint violation",
			ConstraintName: "users_role_id_fkey",
		}
		fkErr := postgres.ErrFKConstraintFailed{} // Create a zero value for comparison
		err := postgres.ResolveError(pgErr)

		require.True(t, errors.Is(err, fkErr))
	})
}

func TestErrValidationFailed(t *testing.T) {
	t.Parallel()

	t.Run("Is", func(t *testing.T) {
		t.Parallel()
		pgErr := &pgconn.PgError{
			Code:    "23502",
			Message: "not-null constraint violation",
		}
		validationErr := postgres.ErrValidationFailed{} // Create a zero value for comparison
		err := postgres.ResolveError(pgErr)

		require.True(t, errors.Is(err, validationErr))
	})
}

func TestConstructorFunctions(t *testing.T) {
	t.Parallel()

	t.Run("newErrFkConstraintFailed", func(t *testing.T) {
		t.Parallel()
		pgErr := &pgconn.PgError{
			Code:           "23503",
			Message:        "foreign key constraint violation",
			ConstraintName: "users_role_id_fkey",
		}
		err := postgres.ResolveError(pgErr)

		var fkErr postgres.ErrFKConstraintFailed
		require.ErrorAs(t, err, &fkErr)
		require.Equal(t, pgErr, fkErr.Unwrap())
		require.Equal(t, "users_role_id_fkey", fkErr.GetConstraint())
	})

	t.Run("newErrNonNullValidationFailed", func(t *testing.T) {
		t.Parallel()
		pgErr := &pgconn.PgError{
			Code:    "23502",
			Message: "not-null constraint violation",
		}
		err := postgres.ResolveError(pgErr)

		var validationErr postgres.ErrValidationFailed
		require.ErrorAs(t, err, &validationErr)
		require.Equal(t, pgErr, validationErr.Unwrap())
	})

	t.Run("newErrConstraintsFailed", func(t *testing.T) {
		t.Parallel()
		pgErr := &pgconn.PgError{
			Code:           "23505",
			Message:        "unique constraint violation",
			ConstraintName: "users_email_key",
		}
		err := postgres.ResolveError(pgErr)

		var constraintsErr postgres.ErrConstraintsFailed
		require.ErrorAs(t, err, &constraintsErr)
		require.Equal(t, pgErr, constraintsErr.Unwrap())
		require.Equal(t, "users_email_key", constraintsErr.GetConstraint())
	})

	t.Run("newErrRaisedException", func(t *testing.T) {
		t.Parallel()
		pgErr := &pgconn.PgError{
			Code:    "P0001",
			Message: "custom exception message",
		}
		err := postgres.ResolveError(pgErr)

		var raisedErr postgres.ErrRaisedException
		require.ErrorAs(t, err, &raisedErr)
		require.Equal(t, pgErr, raisedErr.Unwrap())
		require.Equal(t, "custom exception message", raisedErr.GetMessage())
	})
}
