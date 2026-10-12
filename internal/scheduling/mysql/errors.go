// Package mysql is Scheduling's data access on database/sql: the appointment, the time_slot
// ledger, appointment_event and outbox. Only this package knows MySQL error numbers for those
// tables, and only ledger.go writes time_slot (implementation notes 4.5, 4.6).
package mysql

import (
	"errors"
	"fmt"
	"strings"

	gomysql "github.com/go-sql-driver/mysql"

	"scheduler/internal/scheduling"
)

// MySQL error numbers the booking transaction reacts to.
const (
	errDuplicateEntry = 1062
	errDeadlock       = 1213
)

// mapError turns the MySQL errors the booking transaction must react to into Scheduling's
// errors: 1062 on uq_appointment_booking is scheduling.ErrDuplicateBooking and 1213 is
// scheduling.ErrDeadlockVictim. Anything else is returned unchanged.
func mapError(err error) error {
	var me *gomysql.MySQLError
	if !errors.As(err, &me) {
		return err
	}
	switch {
	case me.Number == errDuplicateEntry && strings.Contains(me.Message, "uq_appointment_booking"):
		return fmt.Errorf("%w: %w", scheduling.ErrDuplicateBooking, err)
	case me.Number == errDeadlock:
		return fmt.Errorf("%w: %w", scheduling.ErrDeadlockVictim, err)
	}
	return err
}
