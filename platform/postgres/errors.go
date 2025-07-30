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

		var pge *pgconn.PgError
		switch {
		case errors.As(err, &pge):
			switch pge.Code {
			case "23502":
				return newErrNonNullValidationFailed(pge)
			case "25503":
				return newErrFkConstraintFailed(pge)
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
			case "P0001":
				return newErrRaisedException(pge)
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

type ErrFKConstraintFailed struct {
	err *pgconn.PgError
}

func (e ErrFKConstraintFailed) Error() string {
	return e.err.Error()
}

func (e ErrFKConstraintFailed) Unwrap() error {
	return e.err
}

func (e ErrFKConstraintFailed) GetConstraint() string {
	return e.err.ConstraintName
}

func newErrFkConstraintFailed(err *pgconn.PgError) ErrFKConstraintFailed {
	return ErrFKConstraintFailed{
		err: err,
	}
}

type ErrValidationFailed struct {
	err *pgconn.PgError
}

func (e ErrValidationFailed) Error() string {
	return e.err.Error()
}

func (e ErrValidationFailed) Unwrap() error {
	return e.err
}

func newErrNonNullValidationFailed(err *pgconn.PgError) ErrValidationFailed {
	return ErrValidationFailed{
		err: err,
	}
}

// ErrConstraintsFailed wraps 23505 pg error, meaning uniqueness constraint failed
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

type ErrRaisedException struct {
	err *pgconn.PgError
}

func (e ErrRaisedException) Error() string {
	return e.err.Error()
}

func (e ErrRaisedException) Is(err error) bool {
	_, ok := err.(ErrRaisedException)
	return ok
}

func (e ErrRaisedException) Unwrap() error {
	return e.err
}

func (e ErrRaisedException) GetMessage() string {
	return e.err.Message
}

func newErrRaisedException(err *pgconn.PgError) ErrRaisedException {
	return ErrRaisedException{
		err: err,
	}
}
