// Package mysql is Scheduling's data access on database/sql: the appointment, the time_slot
// ledger, appointment_event and outbox. Only this package knows MySQL error numbers for those
// tables, and only ledger.go writes time_slot (implementation notes 4.5, 4.6).
package mysql

// mapError turns the MySQL errors the booking transaction must react to into Scheduling's
// errors: 1062 on uq_appointment_booking is scheduling.ErrDuplicateBooking and 1213 is
// scheduling.ErrDeadlockVictim. Anything else is returned unchanged.
func mapError(err error) error { panic("not implemented") }
