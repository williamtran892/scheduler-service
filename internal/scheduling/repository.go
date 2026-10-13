package scheduling

import (
	"context"
	"time"
)

// Store is the Scheduling domain's persistence; scheduling/mysql implements it.
type Store interface {
	// InTx runs fn in one transaction: committed if fn returns nil, rolled back otherwise.
	InTx(ctx context.Context, fn func(tx Tx) error) error
	// DayLedger returns the islands of FREE cells and the BOOKED cell count per resource for
	// one local day at a dealership. Cells booked by owner count as free and are not counted
	// as load; owner is "" for a new booking and the appointment being moved for a reschedule.
	DayLedger(ctx context.Context, dealershipID string, day DayWindow, owner string) ([]Island, Loads, error)
	// WorkingCells counts the distinct cell starts in [from, to) for which the dealership has
	// a ledger cell on any resource, in any status.
	WorkingCells(ctx context.Context, dealershipID string, from, to time.Time) (int, error)
	// Appointment loads one appointment; ErrNotFound if there is none.
	Appointment(ctx context.Context, id string) (Appointment, error)
	// AppointmentByBookingKey loads the appointment that owns key, if any.
	AppointmentByBookingKey(ctx context.Context, key BookingKey) (Appointment, bool, error)
	// OwnedCells returns the cell ranges an appointment owns.
	OwnedCells(ctx context.Context, appointmentID string) (OwnedCells, error)
	// Events returns an appointment's audit trail, oldest first.
	Events(ctx context.Context, appointmentID string) ([]Event, error)
	// DueNoShows returns the CONFIRMED appointments that start before before, oldest first.
	DueNoShows(ctx context.Context, before time.Time) ([]Appointment, error)
}

// Tx is the part of Store that runs inside one booking transaction.
type Tx interface {
	// InsertAppointment is the first statement of a booking attempt; a duplicate booking key
	// is ErrDuplicateBooking.
	InsertAppointment(a Appointment) error
	// ClaimPair locks, checks and books the bay run, then the technician run. A taken cell is
	// ErrSlotTaken; a missing cell is ErrBayNotOpen or ErrTechnicianNotWorking; a deadlock is
	// ErrDeadlockVictim.
	ClaimPair(dealershipID, bayID string, bayRun CellRun, techID string, techRun CellRun, appointmentID string) error
	// UpdateAppointment writes a's new state if the stored version is expectedVersion, setting
	// version = expectedVersion + 1. Zero rows matched is ErrStaleVersion; a new start that is
	// another appointment's booking key is ErrDuplicateBooking. It is the first statement of
	// every transition.
	UpdateAppointment(a Appointment, expectedVersion int) error
	// ReleaseCells frees every cell the appointment owns that keep does not keep and returns
	// how many it freed.
	ReleaseCells(appointmentID string, keep Keep) (int, error)
	// AppendEvent appends an appointment_event row.
	AppendEvent(appointmentID, eventType, actor string, channel Channel, payload any) error
	// AppendOutbox appends an outbox row.
	AppendOutbox(aggregateType, aggregateID, eventType string, payload any) error
}

// WorkshopReader is what Scheduling needs from Workshop Setup, in Scheduling's own types.
type WorkshopReader interface {
	// DealershipConfig returns ErrNotFound for an unknown dealership.
	DealershipConfig(ctx context.Context, dealershipID string) (DealershipConfig, error)
	// Requirement returns ErrNotFound for an unknown service type; an inactive one is returned
	// with Active false.
	Requirement(ctx context.Context, serviceTypeID string) (ServiceRequirement, error)
	// Resources returns the ACTIVE bays and the ACTIVE technicians of a dealership, each
	// technician with the skills valid on localDate (YYYY-MM-DD).
	Resources(ctx context.Context, dealershipID, localDate string) ([]ServiceBay, []Technician, error)
	// Bay and Technician return one resource in any status, for the appointment record.
	Bay(ctx context.Context, id string) (ServiceBay, error)
	Technician(ctx context.Context, id string) (Technician, error)
}

// CustomerReader is what Scheduling needs from Customer & Vehicle.
type CustomerReader interface {
	Customer(ctx context.Context, id string) (Customer, error)
	Vehicle(ctx context.Context, id string) (Vehicle, error)
}
