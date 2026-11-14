package core

import "errors"

const (
	ExitCodeGeneric    = 1
	ExitCodeValidation = 2
)

// ExitCodeError is a command error carrying an explicit process exit code.
type ExitCodeError interface {
	error
	ExitCode() int
}

type exitCodeError struct {
	message  string
	exitCode int
}

func (e *exitCodeError) Error() string {
	return e.message
}

func (e *exitCodeError) ExitCode() int {
	return e.exitCode
}

// NewExitCodeError creates a typed CLI error with deterministic exit behavior.
func NewExitCodeError(code int, message string) error {
	return &exitCodeError{
		message:  message,
		exitCode: code,
	}
}

// AsExitCodeError extracts an ExitCodeError from an error chain.
func AsExitCodeError(err error) (ExitCodeError, bool) {
	if err == nil {
		return nil, false
	}
	// exec.ExitError also has an ExitCode method. Only errors created through
	// NewExitCodeError represent an intentional CLI exit code; treating a
	// wrapped subprocess failure as one hides its surrounding diagnostics.
	var target *exitCodeError
	if !errors.As(err, &target) {
		return nil, false
	}
	return target, true
}
