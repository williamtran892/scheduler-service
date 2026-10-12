package setup

import "errors"

var (
	// ErrNotFound is returned when a row does not exist.
	ErrNotFound = errors.New("setup: not found")
	// ErrInvalidEnum is returned for a value outside an enumeration.
	ErrInvalidEnum = errors.New("setup: invalid enumeration value")
)
