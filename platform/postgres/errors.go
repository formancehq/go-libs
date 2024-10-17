package postgres

import (
	"database/sql"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pkg/errors"
)

// ResolveError is an helper to wrap postgres errors into storage errors
func ResolveError(err error) error {
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}

		switch pge := err.(type) {
		case *pgconn.PgError:
			switch pge.Code {
			case "23505":
				return newErrConstraintsFailed(pge)
			case "53300":
				return newErrTooManyClient(err)
			case "40P01":
				return ErrDeadlockDetected
			case "40001":
				return ErrSerialization
			case "42P01":
				return ErrMissingTable
			case "3F000":
				return ErrMissingSchema
			}
		}

		return err
	}

	return nil
}

var (
	ErrNotFound         = errors.New("not found")
	ErrDeadlockDetected = errors.New("deadlock detected")
	ErrSerialization    = errors.New("serialization error")
	ErrMissingTable     = errors.New("missing table")
	ErrMissingSchema    = errors.New("missing schema")
)

func IsNotFoundError(err error) bool {
	return errors.Is(err, ErrNotFound)
}

type ErrConstraintsFailed struct {
	err *pgconn.PgError
}

func (e ErrConstraintsFailed) Error() string {
	return e.err.Error()
}

func (e ErrConstraintsFailed) Is(err error) bool {
	_, ok := err.(ErrConstraintsFailed)
	return ok
}

func (e ErrConstraintsFailed) Unwrap() error {
	return e.err
}

func (e ErrConstraintsFailed) GetConstraint() string {
	return e.err.ConstraintName
}

func newErrConstraintsFailed(err *pgconn.PgError) ErrConstraintsFailed {
	return ErrConstraintsFailed{
		err: err,
	}
}

type ErrTooManyClient struct {
	err error
}

func (e ErrTooManyClient) Error() string {
	return e.err.Error()
}

func newErrTooManyClient(err error) ErrTooManyClient {
	return ErrTooManyClient{
		err: err,
	}
}
