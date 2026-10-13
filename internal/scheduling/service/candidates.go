package service

import (
	"cmp"
	"context"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"slices"
	"strings"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
)

// candidate is a (bay, technician) pair that can take both runs at a start, with its load: the
// bay's booked cells plus the technician's booked cells that day.
type candidate struct {
	BayID  string
	TechID string
	Load   int
}

// candidates returns the (bay, technician) pairs that can take the request's runs, least-loaded
// first, with the plan's preferred pair ahead of them when it is one. It reads the day's active
// bays and technicians, narrows them at once to the ones eligible for the request (eligible), and
// asks the store about those only: which have every cell of their run free, then how loaded the
// free ones are. The eligible resources stay in the plan for the record of each attempt.
func (s *service) candidates(ctx context.Context, p *plan) ([]candidate, error) {
	day := p.cfg.Day(p.start)
	bays, techs, err := s.workshop.Resources(ctx, p.cfg.DealershipID, day.LocalDate)
	if err != nil {
		return nil, err
	}
	bayIDs, techIDs, err := p.eligible(bays, techs, day.LocalDate)
	if err != nil {
		return nil, err
	}
	freeBays, freeTechs, err := s.store.GetAvailableResources(ctx, p.cfg.DealershipID, bayIDs, p.bay, techIDs, p.tech, p.existingAppointmentID)
	if err != nil {
		return nil, err
	}
	loads, err := s.store.GetResourceLoad(ctx, p.cfg.DealershipID, day, slices.Concat(freeBays, freeTechs), p.existingAppointmentID)
	if err != nil {
		return nil, err
	}
	pairs := candidatePairs(freeBays, freeTechs, loads, seedFrom(p.appointmentID))
	if i := slices.IndexFunc(pairs, func(c candidate) bool {
		return c.BayID == p.preferredPair.BayID && c.TechID == p.preferredPair.TechID
	}); i > 0 {
		preferred := pairs[i]
		pairs = slices.Insert(slices.Delete(pairs, i, i+1), 0, preferred)
	}
	return pairs, nil
}

// eligible narrows the day's active bays and technicians to the ones that can take this request,
// keeps them in the plan by id for record, and returns their ids. A bay is eligible when its type
// is one the service type allows, a technician when it holds every required skill on the day; a
// request that names a bay or a technician is narrowed to that one. A named bay that is not an
// active bay of an allowed type is 422 INVALID_VALUE on serviceBayId, a named technician that is
// not active is 422 INVALID_VALUE on technicianId, and no eligible technician, or a named one
// without the skills, is NO_QUALIFIED_TECHNICIAN.
func (p *plan) eligible(bays []scheduling.ServiceBay, techs []scheduling.Technician, localDate string) (bayIDs, techIDs []string, err error) {
	p.bays = make(map[string]scheduling.ServiceBay)
	for _, b := range bays {
		if (p.requiredBayID == "" || b.ID == p.requiredBayID) && slices.Contains(p.req.AllowedBayTypes, b.Type) {
			p.bays[b.ID] = b
			bayIDs = append(bayIDs, b.ID)
		}
	}
	if p.requiredBayID != "" && len(bayIDs) == 0 {
		return nil, nil, platform.InvalidValue("serviceBayId", "is not an active bay of this dealership of a type the service type allows")
	}

	p.techs = make(map[string]scheduling.Technician)
	active := false // a technician passed the name filter, so the named one is active
	for _, t := range techs {
		if p.requiredTechnicianID != "" && t.ID != p.requiredTechnicianID {
			continue
		}
		active = true
		if t.HasAll(p.req.RequiredSkills) {
			p.techs[t.ID] = t
			techIDs = append(techIDs, t.ID)
		}
	}
	skills := strings.Join(p.req.RequiredSkills, ", ")
	switch {
	case p.requiredTechnicianID != "" && !active:
		return nil, nil, platform.InvalidValue("technicianId", "is not an active technician of this dealership")
	case p.requiredTechnicianID != "" && len(techIDs) == 0:
		return nil, nil, fmt.Errorf("technician %s does not hold %s on %s: %w", p.requiredTechnicianID,
			skills, localDate, scheduling.ErrNoQualifiedTechnician)
	case len(techIDs) == 0:
		return nil, nil, fmt.Errorf("no active technician at %s holds %s on %s: %w", p.cfg.Code,
			skills, localDate, scheduling.ErrNoQualifiedTechnician)
	}
	return bayIDs, techIDs, nil
}

// candidatePairs forms every pair of an available bay and an available technician, least-loaded
// first, with pairs of equal load shuffled by seed (design 5.2). The resources are the ones the
// store found free for the whole of their run and eligible for the request, so the pairs are only
// the cross product: a run that is free partly on one bay and partly on another makes neither bay
// available. A pair's load is the bay's load plus the technician's.
func candidatePairs(bayIDs, techIDs []string, loads scheduling.Loads, seed uint64) []candidate {
	var pairs []candidate
	for _, bayID := range bayIDs {
		for _, techID := range techIDs {
			pairs = append(pairs, candidate{BayID: bayID, TechID: techID, Load: loads[bayID] + loads[techID]})
		}
	}

	// Sort by load, then by id so the order does not depend on the order the caller listed
	// resources in; then shuffle each group of equal load, reproducibly for one seed.
	slices.SortFunc(pairs, func(a, b candidate) int {
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

// seedFrom derives the tie-shuffle seed from a request's appointment id (FNV-1a).
func seedFrom(id string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return h.Sum64()
}
