package service

import (
	"errors"
	"slices"
	"testing"

	"scheduler/internal/scheduling"
)

// candidatePairs forms the pairs of the resources the store found available, so these tests give
// it ids and loads and nothing else; the bay types and skills are eligible's job.

func TestCandidatePairs_NothingAvailableMakesNoPairs(t *testing.T) {
	tests := []struct {
		name  string
		bays  []string
		techs []string
	}{
		{"no technician is free for the whole run", []string{"bay-1"}, nil},
		{"no bay is free for the whole run", nil, []string{"sam"}},
		{"nothing is available", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if pairs := candidatePairs(tt.bays, tt.techs, scheduling.Loads{}, 1); len(pairs) != 0 {
				t.Errorf("candidatePairs = %v, want none", pairs)
			}
		})
	}
}

func TestCandidatePairs_EveryAvailableBayWithEveryAvailableTechnician(t *testing.T) {
	pairs := candidatePairs([]string{"bay-1", "bay-2"}, []string{"sam", "priya", "lee"}, scheduling.Loads{}, 1)
	if got := pairSet(pairs); !slices.Equal(got, []string{
		"bay-1/lee", "bay-1/priya", "bay-1/sam", "bay-2/lee", "bay-2/priya", "bay-2/sam"}) {
		t.Errorf("pairs = %v, want each bay with each technician", got)
	}

	if one := candidatePairs([]string{"bay-1"}, []string{"lee"}, scheduling.Loads{}, 1); len(one) != 1 {
		t.Errorf("one bay and one technician = %v, want one pair", one)
	}
}

// The scenario "The least-loaded qualified bay and technician get the job": Sam holds a 10-cell
// EV_MAJOR on Bay 3, Priya a 3-cell MOT on Bay 2. Load = bay cells + technician cells that day.
func TestCandidatePairs_LeastLoadedFirst(t *testing.T) {
	loads := scheduling.Loads{"bay-3": 10, "sam": 10, "bay-2": 3, "priya": 3}

	pairs := candidatePairs([]string{"bay-1", "bay-2"}, []string{"sam", "priya", "lee"}, loads, 42)

	if len(pairs) != 6 {
		t.Fatalf("got %d pairs, want 6: %v", len(pairs), pairs)
	}
	if first := pairs[0]; first.BayID != "bay-1" || first.TechID != "lee" || first.Load != 0 {
		t.Errorf("first pair = %+v, want Bay 1 with Lee at load 0", first)
	}
	wantLoads := map[string]int{"bay-1/lee": 0, "bay-1/priya": 3, "bay-2/lee": 3, "bay-2/priya": 6, "bay-1/sam": 10, "bay-2/sam": 13}
	for i, p := range pairs {
		if p.Load != wantLoads[p.BayID+"/"+p.TechID] {
			t.Errorf("%s/%s load = %d, want %d", p.BayID, p.TechID, p.Load, wantLoads[p.BayID+"/"+p.TechID])
		}
		if i > 0 && p.Load < pairs[i-1].Load {
			t.Errorf("pairs not least-loaded first: %v", pairs)
		}
	}
}

func TestCandidatePairs_EqualLoadsAreShuffledBySeed(t *testing.T) {
	bays, techs := []string{"bay-1", "bay-2"}, []string{"sam", "priya", "lee"}
	order := func(seed uint64) string {
		return pairOrder(candidatePairs(bays, techs, scheduling.Loads{}, seed))
	}

	if a, b := order(7), order(7); a != b {
		t.Errorf("same seed gave %s and %s, want the same order", a, b)
	}
	seen := map[string]bool{}
	for seed := uint64(1); seed <= 20; seed++ {
		seen[order(seed)] = true
	}
	if len(seen) < 2 {
		t.Errorf("20 seeds produced %d order(s), want ties shuffled per seed", len(seen))
	}
}

func TestSeedFrom_IsStablePerID(t *testing.T) {
	a := "8f1c3b7a-5d2e-4c1f-9a6b-2e7d1c4f8a90"
	b := "9d8c7b6a-5f4e-4d3c-8b2a-1f0e9d8c7b60"
	first, second := seedFrom(a), seedFrom(a)
	if first != second {
		t.Error("seedFrom is not stable for one id")
	}
	if seedFrom(a) == seedFrom(b) {
		t.Error("seedFrom gives two ids the same seed")
	}
}

// eligible is what the old root test showed with bay types and skills: a bay of any allowed type
// and a technician with every required skill, nobody else, and the plan keeps exactly those.
func TestEligible_AnyAllowedBayTypeAndEveryRequiredSkill(t *testing.T) {
	bays, techs := []scheduling.ServiceBay{bay1, bay2, bay3}, []scheduling.Technician{sam, priya, lee}
	tests := []struct {
		name      string
		req       scheduling.ServiceRequirement
		wantBays  []string
		wantTechs []string
	}{
		{"INTERIM: GENERAL or MOT bays, any GENERAL_SERVICE technician", interim,
			[]string{"bay-1", "bay-2"}, []string{"sam", "priya", "lee"}},
		{"EV_MAJOR: the EV_HV bay, Sam only", evMajor, []string{"bay-3"}, []string{"sam"}},
		{"MOT: the MOT bay, Priya only", mot, []string{"bay-2"}, []string{"priya"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := plan{cfg: man01, req: tt.req}

			bayIDs, techIDs, err := p.eligible(bays, techs, "2026-10-20")

			if err != nil || !slices.Equal(bayIDs, tt.wantBays) || !slices.Equal(techIDs, tt.wantTechs) {
				t.Errorf("eligible = %v, %v, %v; want %v, %v", bayIDs, techIDs, err, tt.wantBays, tt.wantTechs)
			}
			if len(p.bays) != len(tt.wantBays) || len(p.techs) != len(tt.wantTechs) {
				t.Errorf("plan keeps bays %v and technicians %v, want only the eligible ones", p.bays, p.techs)
			}
		})
	}
}

func TestEligible_NamedResourcesNarrowToThemAndAreChecked(t *testing.T) {
	bays, techs := []scheduling.ServiceBay{bay1, bay2, bay3}, []scheduling.Technician{sam, priya, lee}
	t.Run("a named bay and technician are the only ones left", func(t *testing.T) {
		p := plan{cfg: man01, req: interim, requiredBayID: "bay-2", requiredTechnicianID: "priya"}

		bayIDs, techIDs, err := p.eligible(bays, techs, "2026-10-20")

		if err != nil || !slices.Equal(bayIDs, []string{"bay-2"}) || !slices.Equal(techIDs, []string{"priya"}) {
			t.Errorf("eligible = %v, %v, %v; want bay-2 and priya", bayIDs, techIDs, err)
		}
	})
	t.Run("a technician without the skills", func(t *testing.T) {
		p := plan{cfg: man01, req: mot, requiredTechnicianID: "lee"}

		_, _, err := p.eligible(bays, techs, "2026-10-20")

		const want = "technician lee does not hold MOT_TESTER on 2026-10-20: no qualified technician"
		if !errors.Is(err, scheduling.ErrNoQualifiedTechnician) || err.Error() != want {
			t.Errorf("error = %q, want %q", err, want)
		}
	})
	t.Run("nobody holds the skills", func(t *testing.T) {
		p := plan{cfg: man01, req: aircon}

		_, _, err := p.eligible(bays, techs, "2026-10-20")

		const want = "no active technician at MAN01 holds AIRCON_FGAS on 2026-10-20: no qualified technician"
		if !errors.Is(err, scheduling.ErrNoQualifiedTechnician) || err.Error() != want {
			t.Errorf("error = %q, want %q", err, want)
		}
	})
	t.Run("the bay is judged before the technician", func(t *testing.T) {
		p := plan{cfg: man01, req: mot, requiredBayID: "bay-1", requiredTechnicianID: "nobody"}

		_, _, err := p.eligible(bays, techs, "2026-10-20")

		requireField(t, err, "INVALID_VALUE", "serviceBayId")
	})
}

func pairSet(pairs []candidate) []string {
	var s []string
	for _, p := range pairs {
		s = append(s, p.BayID+"/"+p.TechID)
	}
	slices.Sort(s)
	return s
}

func pairOrder(pairs []candidate) string {
	var s string
	for _, p := range pairs {
		s += p.BayID + "/" + p.TechID + " "
	}
	return s
}
