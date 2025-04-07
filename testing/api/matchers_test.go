package api

import (
	"errors"
	"testing"

	"github.com/formancehq/go-libs/v2/api"
	"github.com/stretchr/testify/require"
)

func TestHaveErrorCode(t *testing.T) {
	matcher := HaveErrorCode("test_error")
	require.NotNil(t, matcher, "Matcher should not be nil")
	require.Equal(t, "test_error", matcher.expected, "Expected error code should be set correctly")
}

func TestHaveErrorCodeMatcher_Match(t *testing.T) {
	matcher := HaveErrorCode("test_error")

	t.Run("with non-error input", func(t *testing.T) {
		success, err := matcher.Match("not an error")
		require.Error(t, err, "Should return error for non-error input")
		require.False(t, success, "Should not match non-error input")
		require.Contains(t, err.Error(), "expected input type error", "Error message should indicate wrong type")
	})

	t.Run("with regular error", func(t *testing.T) {
		regularErr := errors.New("regular error")
		success, err := matcher.Match(regularErr)
		require.NoError(t, err, "Should not return error for regular error input")
		require.False(t, success, "Should not match regular error")
	})

	t.Run("with matching error code", func(t *testing.T) {
		errorResponse := api.NewErrorResponse("test_error", "Test error message")
		success, err := matcher.Match(errorResponse)
		require.NoError(t, err, "Should not return error for error response input")
		require.True(t, success, "Should match error with correct code")
		require.Equal(t, "test_error", matcher.lastSeen, "Should set lastSeen field")
		require.Equal(t, "Test error message", matcher.lastSeenMessage, "Should set lastSeenMessage field")
	})

	t.Run("with non-matching error code", func(t *testing.T) {
		errorResponse := api.NewErrorResponse("other_error", "Other error message")
		success, err := matcher.Match(errorResponse)
		require.NoError(t, err, "Should not return error for error response input")
		require.False(t, success, "Should not match error with incorrect code")
		require.Equal(t, "other_error", matcher.lastSeen, "Should set lastSeen field")
		require.Equal(t, "Other error message", matcher.lastSeenMessage, "Should set lastSeenMessage field")
	})
}

func TestHaveErrorCodeMatcher_FailureMessage(t *testing.T) {
	matcher := HaveErrorCode("test_error")

	t.Run("with nil input", func(t *testing.T) {
		message := matcher.FailureMessage(nil)
		require.Contains(t, message, "error should have code test_error but is nil", "Failure message should mention nil input")
	})

	t.Run("with non-nil input", func(t *testing.T) {
		matcher.lastSeen = "other_error"
		matcher.lastSeenMessage = "Other error message"

		message := matcher.FailureMessage("some input")
		require.Contains(t, message, "error should have code test_error but have other_error", "Failure message should mention expected and actual codes")
		require.Contains(t, message, "Other error message", "Failure message should include the error message")
	})
}

func TestHaveErrorCodeMatcher_NegatedFailureMessage(t *testing.T) {
	matcher := HaveErrorCode("test_error")
	message := matcher.NegatedFailureMessage("some input")
	require.Contains(t, message, "error should not have code test_error", "Negated failure message should mention the code")
}
