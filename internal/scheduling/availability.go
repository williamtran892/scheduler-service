package scheduling

import (
	"cmp"
	"hash/fnv"
	"math/rand/v2"
	"slices"
	"time"
)

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
	var starts []time.Time
	for s := day.From.Truncate(g.Slot()); s.Before(day.To); s = s.Add(g.Slot()) {
		if s.Before(day.From) {
			continue
		}
		bayRun, techRun := req.Runs(s, g)
		if len(freeBays(bayRun, req, bays, islands)) > 0 && len(freeTechnicians(techRun, req, techs, islands)) > 0 {
			starts = append(starts, s)
		}
	}
	return starts
}

// CandidatePairs returns the pairs that can take start, least-loaded first, with pairs of equal
// load shuffled by seed (design 5.2).
func CandidatePairs(start time.Time, g Grid, req ServiceRequirement, bays []ServiceBay, techs []Technician,
	islands []Island, loads Loads, seed uint64) []Candidate {
	bayRun, techRun := req.Runs(start, g)
	var pairs []Candidate
	for _, bayID := range freeBays(bayRun, req, bays, islands) {
		for _, techID := range freeTechnicians(techRun, req, techs, islands) {
			pairs = append(pairs, Candidate{BayID: bayID, TechID: techID, Load: loads[bayID] + loads[techID]})
		}
	}

	// Sort by load, then by id so the order does not depend on the order the caller listed
	// resources in; then shuffle each group of equal load, reproducibly for one seed.
	slices.SortFunc(pairs, func(a, b Candidate) int {
		return cmp.Or(cmp.Compare(a.Load, b.Load), cmp.Compare(a.BayID, b.BayID), cmp.Compare(a.TechID, b.TechID))
	})
	rng := rand.New(rand.NewPCG(seed, 0))
	for lo := 0; lo < len(pairs); {
		hi := lo + 1
		for hi < len(pairs) && pairs[hi].Load == pairs[lo].Load {
			hi++
		}
		group := pairs[lo:hi]
		rng.Shuffle(len(group), func(i, j int) { group[i], group[j] = group[j], group[i] })
		lo = hi
	}
	return pairs
}

// freeBays returns the ids of the bays of an allowed type whose own island contains run.
func freeBays(run CellRun, req ServiceRequirement, bays []ServiceBay, islands []Island) []string {
	var ids []string
	for _, b := range bays {
		if slices.Contains(req.AllowedBayTypes, b.Type) && holds(islands, ResourceBay, b.ID, run) {
			ids = append(ids, b.ID)
		}
	}
	return ids
}

// freeTechnicians returns the ids of the technicians holding every required skill whose own
// island contains run.
func freeTechnicians(run CellRun, req ServiceRequirement, techs []Technician, islands []Island) []string {
	var ids []string
	for _, t := range techs {
		if t.HasAll(req.RequiredSkills) && holds(islands, ResourceTechnician, t.ID, run) {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

// holds reports whether one island of the resource contains the whole run. A run split across
// two islands, or across two resources, is not held (design 5.1).
func holds(islands []Island, kind ResourceKind, id string, run CellRun) bool {
	return slices.ContainsFunc(islands, func(i Island) bool {
		return i.Kind == kind && i.ResourceID == id && run.Within(i)
	})
}

// SeedFrom derives the tie-shuffle seed from a request's appointment id (FNV-1a).
func SeedFrom(id string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return h.Sum64()
}

// NearestAlternatives keeps the starts later than now and not equal to requested, orders them
// by distance from requested (earlier first on a tie), and returns at most limit of them.
func NearestAlternatives(starts []time.Time, requested, now time.Time, limit int) []time.Time {
	var kept []time.Time
	for _, s := range starts {
		if s.After(now) && !s.Equal(requested) {
			kept = append(kept, s)
		}
	}
	slices.SortFunc(kept, func(a, b time.Time) int {
		return cmp.Or(cmp.Compare(distance(a, requested), distance(b, requested)), a.Compare(b))
	})
	return kept[:min(limit, len(kept))]
}

// distance is how far a is from b, whichever is earlier.
func distance(a, b time.Time) time.Duration { return max(a.Sub(b), b.Sub(a)) }
