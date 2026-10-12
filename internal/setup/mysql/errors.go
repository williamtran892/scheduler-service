// Package mysql is Workshop Setup's read access on database/sql: dealerships, bays,
// technicians with their skills, and the service catalogue.
package mysql

import (
	"database/sql"
	"errors"
	"fmt"

	"scheduler/internal/setup"
)

// readError gives a failed read of what/id its context: no row is setup.ErrNotFound.
func readError(what, id string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s %s: %w", what, id, setup.ErrNotFound)
	}
	return fmt.Errorf("read %s %s: %w", what, id, err)
}
