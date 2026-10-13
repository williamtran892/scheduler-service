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
	// GetAvailableResources returns, of the given bays and technicians only, the ids of the bays
	// whose every cell of bayRun is free and of the technicians whose every cell of techRun is
	// free. A cell is free when its status is FREE or owner owns it; owner is "" for a new
	// booking and the appointment being moved for a reschedule. A resource with a missing cell in
	// its run is not available. With no ids it returns without querying.
	GetAvailableResources(ctx context.Context, dealershipID string, bayIDs []string, bayRun scheduling.CellRun,
		techIDs []string, techRun scheduling.CellRun, owner string) (availableBays, availableTechs []string, err error)
	// GetResourceLoad returns, for the given resources only, the number of BOOKED cells each has
	// on the local day, in the day's UTC window. Cells owned by owner are not counted. A resource
	// with no such cell is absent from the result, which reads as a load of 0. With no ids it
	// returns without querying.
	GetResourceLoad(ctx context.Context, dealershipID string, day scheduling.DayWindow, resourceIDs []string, owner string) (scheduling.Loads, error)
	// WorkingCellCounts returns, for each of the two runs, how many of its cells the dealership
	// has a ledger cell for, on any resource and in any status: the number of distinct cell
	// starts between the run's first and last cell. A run is inside working time when its count
	// equals its cells. One query answers both.
	WorkingCellCounts(ctx context.Context, dealershipID string, bayRun, techRun scheduling.CellRun) (bayCells, techCells int, err error)
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
	// ClaimPair books the bay run, then the technician run, each with one conditional UPDATE. A
	// taken cell is scheduling.ErrSlotTaken; a missing cell is scheduling.ErrBayNotOpen or
	// scheduling.ErrTechnicianNotWorking; a deadlock is scheduling.ErrDeadlockVictim. After any
	// error the transaction must be rolled back: the rows a refused claim matched were changed.
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
