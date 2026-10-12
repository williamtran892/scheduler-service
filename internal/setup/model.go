// Package setup is the Workshop Setup domain root: dealerships, bays, technicians, skills and
// the service catalogue. In this release it is read-only; its rows come from the fixture.
// It imports only the standard library (implementation notes 2).
package setup

// Dealership is one row of dealership.
type Dealership struct {
	ID                 string
	TenantID           string
	Code               string
	Name               string
	Timezone           string // IANA name
	SlotMinutes        int
	BookingHorizonDays int
}

// ServiceBay is one row of service_bay.
type ServiceBay struct {
	ID           string
	DealershipID string
	Name         string
	Type         BayType
	Status       BayStatus
}

// Technician is one row of technician with the skill codes valid on the date it was read for.
type Technician struct {
	ID           string
	DealershipID string
	DisplayName  string
	Status       TechnicianStatus
	Skills       []string
}

// ServiceType is one row of service_type with its required skills (all of them) and allowed
// bay types (any one of them).
type ServiceType struct {
	ID                  string
	TenantID            string
	Code                string
	Name                string
	DurationMinutes     int
	BufferBeforeMinutes int
	BufferAfterMinutes  int
	OnlineBookable      bool
	Active              bool
	RequiredSkills      []string
	BayTypes            []BayType
}
