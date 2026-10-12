package mysql

import (
	"errors"
	"fmt"
	"testing"

	gomysql "github.com/go-sql-driver/mysql"

	"scheduler/internal/scheduling"
)

func TestMapError(t *testing.T) {
	bookingKey := &gomysql.MySQLError{Number: 1062, Message: "Duplicate entry 'f0..-c0..-2026-10-20 08:00:00.000000-DROP_OFF' for key 'appointment.uq_appointment_booking'"}
	otherKey := &gomysql.MySQLError{Number: 1062, Message: "Duplicate entry 'BAY-b0..-2026-10-20 08:00:00' for key 'time_slot.PRIMARY'"}
	deadlock := &gomysql.MySQLError{Number: 1213, Message: "Deadlock found when trying to get lock; try restarting transaction"}
	plain := errors.New("connection reset")

	tests := []struct {
		name     string
		in       error
		wantIs   error
		wantSame bool
	}{
		{"1062 on the booking key is a duplicate booking", bookingKey, scheduling.ErrDuplicateBooking, false},
		{"wrapped 1062 on the booking key", fmt.Errorf("insert appointment: %w", bookingKey), scheduling.ErrDuplicateBooking, false},
		{"1213 is a deadlock victim", deadlock, scheduling.ErrDeadlockVictim, false},
		{"wrapped 1213", fmt.Errorf("claim bay: %w", deadlock), scheduling.ErrDeadlockVictim, false},
		{"1062 on another key is unchanged", otherKey, otherKey, true},
		{"anything else is unchanged", plain, plain, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapError(tt.in)
			if !errors.Is(got, tt.wantIs) {
				t.Errorf("mapError(%v) = %v, want it to be %v", tt.in, got, tt.wantIs)
			}
			if tt.wantSame && errors.Is(got, scheduling.ErrDuplicateBooking) {
				t.Errorf("mapError(%v) became a duplicate booking", tt.in)
			}
		})
	}
	if mapError(nil) != nil {
		t.Error("mapError(nil) is not nil")
	}
}
