// Package app holds the use cases the web UI and the JSON API both serve.
// Each area lives in its own subpackage with a small interface, and reports
// failure with the errors below so neither transport needs to know how the
// store or the runner spells its own errors.
package app

import (
	"errors"
	"fmt"
)

var (
	// ErrNotFound means the requested Project, Execution, Step or Artifact
	// does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInvalid means the request was understood but rejected, such as an
	// artifact path escaping the Step's directory or a missing workflow.
	ErrInvalid = errors.New("invalid request")
	// ErrConflict means the request is valid but the Execution is in a state
	// that forbids it, such as rebuilding a snapshot Execution.
	ErrConflict = errors.New("conflict")
	// ErrNotRetryable means the Execution cannot be retried: it already has a
	// newer Attempt, or none of its Steps can be re-run.
	ErrNotRetryable = errors.New("execution is not retryable")
	// ErrWorkspaceGone means the Execution's Workspace was reclaimed, so a
	// retry has no source to continue from.
	ErrWorkspaceGone = errors.New("workspace no longer available")
	// ErrUnavailable means the backing dependency, such as the container
	// engine, is not reachable.
	ErrUnavailable = errors.New("unavailable")
)

// InvalidError is rejected input with a message a surface can show as is. It
// matches errors.Is(err, ErrInvalid).
type InvalidError struct {
	Message string
}

func (e InvalidError) Error() string { return e.Message }

func (e InvalidError) Is(target error) bool { return target == ErrInvalid }

// Invalidf reports rejected input with a message written for a human.
func Invalidf(format string, args ...any) error {
	return InvalidError{Message: fmt.Sprintf(format, args...)}
}
