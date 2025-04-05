package postgres

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestResolveError_Nil(t *testing.T) {
	err := ResolveError(nil)
	require.NoError(t, err, "Nil error devrait être résolu en nil")
}

func TestResolveError_NoRows(t *testing.T) {
	err := ResolveError(sql.ErrNoRows)
	require.ErrorIs(t, err, ErrNotFound, "sql.ErrNoRows devrait être résolu en ErrNotFound")
}

func TestResolveError_ConstraintsFailed(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:           "23505",
		ConstraintName: "test_constraint",
	}
	
	err := ResolveError(pgErr)
	require.IsType(t, ErrConstraintsFailed{}, err, "L'erreur devrait être de type ErrConstraintsFailed")
	
	constraintErr, ok := err.(ErrConstraintsFailed)
	require.True(t, ok, "L'erreur devrait pouvoir être convertie en ErrConstraintsFailed")
	require.Equal(t, "test_constraint", constraintErr.GetConstraint(), "Le nom de la contrainte devrait être correct")
}

func TestResolveError_TooManyClient(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code: "53300",
	}
	
	err := ResolveError(pgErr)
	require.IsType(t, ErrTooManyClient{}, err, "L'erreur devrait être de type ErrTooManyClient")
}

func TestResolveError_DeadlockDetected(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code: "40P01",
	}
	
	err := ResolveError(pgErr)
	require.ErrorIs(t, err, ErrDeadlockDetected, "L'erreur devrait être ErrDeadlockDetected")
}

func TestResolveError_Serialization(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code: "40001",
	}
	
	err := ResolveError(pgErr)
	require.ErrorIs(t, err, ErrSerialization, "L'erreur devrait être ErrSerialization")
}

func TestResolveError_MissingTable(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code: "42P01",
	}
	
	err := ResolveError(pgErr)
	require.ErrorIs(t, err, ErrMissingTable, "L'erreur devrait être ErrMissingTable")
}

func TestResolveError_MissingSchema(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code: "3F000",
	}
	
	err := ResolveError(pgErr)
	require.ErrorIs(t, err, ErrMissingSchema, "L'erreur devrait être ErrMissingSchema")
}

func TestResolveError_RaisedException(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:    "P0001",
		Message: "test message",
	}
	
	err := ResolveError(pgErr)
	require.IsType(t, ErrRaisedException{}, err, "L'erreur devrait être de type ErrRaisedException")
	
	raisedErr, ok := err.(ErrRaisedException)
	require.True(t, ok, "L'erreur devrait pouvoir être convertie en ErrRaisedException")
	require.Equal(t, "test message", raisedErr.GetMessage(), "Le message d'erreur devrait être correct")
}

func TestResolveError_UnknownPgError(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code: "UNKNOWN",
	}
	
	err := ResolveError(pgErr)
	require.Same(t, pgErr, err, "Une erreur postgres inconnue devrait être retournée telle quelle")
}

func TestResolveError_OtherError(t *testing.T) {
	originalErr := errors.New("autre erreur")
	err := ResolveError(originalErr)
	require.Same(t, originalErr, err, "Une erreur non postgres devrait être retournée telle quelle")
}

func TestIsNotFoundError(t *testing.T) {
	require.True(t, IsNotFoundError(ErrNotFound), "ErrNotFound devrait être détecté comme une erreur 'not found'")
	require.False(t, IsNotFoundError(errors.New("autre erreur")), "Une autre erreur ne devrait pas être détectée comme 'not found'")
}

func TestErrConstraintsFailed_Error(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:           "23505",
		Message:        "test message",
		ConstraintName: "test_constraint",
	}
	
	err := newErrConstraintsFailed(pgErr)
	require.Equal(t, pgErr.Error(), err.Error(), "Le message d'erreur devrait être celui de l'erreur postgres")
}

func TestErrConstraintsFailed_Is(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code: "23505",
	}
	
	err := newErrConstraintsFailed(pgErr)
	
	otherErr := ErrConstraintsFailed{}
	require.True(t, err.Is(otherErr), "Is devrait retourner true pour une erreur du même type")
	
	otherTypeErr := errors.New("autre erreur")
	require.False(t, err.Is(otherTypeErr), "Is devrait retourner false pour une erreur d'un autre type")
}

func TestErrConstraintsFailed_Unwrap(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code: "23505",
	}
	
	err := newErrConstraintsFailed(pgErr)
	unwrappedErr := err.Unwrap()
	
	require.Same(t, pgErr, unwrappedErr, "Unwrap devrait retourner l'erreur postgres originale")
}

func TestErrTooManyClient_Error(t *testing.T) {
	originalErr := errors.New("trop de clients")
	err := newErrTooManyClient(originalErr)
	
	require.Equal(t, originalErr.Error(), err.Error(), "Le message d'erreur devrait être celui de l'erreur originale")
}

func TestErrRaisedException_Error(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:    "P0001",
		Message: "test message",
	}
	
	err := newErrRaisedException(pgErr)
	require.Equal(t, pgErr.Error(), err.Error(), "Le message d'erreur devrait être celui de l'erreur postgres")
}

func TestErrRaisedException_Is(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code: "P0001",
	}
	
	err := newErrRaisedException(pgErr)
	
	otherErr := ErrRaisedException{}
	require.True(t, err.Is(otherErr), "Is devrait retourner true pour une erreur du même type")
	
	otherTypeErr := errors.New("autre erreur")
	require.False(t, err.Is(otherTypeErr), "Is devrait retourner false pour une erreur d'un autre type")
}

func TestErrRaisedException_Unwrap(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code: "P0001",
	}
	
	err := newErrRaisedException(pgErr)
	unwrappedErr := err.Unwrap()
	
	require.Same(t, pgErr, unwrappedErr, "Unwrap devrait retourner l'erreur postgres originale")
}
