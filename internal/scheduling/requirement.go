package scheduling

import (
	"slices"
	"time"
)

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
	first := start.Add(-r.BufferBefore).Truncate(g.Slot()) // the cell the before-buffer begins in
	end := start.Add(r.Duration + r.BufferAfter)
	run := CellRun{First: first, Cells: g.CellsFor(end.Sub(first)), Slot: g.Slot()}
	return run, run
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
func (c DealershipConfig) Day(t time.Time) DayWindow {
	local := t.In(c.Location)
	from := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, c.Location)
	to := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, c.Location)
	return DayWindow{LocalDate: from.Format(time.DateOnly), From: from.UTC(), To: to.UTC()}
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

// HasAll reports whether the technician holds every skill in skills.
func (t Technician) HasAll(skills []string) bool {
	for _, s := range skills {
		if !slices.Contains(t.Skills, s) {
			return false
		}
	}
	return true
}

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
