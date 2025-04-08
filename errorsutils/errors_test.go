package errorsutils_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/formancehq/go-libs/v3/errorsutils"
	"github.com/stretchr/testify/require"
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

		expectedMsg := fmt.Sprintf("error with exit code '%v': %d", originalErr, exitCode)
		require.Equal(t, expectedMsg, errorMsg)

		// Test with nil error
		nilErr := errorsutils.NewErrorWithExitCode(nil, exitCode)
		nilErrorMsg := nilErr.Error()

		expectedNilMsg := fmt.Sprintf("error with exit code '%v': %d", nil, exitCode)
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
