// Package mysql is Workshop Setup's read access on database/sql: dealerships, bays,
// technicians with their skills, and the service catalogue. Its Repository implements
// scheduling.WorkshopReader and returns Scheduling's own types, so the translation Scheduling
// needs happens in its queries and column scanners.
package mysql

import (
	"database/sql"
	"errors"
	"fmt"

	"scheduler/internal/scheduling"
)

// readError gives a failed read of what/id its context: no row is scheduling.ErrNotFound.
func readError(what, id string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s %s: %w", what, id, scheduling.ErrNotFound)
	}
	return fmt.Errorf("read %s %s: %w", what, id, err)
}
