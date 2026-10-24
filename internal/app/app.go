// Package app holds the use cases the web UI and the JSON API both serve.
// Each area lives in its own subpackage with a small interface, and reports
// failure with the errors below so neither transport needs to know how the
// store spells "not found".
package app

import "errors"

var (
	// ErrNotFound means the requested Project, Execution, Step or Artifact
	// does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInvalid means the request was understood but rejected, such as an
	// artifact path escaping the Step's directory.
	ErrInvalid = errors.New("invalid request")
)
