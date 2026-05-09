package main

import "errors"

const (
	exitCodeGeneral          = 1
	exitCodeConfiguration    = 2
	exitCodeReplayFailure    = 3
	exitCodeBehaviorDiff     = 4
	exitCodeAssertionFailure = 5
)

type commandError struct {
	code int
	err  error
}

func (e *commandError) Error() string {
	return e.err.Error()
}

func (e *commandError) Unwrap() error {
	return e.err
}

func newCommandError(code int, err error) error {
	if err == nil {
		return nil
	}
	return &commandError{code: code, err: err}
}

func exitCodeForError(err error) int {
	var coded *commandError
	if errors.As(err, &coded) && coded.code > 0 {
		return coded.code
	}
	return exitCodeGeneral
}
