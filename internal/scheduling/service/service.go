// Package service is the Scheduling service (go-kit): the Service interface, its commands and
// Record, its business code and its middleware (implementation notes 4.0). Its HTTP surface is
// the endpoint package.
package service

import (
	"context"
	"fmt"
	"time"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
)

//go:generate go tool mockgen -destination=mock_scheduling_test.go -package=service scheduler/internal/scheduling Store,Tx,WorkshopReader,CustomerReader
//go:generate go tool mockgen -destination=mock_service_test.go -package=service -self_package=scheduler/internal/scheduling/service scheduler/internal/scheduling/service Service

// Service books, reads and moves appointments through their lifecycle.
type Service interface {
	// Book confirms an appointment, or returns the one that already owns the booking key.
	Book(ctx context.Context, cmd BookCommand) (Record, error)
	// Get reads one appointment; scheduling.ErrNotFound if there is none.
	Get(ctx context.Context, q GetQuery) (Record, error)

	// Reschedule moves a CONFIRMED appointment in time, to other resources, or both (design 5.4).
	Reschedule(ctx context.Context, cmd RescheduleCommand) (Record, error)
	// Cancel releases every cell of a CONFIRMED or CHECKED_IN appointment; a CANCELLED one is
	// returned unchanged.
	Cancel(ctx context.Context, cmd TransitionCommand) (Record, error)
	// CheckIn checks a CONFIRMED appointment in, or a NO_SHOW in as a late arrival.
	CheckIn(ctx context.Context, cmd CheckInCommand) (Record, error)
	// Start records the actual start of a CHECKED_IN appointment.
	Start(ctx context.Context, cmd StartCommand) (Record, error)
	// Complete records the actual end of an IN_PROGRESS appointment and releases its cells from
	// the current grid cell on.
	Complete(ctx context.Context, cmd CompleteCommand) (Record, error)
	// MarkNoShow marks a CONFIRMED appointment NO_SHOW and releases every cell.
	MarkNoShow(ctx context.Context, cmd TransitionCommand) (Record, error)
}

// TransitionCommand is what every lifecycle request carries (contract TransitionRequest), plus
// who is acting: the calling channel, or none and a job name when the sweeper acts.
type TransitionCommand struct {
	AppointmentID   string
	ExpectedVersion int
	Reason          string
	Channel         scheduling.Channel // empty when a background job acts
	Actor           string             // empty means "anonymous"
}

// RescheduleCommand is a decoded rescheduleAppointment request. A nil StartAt keeps the current
// start (a pure reassign); an empty TechnicianID or ServiceBayID leaves the choice to the service.
type RescheduleCommand struct {
	TransitionCommand
	StartAt      *time.Time
	TechnicianID string
	ServiceBayID string
}

// CheckInCommand is a decoded checkInAppointment request.
type CheckInCommand struct {
	TransitionCommand
	Mileage            *int
	AdditionalConcerns string
}

// StartCommand is a decoded startAppointment request; a nil StartedAt means now.
type StartCommand struct {
	TransitionCommand
	StartedAt *time.Time
}

// CompleteCommand is a decoded completeAppointment request; a nil CompletedAt means now.
type CompleteCommand struct {
	TransitionCommand
	CompletedAt *time.Time
}

// BookCommand is a decoded bookAppointment request plus the calling channel.
type BookCommand struct {
	DealershipID     string
	ServiceTypeID    string
	CustomerID       string
	VehicleID        string
	StartAt          time.Time
	Kind             scheduling.AppointmentKind
	CustomerRequests scheduling.CustomerRequests
	Notes            string
	Channel          scheduling.Channel
}

// GetQuery is a decoded getAppointment request.
type GetQuery struct {
	AppointmentID string
	IncludeEvents bool
}

// Record is an appointment with everything its public record shows: names from the other
// domains, the owned cells and, when asked, the audit trail.
type Record struct {
	Appointment scheduling.Appointment
	Dealership  scheduling.DealershipConfig
	ServiceType scheduling.ServiceRequirement
	Customer    scheduling.Customer
	Vehicle     scheduling.Vehicle
	Bay         scheduling.ServiceBay
	Technician  scheduling.Technician
	Cells       scheduling.OwnedCells
	Events      []scheduling.Event
}

// NewService builds the Scheduling service. Every dependency is passed in; main chooses the
// real ones, tests pass mocks.
func NewService(store scheduling.Store, workshop scheduling.WorkshopReader, customers scheduling.CustomerReader,
	clock platform.Clock, ids platform.IDs) Service {
	return &service{store: store, workshop: workshop, customers: customers, clock: clock, ids: ids}
}

// service is the Scheduling service.
type service struct {
	store     scheduling.Store
	workshop  scheduling.WorkshopReader
	customers scheduling.CustomerReader
	clock     platform.Clock
	ids       platform.IDs
}

// Get reads one appointment with its names, owned cells and, when asked, its events.
func (s *service) Get(ctx context.Context, q GetQuery) (Record, error) {
	a, err := s.store.Appointment(ctx, q.AppointmentID)
	if err != nil {
		return Record{}, err
	}
	rec, err := s.load(ctx, a)
	if err != nil {
		return Record{}, err
	}
	if q.IncludeEvents {
		if rec.Events, err = s.store.Events(ctx, a.ID); err != nil {
			return Record{}, fmt.Errorf("read events of appointment %s: %w", a.ID, err)
		}
	}
	return rec, nil
}

// load assembles the record of a stored appointment, reading the cells it owns.
func (s *service) load(ctx context.Context, a scheduling.Appointment) (Record, error) {
	cells, err := s.store.OwnedCells(ctx, a.ID)
	if err != nil {
		return Record{}, fmt.Errorf("read cells of appointment %s: %w", a.ID, err)
	}
	return s.record(ctx, a, cells)
}

// record assembles an appointment's record: the names come from the other domains through the
// readers, so each domain's tables are read only by its own package (notes 2).
func (s *service) record(ctx context.Context, a scheduling.Appointment, cells scheduling.OwnedCells) (Record, error) {
	rec := Record{Appointment: a, Cells: cells}
	var err error
	if rec.Dealership, err = s.workshop.DealershipConfig(ctx, a.DealershipID); err != nil {
		return Record{}, fmt.Errorf("record of appointment %s: %w", a.ID, err)
	}
	if rec.ServiceType, err = s.workshop.Requirement(ctx, a.ServiceTypeID); err != nil {
		return Record{}, fmt.Errorf("record of appointment %s: %w", a.ID, err)
	}
	if rec.Customer, err = s.customers.Customer(ctx, a.CustomerID); err != nil {
		return Record{}, fmt.Errorf("record of appointment %s: %w", a.ID, err)
	}
	if rec.Vehicle, err = s.customers.Vehicle(ctx, a.VehicleID); err != nil {
		return Record{}, fmt.Errorf("record of appointment %s: %w", a.ID, err)
	}
	if rec.Bay, err = s.workshop.Bay(ctx, a.ServiceBayID); err != nil {
		return Record{}, fmt.Errorf("record of appointment %s: %w", a.ID, err)
	}
	if rec.Technician, err = s.workshop.Technician(ctx, a.TechnicianID); err != nil {
		return Record{}, fmt.Errorf("record of appointment %s: %w", a.ID, err)
	}
	return rec, nil
}
