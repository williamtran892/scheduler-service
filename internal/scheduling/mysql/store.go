package mysql

import (
	"context"
	"time"

	"scheduler/internal/scheduling"
)

// Store is the Scheduling domain's persistence: the service and the sweeper depend on it, and
// New returns its MySQL implementation.
type Store interface {
	// InTx runs fn in one transaction: committed if fn returns nil, rolled back otherwise.
	InTx(ctx context.Context, fn func(tx Tx) error) error
	// DayLedger returns the islands of FREE cells and the BOOKED cell count per resource for
	// one local day at a dealership. Cells booked by owner count as free and are not counted
	// as load; owner is "" for a new booking and the appointment being moved for a reschedule.
	DayLedger(ctx context.Context, dealershipID string, day scheduling.DayWindow, owner string) ([]scheduling.Island, scheduling.Loads, error)
	// WorkingCells counts the distinct cell starts in [from, to) for which the dealership has
	// a ledger cell on any resource, in any status.
	WorkingCells(ctx context.Context, dealershipID string, from, to time.Time) (int, error)
	// Appointment loads one appointment; scheduling.ErrNotFound if there is none.
	Appointment(ctx context.Context, id string) (scheduling.Appointment, error)
	// AppointmentByBookingKey loads the appointment that owns key, if any.
	AppointmentByBookingKey(ctx context.Context, key scheduling.BookingKey) (scheduling.Appointment, bool, error)
	// OwnedCells returns the cell ranges an appointment owns.
	OwnedCells(ctx context.Context, appointmentID string) (scheduling.OwnedCells, error)
	// Events returns an appointment's audit trail, oldest first.
	Events(ctx context.Context, appointmentID string) ([]scheduling.Event, error)
	// DueNoShows returns the CONFIRMED appointments that start before before, oldest first.
	DueNoShows(ctx context.Context, before time.Time) ([]scheduling.Appointment, error)
}

// Tx is the part of Store that runs inside one transaction: a booking attempt or a transition.
type Tx interface {
	// InsertAppointment is the first statement of a booking attempt; a duplicate booking key
	// is scheduling.ErrDuplicateBooking.
	InsertAppointment(a scheduling.Appointment) error
	// ClaimPair locks, checks and books the bay run, then the technician run. A taken cell is
	// scheduling.ErrSlotTaken; a missing cell is scheduling.ErrBayNotOpen or
	// scheduling.ErrTechnicianNotWorking; a deadlock is scheduling.ErrDeadlockVictim.
	ClaimPair(dealershipID, bayID string, bayRun scheduling.CellRun, techID string, techRun scheduling.CellRun, appointmentID string) error
	// UpdateAppointment writes a's new state if the stored version is expectedVersion, setting
	// version = expectedVersion + 1. Zero rows matched is scheduling.ErrStaleVersion; a new
	// start that is another appointment's booking key is scheduling.ErrDuplicateBooking. It is
	// the first statement of every transition.
	UpdateAppointment(a scheduling.Appointment, expectedVersion int) error
	// ReleaseCells frees every cell the appointment owns that keep does not keep and returns
	// how many it freed.
	ReleaseCells(appointmentID string, keep scheduling.Keep) (int, error)
	// AppendEvent appends an appointment_event row.
	AppendEvent(appointmentID, eventType, actor string, channel scheduling.Channel, payload any) error
	// AppendOutbox appends an outbox row.
	AppendOutbox(aggregateType, aggregateID, eventType string, payload any) error
}
