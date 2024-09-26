package postgres

import (
	"database/sql"

	"github.com/lib/pq"
	"github.com/pkg/errors"
)

// postgresError is an helper to wrap postgres errors into storage errors
func ResolveError(err error) error {
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}

		switch pge := err.(type) {
		case *pq.Error:
			switch pge.Code {
			case "23505":
				return newErrConstraintsFailed(pge)
			case "53300":
				return newErrTooManyClient(err)
			case "40P01":
				return ErrDeadlockDetected
			case "40001":
				return ErrSerialization
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
)

func IsNotFoundError(err error) bool {
	return errors.Is(err, ErrNotFound)
}

type ErrConstraintsFailed struct {
	err *pq.Error
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
	return e.err.Constraint
}

func newErrConstraintsFailed(err *pq.Error) ErrConstraintsFailed {
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
