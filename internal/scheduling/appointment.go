package scheduling

import (
	"encoding/json"
	"time"
)

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

// Key returns the appointment's booking key.
func (a Appointment) Key() BookingKey {
	return BookingKey{VehicleID: a.VehicleID, CustomerID: a.CustomerID, StartAt: a.StartAt, Kind: a.Kind}
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

// Keep says which of an appointment's cells a release keeps (research R3): the cells inside the
// kept bay run and technician run, matched on resource id as well as time, and the cells that
// start before Before. The zero Keep keeps nothing.
type Keep struct {
	Bay        *ResourceRun
	Technician *ResourceRun
	Before     time.Time
}

// Keeps reports whether the cell of kind and resourceID starting at slotStart stays with the
// appointment.
func (k Keep) Keeps(kind ResourceKind, resourceID string, slotStart time.Time) bool {
	if !k.Before.IsZero() && slotStart.Before(k.Before) {
		return true
	}
	switch kind {
	case ResourceBay:
		return k.Bay.holds(resourceID, slotStart)
	case ResourceTechnician:
		return k.Technician.holds(resourceID, slotStart)
	}
	return false
}

// holds reports whether the cell starting at slotStart on resourceID lies inside the run; a nil
// run holds nothing.
func (r *ResourceRun) holds(resourceID string, slotStart time.Time) bool {
	return r != nil && r.ResourceID == resourceID &&
		!slotStart.Before(r.Run.First) && slotStart.Before(r.Run.End())
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
