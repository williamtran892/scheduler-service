// Package scheduling is the Scheduling domain root: the shared types of the domain, the ledger's
// grid algebra, the candidate-pair search, and what Scheduling needs from other domains
// (WorkshopReader, CustomerReader). Its data access, Store and Tx, is declared in
// scheduling/mysql. It imports only the standard library (implementation notes 2).
//
// This file holds every shared struct and interface declaration; the methods and functions
// that work on them stay with their algorithms in grid.go, availability.go, requirement.go and
// appointment.go.
package scheduling

import (
	"context"
	"encoding/json"
	"time"
)

// ---- appointment and ledger ----

// BookingKey identifies a booking: at most one appointment exists per key, in any status
// (design 4.1.4; deviation D4).
type BookingKey struct {
	VehicleID  string
	CustomerID string
	StartAt    time.Time
	Kind       AppointmentKind
}

// CustomerRequests are the free-text and flag requests a customer makes (contract
// CustomerRequests).
type CustomerRequests struct {
	Waiting               *bool  `json:"waiting,omitempty"`
	CourtesyCar           *bool  `json:"courtesyCar,omitempty"`
	CollectionAndDelivery *bool  `json:"collectionAndDelivery,omitempty"`
	Notes                 string `json:"notes,omitempty"`
}

// Appointment is one row of the appointment table.
type Appointment struct {
	ID               string
	TenantID         string
	DealershipID     string
	CustomerID       string
	VehicleID        string
	ServiceTypeID    string
	Kind             AppointmentKind
	StartAt          time.Time
	EndAt            time.Time
	ServiceBayID     string
	TechnicianID     string
	Status           AppointmentStatus
	CustomerRequests CustomerRequests
	Notes            string
	Channel          Channel
	ActualStartAt    *time.Time // set when work starts (IN_PROGRESS)
	ActualEndAt      *time.Time // set when the job is completed
	Version          int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// CellRange is the cells an appointment owns on one resource, [From, To).
type CellRange struct {
	From, To time.Time
	Cells    int
}

// OwnedCells is what an appointment owns on its bay and on its technician.
type OwnedCells struct {
	Bay        *CellRange
	Technician *CellRange
}

// ResourceRun is a run of cells on one named bay or technician.
type ResourceRun struct {
	ResourceID string
	Run        CellRun
}

// Keep says which of an appointment's cells a release keeps: the cells inside the
// kept bay run and technician run, matched on resource id as well as time, and the cells that
// start before Before. The zero Keep keeps nothing.
type Keep struct {
	Bay        *ResourceRun
	Technician *ResourceRun
	Before     time.Time
}

// Event is one appointment_event row.
type Event struct {
	ID         int64
	Type       string
	Actor      string
	Channel    Channel
	OccurredAt time.Time
	Payload    json.RawMessage
}

// Island is a run of consecutive FREE cells on one resource, [From, To).
type Island struct {
	Kind       ResourceKind
	ResourceID string
	From, To   time.Time
}

// Free is the set of resource ids that are free for the whole run a request needs: the bays free
// for every cell of the bay run, the technicians free for every cell of the technician run. A
// cell is free when it is FREE or the appointment being moved owns it.
type Free map[string]bool

// Loads is the number of BOOKED cells per resource id on one local day.
type Loads map[string]int

// ---- grid ----

// Grid is the dealership's booking grid: cells of SlotMinutes anchored to the top of the UTC
// hour (design 11.1).
type Grid struct {
	SlotMinutes int
}

// CellRun is a run of consecutive cells on one resource.
type CellRun struct {
	First time.Time
	Cells int
	Slot  time.Duration
}

// Slot is one alternative start offered with SLOT_TAKEN or AVAILABILITY_CHANGED.
type Slot struct {
	StartAt, EndAt time.Time
}

// ---- requirement and resources ----

// ServiceRequirement is what a service type needs, in Scheduling's own terms (notes 4.2).
type ServiceRequirement struct {
	ServiceTypeID   string
	TenantID        string
	Code            string
	Name            string
	Duration        time.Duration
	BufferBefore    time.Duration
	BufferAfter     time.Duration
	RequiredSkills  []string // all of them
	AllowedBayTypes []string // any one of them
	OnlineBookable  bool
	Active          bool
}

// DealershipConfig is the dealership as booking sees it.
type DealershipConfig struct {
	DealershipID       string
	TenantID           string
	Code               string
	Name               string
	Location           *time.Location
	Grid               Grid
	BookingHorizonDays int
}

// DayWindow is one local day at a dealership, as UTC instants [From, To).
type DayWindow struct {
	LocalDate string // YYYY-MM-DD
	From, To  time.Time
}

// ServiceBay is a bay as Scheduling sees it.
type ServiceBay struct {
	ID   string
	Name string
	Type string
}

// Technician is a technician as Scheduling sees it, with the skills valid on the day asked.
type Technician struct {
	ID     string
	Name   string
	Skills []string
}

// Candidate is a (bay, technician) pair that can take both runs at a start, with its load:
// the bay's booked cells plus the technician's booked cells that day.
type Candidate struct {
	BayID  string
	TechID string
	Load   int
}

// ---- customers ----

// Customer is a customer as Scheduling sees it.
type Customer struct {
	ID          string
	TenantID    string
	DisplayName string
}

// Vehicle is a vehicle as Scheduling sees it.
type Vehicle struct {
	ID           string
	TenantID     string
	CustomerID   string
	Registration string
	Make         string
	Model        string
}

// ---- readers ----

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
