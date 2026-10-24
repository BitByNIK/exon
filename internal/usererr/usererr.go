// Package usererr defines the error type whose text is shown to the user.
// Any other error is treated as internal: its details go to the log only.
package usererr

import "errors"

// Error is a problem the user can act on: what went wrong and what to do about it.
type Error struct {
	Msg  string // "Homebrew not found"
	Hint string // "Install it: /bin/bash -c ..."; may be empty
	Err  error  // underlying cause, for the log; may be nil
}

// New returns an Error with the given message, hint and cause.
func New(msg, hint string, err error) *Error {
	return &Error{Msg: msg, Hint: hint, Err: err}
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Msg + ": " + e.Err.Error()
	}
	return e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

// As returns the first *Error in err's chain, if any.
func As(err error) (*Error, bool) {
	var ue *Error
	ok := errors.As(err, &ue)
	return ue, ok
}
