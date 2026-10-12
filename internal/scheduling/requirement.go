package scheduling

import "time"

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

// Runs returns the bay run and the technician run for a start: both begin at
// start - BufferBefore and cover BufferBefore + Duration + BufferAfter, rounded up to whole
// cells. They are identical in this release (design 5.1).
func (r ServiceRequirement) Runs(start time.Time, g Grid) (bay, tech CellRun) {
	panic("not implemented")
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

// Day returns the local day, in the dealership's time zone, that contains t.
func (c DealershipConfig) Day(t time.Time) DayWindow { panic("not implemented") }

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

// HasAll reports whether the technician holds every skill in skills.
func (t Technician) HasAll(skills []string) bool { panic("not implemented") }

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
