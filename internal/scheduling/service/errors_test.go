package service

import (
	"errors"
	"net/http"
	"testing"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
)

// Every error the scheduling root exports reaches the client as a contract problem, never as a
// 500 (implementation notes 4.6).
func TestEverySchedulingErrorHasAContractStatus(t *testing.T) {
	all := []error{
		scheduling.ErrInvalidEnum, scheduling.ErrNotFound, scheduling.ErrOffGrid, scheduling.ErrOutsideOpeningHours,
		scheduling.ErrNoQualifiedTechnician, scheduling.ErrNotBookableOnline, scheduling.ErrSlotTaken,
		scheduling.ErrCellsMissing, scheduling.ErrBayNotOpen, scheduling.ErrTechnicianNotWorking,
		scheduling.ErrAvailabilityChanged, scheduling.ErrDuplicateBooking, scheduling.ErrDeadlockVictim,
		&scheduling.SlotTakenError{}, &scheduling.AvailabilityChangedError{}, &scheduling.DuplicateBookingError{},
		scheduling.ErrStaleVersion, scheduling.ErrInvalidTransition, scheduling.ErrForbidden,
		&scheduling.VersionConflictError{Current: 2, Sent: 1},
	}
	for _, err := range all {
		var coded platform.Coded
		if !errors.As(err, &coded) {
			t.Errorf("%T has no problem code", err)
			continue
		}
		if status := platform.StatusFor(coded.ProblemCode()); status < 400 || status >= http.StatusInternalServerError {
			t.Errorf("%T code %s maps to %d, want a 4xx", err, coded.ProblemCode(), status)
		}
	}
}
