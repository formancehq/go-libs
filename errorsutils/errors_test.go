package errorsutils_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/formancehq/go-libs/v2/errorsutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorWithExitCode(t *testing.T) {
	t.Run("NewErrorWithExitCode", func(t *testing.T) {
		// Test with a simple error
		originalErr := errors.New("test error")
		exitCode := 42

		errWithCode := errorsutils.NewErrorWithExitCode(originalErr, exitCode)

		assert.NotNil(t, errWithCode)
		assert.Equal(t, originalErr, errWithCode.Err)
		assert.Equal(t, exitCode, errWithCode.ExitCode)

		// Test with nil error
		nilErr := errorsutils.NewErrorWithExitCode(nil, exitCode)

		assert.NotNil(t, nilErr)
		assert.Nil(t, nilErr.Err)
		assert.Equal(t, exitCode, nilErr.ExitCode)
	})

	t.Run("Unwrap", func(t *testing.T) {
		// Test unwrapping a wrapped error
		originalErr := errors.New("test error")
		exitCode := 42

		errWithCode := errorsutils.NewErrorWithExitCode(originalErr, exitCode)
		unwrappedErr := errWithCode.Unwrap()

		assert.Equal(t, originalErr, unwrappedErr)

		// Test unwrapping with nil error
		nilErr := errorsutils.NewErrorWithExitCode(nil, exitCode)
		unwrappedNilErr := nilErr.Unwrap()

		assert.Nil(t, unwrappedNilErr)
	})

	t.Run("Error", func(t *testing.T) {
		// Test error message formatting
		originalErr := errors.New("test error")
		exitCode := 42

		errWithCode := errorsutils.NewErrorWithExitCode(originalErr, exitCode)
		errorMsg := errWithCode.Error()

		expectedMsg := fmt.Sprintf("error with exit code '%v': %d", originalErr, exitCode)
		assert.Equal(t, expectedMsg, errorMsg)

		// Test with nil error
		nilErr := errorsutils.NewErrorWithExitCode(nil, exitCode)
		nilErrorMsg := nilErr.Error()

		expectedNilMsg := fmt.Sprintf("error with exit code '%v': %d", nil, exitCode)
		assert.Equal(t, expectedNilMsg, nilErrorMsg)
	})

	t.Run("Is", func(t *testing.T) {
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
		assert.True(t, errWithCode.Is(otherErr))

		// Test with different error type
		plainErr := errors.New("plain error")
		assert.False(t, errWithCode.Is(plainErr))
	})

	t.Run("IsErrorWithExitCode", func(t *testing.T) {
		// Test with ErrorWithExitCode
		originalErr := errors.New("test error")
		exitCode := 42

		errWithCode := errorsutils.NewErrorWithExitCode(originalErr, exitCode)

		assert.True(t, errorsutils.IsErrorWithExitCode(errWithCode))

		// Test with wrapped ErrorWithExitCode
		wrappedErr := fmt.Errorf("wrapped: %w", errWithCode)
		assert.True(t, errorsutils.IsErrorWithExitCode(wrappedErr))

		// Test with plain error
		plainErr := errors.New("plain error")
		assert.False(t, errorsutils.IsErrorWithExitCode(plainErr))

		// Test with nil
		assert.False(t, errorsutils.IsErrorWithExitCode(nil))
	})

	t.Run("Error Wrapping and Unwrapping", func(t *testing.T) {
		// Create a chain of wrapped errors
		baseErr := errors.New("base error")
		exitCode := 42

		errWithCode := errorsutils.NewErrorWithExitCode(baseErr, exitCode)
		wrappedOnce := fmt.Errorf("wrapped once: %w", errWithCode)
		wrappedTwice := fmt.Errorf("wrapped twice: %w", wrappedOnce)

		// Test unwrapping through the chain
		assert.True(t, errors.Is(wrappedTwice, errWithCode))
		assert.True(t, errors.Is(wrappedTwice, baseErr))

		// Test that IsErrorWithExitCode works through the chain
		assert.True(t, errorsutils.IsErrorWithExitCode(wrappedTwice))

		// Test extracting the original error
		var extractedErrWithCode *errorsutils.ErrorWithExitCode
		require.True(t, errors.As(wrappedTwice, &extractedErrWithCode))
		assert.Equal(t, baseErr, extractedErrWithCode.Err)
		assert.Equal(t, exitCode, extractedErrWithCode.ExitCode)
	})
}
