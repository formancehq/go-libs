package errors

import (
	"errors"
	"fmt"
)

type ErrorWithExitCode struct {
	Err      error
	ExitCode int
}

func (e ErrorWithExitCode) Unwrap() error {
	return e.Err
}

func (e ErrorWithExitCode) Error() string {
	return fmt.Sprintf("error with exit code '%d': %v", e.ExitCode, e.Err)
}

func (e ErrorWithExitCode) Is(err error) bool {
	switch err.(type) {
	case ErrorWithExitCode, *ErrorWithExitCode:
		return true
	default:
		return false
	}
}

func IsErrorWithExitCode(err error) bool {
	return errors.Is(err, ErrorWithExitCode{})
}

// ExitCodeFromError extracts the exit code carried by an ErrorWithExitCode
// anywhere in the wrap chain of err. It returns the exit code and true if
// such an error is found, or 0 and false otherwise.
//
// Unlike a direct type assertion, it survives intermediate wrapping
// (e.g. fmt.Errorf("...: %w", err)) and matches both the pointer and value
// forms of ErrorWithExitCode.
func ExitCodeFromError(err error) (int, bool) {
	var ptr *ErrorWithExitCode
	if errors.As(err, &ptr) {
		return ptr.ExitCode, true
	}

	var value ErrorWithExitCode
	if errors.As(err, &value) {
		return value.ExitCode, true
	}

	return 0, false
}

func NewErrorWithExitCode(err error, exitCode int) *ErrorWithExitCode {
	return &ErrorWithExitCode{
		Err:      err,
		ExitCode: exitCode,
	}
}
