package errors_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	errorsutils "github.com/formancehq/go-libs/v5/pkg/errors"
)

func TestErrorWithExitCode(t *testing.T) {
	t.Parallel()
	t.Run("NewErrorWithExitCode", func(t *testing.T) {
		t.Parallel()
		// Test with a simple error
		originalErr := errors.New("test error")
		exitCode := 42

		errWithCode := errorsutils.NewErrorWithExitCode(originalErr, exitCode)

		require.NotNil(t, errWithCode)
		require.Equal(t, originalErr, errWithCode.Err)
		require.Equal(t, exitCode, errWithCode.ExitCode)

		// Test with nil error
		nilErr := errorsutils.NewErrorWithExitCode(nil, exitCode)

		require.NotNil(t, nilErr)
		require.Nil(t, nilErr.Err)
		require.Equal(t, exitCode, nilErr.ExitCode)
	})

	t.Run("Unwrap", func(t *testing.T) {
		t.Parallel()
		// Test unwrapping a wrapped error
		originalErr := errors.New("test error")
		exitCode := 42

		errWithCode := errorsutils.NewErrorWithExitCode(originalErr, exitCode)
		unwrappedErr := errWithCode.Unwrap()

		require.Equal(t, originalErr, unwrappedErr)

		// Test unwrapping with nil error
		nilErr := errorsutils.NewErrorWithExitCode(nil, exitCode)
		unwrappedNilErr := nilErr.Unwrap()

		require.Nil(t, unwrappedNilErr)
	})

	t.Run("Error", func(t *testing.T) {
		t.Parallel()
		// Test error message formatting
		originalErr := errors.New("test error")
		exitCode := 42

		errWithCode := errorsutils.NewErrorWithExitCode(originalErr, exitCode)
		errorMsg := errWithCode.Error()

		expectedMsg := fmt.Sprintf("error with exit code '%d': %v", exitCode, originalErr)
		require.Equal(t, expectedMsg, errorMsg)

		// Test with nil error
		nilErr := errorsutils.NewErrorWithExitCode(nil, exitCode)
		nilErrorMsg := nilErr.Error()

		expectedNilMsg := fmt.Sprintf("error with exit code '%d': %v", exitCode, nil)
		require.Equal(t, expectedNilMsg, nilErrorMsg)
	})

	t.Run("Is", func(t *testing.T) {
		t.Parallel()
		// Test Is method with same type
		originalErr := errors.New("test error")
		exitCode := 42

		errWithCode := errorsutils.NewErrorWithExitCode(originalErr, exitCode)

		// Create another ErrorWithExitCode for comparison
		otherErr := errorsutils.ErrorWithExitCode{
			Err:      errors.New("other error"),
			ExitCode: 100,
		}

		// Test Is method
		require.True(t, errWithCode.Is(otherErr))

		// Test with different error type
		plainErr := errors.New("plain error")
		require.False(t, errWithCode.Is(plainErr))
	})

	t.Run("errors.Is with pointer target", func(t *testing.T) {
		t.Parallel()
		// errors.Is must match when the target is the pointer form returned
		// by NewErrorWithExitCode, even through wrapping
		errWithCode := errorsutils.NewErrorWithExitCode(errors.New("test error"), 42)

		require.True(t, errors.Is(errWithCode, errorsutils.NewErrorWithExitCode(nil, 0)))

		wrapped := fmt.Errorf("wrapped: %w", errWithCode)
		require.True(t, errors.Is(wrapped, errorsutils.NewErrorWithExitCode(nil, 0)))

		// Value target keeps working
		require.True(t, errors.Is(wrapped, errorsutils.ErrorWithExitCode{}))

		// Plain error never matches
		require.False(t, errors.Is(errors.New("plain error"), errorsutils.NewErrorWithExitCode(nil, 0)))
	})

	t.Run("ExitCodeFromError", func(t *testing.T) {
		t.Parallel()
		errWithCode := errorsutils.NewErrorWithExitCode(errors.New("base error"), 42)

		// Direct error
		exitCode, ok := errorsutils.ExitCodeFromError(errWithCode)
		require.True(t, ok)
		require.Equal(t, 42, exitCode)

		// Double-wrapped error: this is the regression for the old
		// direct type assertion which panicked on wrapped errors
		wrappedTwice := fmt.Errorf("wrapped twice: %w", fmt.Errorf("wrapped once: %w", errWithCode))
		require.NotPanics(t, func() {
			exitCode, ok = errorsutils.ExitCodeFromError(wrappedTwice)
		})
		require.True(t, ok)
		require.Equal(t, 42, exitCode)

		// Value form in the chain
		valueErr := fmt.Errorf("wrapped: %w", errorsutils.ErrorWithExitCode{Err: errors.New("base"), ExitCode: 7})
		exitCode, ok = errorsutils.ExitCodeFromError(valueErr)
		require.True(t, ok)
		require.Equal(t, 7, exitCode)

		// Plain error
		exitCode, ok = errorsutils.ExitCodeFromError(errors.New("plain error"))
		require.False(t, ok)
		require.Equal(t, 0, exitCode)

		// Nil error
		exitCode, ok = errorsutils.ExitCodeFromError(nil)
		require.False(t, ok)
		require.Equal(t, 0, exitCode)
	})

	t.Run("IsErrorWithExitCode", func(t *testing.T) {
		t.Parallel()
		// Test with ErrorWithExitCode
		originalErr := errors.New("test error")
		exitCode := 42

		errWithCode := errorsutils.NewErrorWithExitCode(originalErr, exitCode)

		require.True(t, errorsutils.IsErrorWithExitCode(errWithCode))

		// Test with wrapped ErrorWithExitCode
		wrappedErr := fmt.Errorf("wrapped: %w", errWithCode)
		require.True(t, errorsutils.IsErrorWithExitCode(wrappedErr))

		// Test with plain error
		plainErr := errors.New("plain error")
		require.False(t, errorsutils.IsErrorWithExitCode(plainErr))

		// Test with nil
		require.False(t, errorsutils.IsErrorWithExitCode(nil))
	})

	t.Run("Error Wrapping and Unwrapping", func(t *testing.T) {
		t.Parallel()
		// Create a chain of wrapped errors
		baseErr := errors.New("base error")
		exitCode := 42

		errWithCode := errorsutils.NewErrorWithExitCode(baseErr, exitCode)
		wrappedOnce := fmt.Errorf("wrapped once: %w", errWithCode)
		wrappedTwice := fmt.Errorf("wrapped twice: %w", wrappedOnce)

		// Test unwrapping through the chain
		require.True(t, errors.Is(wrappedTwice, errWithCode))
		require.True(t, errors.Is(wrappedTwice, baseErr))

		// Test that IsErrorWithExitCode works through the chain
		require.True(t, errorsutils.IsErrorWithExitCode(wrappedTwice))

		// Test extracting the original error
		var extractedErrWithCode *errorsutils.ErrorWithExitCode
		require.True(t, errors.As(wrappedTwice, &extractedErrWithCode))
		require.Equal(t, baseErr, extractedErrWithCode.Err)
		require.Equal(t, exitCode, extractedErrWithCode.ExitCode)
	})
}
