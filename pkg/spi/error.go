package spi

import "errors"

// UserError marks a node failure that is caused by the flow author or the
// system they are calling — an unreachable or misconfigured target endpoint,
// invalid node input, a failed assertion — rather than by a fault in the runner
// itself.
//
// It carries a stable Code and a user-facing Message that node results surface
// to the caller (error_code / error_message), and it preserves the underlying
// Cause for errors.Is/errors.As. The engine logs UserErrors at debug, reserving
// error level for genuine runner faults, because a UserError is an expected
// outcome already reported back to the user in the execution result.
type UserError struct {
	Code    string
	Message string
	Cause   error
}

// NewUserError builds a UserError with a stable code, a user-facing message, and
// the underlying cause (which may be nil).
func NewUserError(code, message string, cause error) *UserError {
	return &UserError{Code: code, Message: message, Cause: cause}
}

func (e *UserError) Error() string {
	switch {
	case e.Message != "" && e.Cause != nil:
		return e.Message + ": " + e.Cause.Error()
	case e.Message != "":
		return e.Message
	case e.Cause != nil:
		return e.Cause.Error()
	default:
		return e.Code
	}
}

func (e *UserError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// AsUserError reports whether err is (or wraps) a UserError, returning it when so.
func AsUserError(err error) (*UserError, bool) {
	var target *UserError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}
