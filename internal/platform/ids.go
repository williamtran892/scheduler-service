package platform

import "github.com/google/uuid"

// IDs generates identifiers for new records.
type IDs interface {
	New() string
}

// UUIDs generates random (version 4) UUIDs in their canonical 36-character form.
type UUIDs struct{}

func (UUIDs) New() string { return uuid.NewString() }
