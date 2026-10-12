package scheduling

import "time"

// Island is a run of consecutive FREE cells on one resource, [From, To).
type Island struct {
	Kind       ResourceKind
	ResourceID string
	From, To   time.Time
}

// Loads is the number of BOOKED cells per resource id on one local day.
type Loads map[string]int

// Candidate is a (bay, technician) pair that can take both runs at a start, with its load:
// the bay's booked cells plus the technician's booked cells that day.
type Candidate struct {
	BayID  string
	TechID string
	Load   int
}

// FeasibleStarts returns every grid start inside the day at which some single bay of an
// allowed type and some single qualified technician each have an island containing their run
// (design 5.1). Starts are in time order.
func FeasibleStarts(day DayWindow, g Grid, req ServiceRequirement, bays []ServiceBay, techs []Technician, islands []Island) []time.Time {
	panic("not implemented")
}

// CandidatePairs returns the pairs that can take start, least-loaded first, with pairs of equal
// load shuffled by seed (design 5.2).
func CandidatePairs(start time.Time, g Grid, req ServiceRequirement, bays []ServiceBay, techs []Technician,
	islands []Island, loads Loads, seed uint64) []Candidate {
	panic("not implemented")
}

// SeedFrom derives the tie-shuffle seed from a request's appointment id.
func SeedFrom(id string) uint64 { panic("not implemented") }

// NearestAlternatives keeps the starts later than now and not equal to requested, orders them
// by distance from requested (earlier first on a tie), and returns at most limit of them.
func NearestAlternatives(starts []time.Time, requested, now time.Time, limit int) []time.Time {
	panic("not implemented")
}
