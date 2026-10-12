// Package service is the Scheduling service (go-kit): the Service interface, its endpoints,
// its HTTP transport and its middleware (implementation notes 4.0).
package service

import (
	"context"
	"time"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
)

//go:generate go tool mockgen -destination=mock_scheduling_test.go -package=service scheduler/internal/scheduling Store,Tx,WorkshopReader,CustomerReader
//go:generate go tool mockgen -destination=mock_service_test.go -package=service -self_package=scheduler/internal/scheduling/service scheduler/internal/scheduling/service Service

// Service books and reads appointments.
type Service interface {
	// Book confirms an appointment, or returns the one that already owns the booking key.
	Book(ctx context.Context, cmd BookCommand) (Record, error)
	// Get reads one appointment; scheduling.ErrNotFound if there is none.
	Get(ctx context.Context, q GetQuery) (Record, error)
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
	return &service{}
}

// service is the Scheduling service. Its dependencies are added with its behaviour.
type service struct{}

// Get reads one appointment with its names, owned cells and, when asked, its events.
func (s *service) Get(ctx context.Context, q GetQuery) (Record, error) { panic("not implemented") }
