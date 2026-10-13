package scheduling

import (
	"cmp"
	"hash/fnv"
	"math/rand/v2"
	"slices"
	"time"
)

// FeasibleStarts returns every grid start inside the day at which some single bay of an
// allowed type and some single qualified technician each have an island containing their run
// (design 5.1). Starts are in time order.
func FeasibleStarts(day DayWindow, g Grid, req ServiceRequirement, bays []ServiceBay, techs []Technician, islands []Island) []time.Time {
	var starts []time.Time
	for s := g.Ceil(day.From); s.Before(day.To); s = s.Add(g.Slot()) {
		bayRun, techRun := req.Runs(s, g)
		bayFree := func(id string) bool { return holds(islands, ResourceBay, id, bayRun) }
		techFree := func(id string) bool { return holds(islands, ResourceTechnician, id, techRun) }
		if len(freeBays(req, bays, bayFree)) > 0 && len(freeTechnicians(req, techs, techFree)) > 0 {
			starts = append(starts, s)
		}
	}
	return starts
}

// CandidatePairs returns the pairs of a bay and a technician that can take the request, least-loaded
// first, with pairs of equal load shuffled by seed (design 5.2). free holds the resources that are
// free for the whole of their run, which the ledger's scan decides, so a run that is free partly on
// one bay and partly on another makes neither bay a candidate.
func CandidatePairs(req ServiceRequirement, bays []ServiceBay, techs []Technician, free Free, loads Loads, seed uint64) []Candidate {
	isFree := func(id string) bool { return free[id] }
	techIDs := freeTechnicians(req, techs, isFree)
	var pairs []Candidate
	for _, bayID := range freeBays(req, bays, isFree) {
		for _, techID := range techIDs {
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

// freeBays returns the ids of the bays of an allowed type for which isFree reports true.
func freeBays(req ServiceRequirement, bays []ServiceBay, isFree func(id string) bool) []string {
	var ids []string
	for _, b := range bays {
		if slices.Contains(req.AllowedBayTypes, b.Type) && isFree(b.ID) {
			ids = append(ids, b.ID)
		}
	}
	return ids
}

// freeTechnicians returns the ids of the technicians holding every required skill for which
// isFree reports true.
func freeTechnicians(req ServiceRequirement, techs []Technician, isFree func(id string) bool) []string {
	var ids []string
	for _, t := range techs {
		if t.HasAll(req.RequiredSkills) && isFree(t.ID) {
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
