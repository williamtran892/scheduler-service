package scheduling

import (
	"slices"
	"testing"
	"time"
)

// The day used throughout: 2026-10-20 in Europe/London (BST), cells 07:00Z to 17:00Z.
var testDay = DayWindow{
	LocalDate: "2026-10-20",
	From:      time.Date(2026, 10, 19, 23, 0, 0, 0, time.UTC),
	To:        time.Date(2026, 10, 20, 23, 0, 0, 0, time.UTC),
}

var (
	bay1  = ServiceBay{ID: "bay-1", Name: "Bay 1", Type: "GENERAL"}
	bay2  = ServiceBay{ID: "bay-2", Name: "Bay 2", Type: "MOT"}
	bay3  = ServiceBay{ID: "bay-3", Name: "Bay 3", Type: "EV_HV"}
	sam   = Technician{ID: "sam", Name: "Sam", Skills: []string{"GENERAL_SERVICE", "EV_HV_L3"}}
	priya = Technician{ID: "priya", Name: "Priya", Skills: []string{"GENERAL_SERVICE", "MOT_TESTER"}}
	lee   = Technician{ID: "lee", Name: "Lee", Skills: []string{"GENERAL_SERVICE"}}

	interim = ServiceRequirement{ServiceTypeID: "interim", Code: "INTERIM", Duration: 45 * time.Minute,
		RequiredSkills: []string{"GENERAL_SERVICE"}, AllowedBayTypes: []string{"GENERAL", "MOT"}, OnlineBookable: true, Active: true}
	major = ServiceRequirement{ServiceTypeID: "major", Code: "MAJOR", Duration: 90 * time.Minute,
		BufferBefore: 15 * time.Minute, BufferAfter: 15 * time.Minute,
		RequiredSkills: []string{"GENERAL_SERVICE"}, AllowedBayTypes: []string{"GENERAL"}, OnlineBookable: true, Active: true}
	evCheck = ServiceRequirement{ServiceTypeID: "ev-check", Code: "EV_CHECK", Duration: 30 * time.Minute,
		RequiredSkills: []string{"EV_HV_L3", "GENERAL_SERVICE"}, AllowedBayTypes: []string{"GENERAL", "EV_HV"}, OnlineBookable: true, Active: true}
)

func island(t *testing.T, kind ResourceKind, id, from, to string) Island {
	return Island{Kind: kind, ResourceID: id, From: z(t, from), To: z(t, to)}
}

// wholeDay gives every resource one island 07:00Z to 17:00Z.
func wholeDay(t *testing.T, bays []ServiceBay, techs []Technician) []Island {
	var is []Island
	for _, b := range bays {
		is = append(is, island(t, ResourceBay, b.ID, "07:00", "17:00"))
	}
	for _, tech := range techs {
		is = append(is, island(t, ResourceTechnician, tech.ID, "07:00", "17:00"))
	}
	return is
}

// startsEvery lists grid starts from..to inclusive, as UTC times on the test day.
func startsEvery(t *testing.T, from, to string) []time.Time {
	var out []time.Time
	for s := z(t, from); !s.After(z(t, to)); s = s.Add(15 * time.Minute) {
		out = append(out, s)
	}
	return out
}

// The design's named case (design 5.1, notes 4.3): Bay 1 free until 09:15 and Bay 2 free from
// 09:15 do not make a 30-minute slot at 09:00, because neither bay holds the whole run.
func TestFeasibleStarts_RunSplitAcrossTwoBaysIsNotASlot(t *testing.T) {
	general2 := ServiceBay{ID: "bay-2", Name: "Bay 2", Type: "GENERAL"}
	req := ServiceRequirement{Duration: 30 * time.Minute, RequiredSkills: []string{"GENERAL_SERVICE"}, AllowedBayTypes: []string{"GENERAL"}}
	islands := []Island{
		island(t, ResourceBay, "bay-1", "08:00", "09:15"),
		island(t, ResourceBay, "bay-1", "10:00", "17:00"),
		island(t, ResourceBay, "bay-2", "09:15", "17:00"),
		island(t, ResourceTechnician, "sam", "08:00", "17:00"),
	}

	got := FeasibleStarts(testDay, grid15, req, []ServiceBay{bay1, general2}, []Technician{sam}, islands)

	want := append(startsEvery(t, "08:00", "08:45"), startsEvery(t, "09:15", "16:30")...)
	if !slices.EqualFunc(got, want, time.Time.Equal) {
		t.Errorf("FeasibleStarts = %v\nwant %v (everything but 09:00)", got, want)
	}
	if pairs := CandidatePairs(z(t, "09:00"), grid15, req, []ServiceBay{bay1, general2}, []Technician{sam}, islands, Loads{}, 1); len(pairs) != 0 {
		t.Errorf("CandidatePairs at 09:00 = %v, want none", pairs)
	}
}

func TestCandidatePairs_EachTechnicianMustHoldTheWholeRun(t *testing.T) {
	islands := []Island{
		island(t, ResourceBay, "bay-1", "07:00", "17:00"),
		island(t, ResourceTechnician, "sam", "08:00", "08:30"),
		island(t, ResourceTechnician, "lee", "08:30", "17:00"),
	}
	if pairs := CandidatePairs(z(t, "08:00"), grid15, interim, []ServiceBay{bay1}, []Technician{sam, lee}, islands, Loads{}, 1); len(pairs) != 0 {
		t.Errorf("CandidatePairs = %v, want none: no single technician is free 08:00-08:45", pairs)
	}
}

func TestCandidatePairs_AnyAllowedBayTypeAndEveryRequiredSkill(t *testing.T) {
	bays, techs := []ServiceBay{bay1, bay2, bay3}, []Technician{sam, priya, lee}
	islands := wholeDay(t, bays, techs)

	interimPairs := CandidatePairs(z(t, "08:00"), grid15, interim, bays, techs, islands, Loads{}, 1)
	if got := pairSet(interimPairs); !slices.Equal(got, []string{
		"bay-1/lee", "bay-1/priya", "bay-1/sam", "bay-2/lee", "bay-2/priya", "bay-2/sam"}) {
		t.Errorf("INTERIM pairs = %v, want GENERAL or MOT bays with any GENERAL_SERVICE technician", got)
	}

	evPairs := CandidatePairs(z(t, "08:00"), grid15, evCheck, bays, techs, islands, Loads{}, 1)
	if got := pairSet(evPairs); !slices.Equal(got, []string{"bay-1/sam", "bay-3/sam"}) {
		t.Errorf("EV_CHECK pairs = %v, want Bay 1 or Bay 3 with Sam only", got)
	}
}

func TestCandidatePairs_BuffersMustFitOnTheLedger(t *testing.T) {
	bays, techs := []ServiceBay{bay1}, []Technician{lee}
	opensAt8 := []Island{island(t, ResourceBay, "bay-1", "08:00", "17:00"), island(t, ResourceTechnician, "lee", "08:00", "17:00")}

	if pairs := CandidatePairs(z(t, "08:00"), grid15, major, bays, techs, opensAt8, Loads{}, 1); len(pairs) != 0 {
		t.Errorf("MAJOR at 08:00 = %v, want none: its 15-minute buffer starts at 07:45", pairs)
	}
	if pairs := CandidatePairs(z(t, "08:15"), grid15, major, bays, techs, opensAt8, Loads{}, 1); len(pairs) != 1 {
		t.Errorf("MAJOR at 08:15 = %v, want one pair", pairs)
	}
}

// The scenario "The least-loaded qualified bay and technician get the job": Sam holds a 10-cell
// EV_MAJOR on Bay 3, Priya a 3-cell MOT on Bay 2. Load = bay cells + technician cells that day.
func TestCandidatePairs_LeastLoadedFirst(t *testing.T) {
	bays, techs := []ServiceBay{bay1, bay2, bay3}, []Technician{sam, priya, lee}
	loads := Loads{"bay-3": 10, "sam": 10, "bay-2": 3, "priya": 3}

	pairs := CandidatePairs(z(t, "12:00"), grid15, interim, bays, techs, wholeDay(t, bays, techs), loads, 42)

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
	bays, techs := []ServiceBay{bay1, bay2}, []Technician{sam, priya, lee}
	islands := wholeDay(t, bays, techs)
	order := func(seed uint64) string {
		return pairOrder(CandidatePairs(z(t, "08:00"), grid15, interim, bays, techs, islands, Loads{}, seed))
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
	first, second := SeedFrom(a), SeedFrom(a)
	if first != second {
		t.Error("SeedFrom is not stable for one id")
	}
	if SeedFrom(a) == SeedFrom(b) {
		t.Error("SeedFrom gives two ids the same seed")
	}
}

func TestNearestAlternatives(t *testing.T) {
	starts := []time.Time{z(t, "06:45"), z(t, "07:00"), z(t, "07:15"), z(t, "08:00"), z(t, "08:45"),
		z(t, "09:00"), z(t, "09:15"), z(t, "11:00")}
	now := z(t, "06:50")

	got := NearestAlternatives(starts, z(t, "08:00"), now, 5)

	// 06:45 is past; 08:00 is the requested start; 07:15 and 08:45 tie at 45 minutes, earlier
	// first; then 07:00 and 09:00 at 60; then 09:15 at 75; 11:00 falls beyond the five.
	want := []time.Time{z(t, "07:15"), z(t, "08:45"), z(t, "07:00"), z(t, "09:00"), z(t, "09:15")}
	if !slices.EqualFunc(got, want, time.Time.Equal) {
		t.Errorf("NearestAlternatives = %v, want %v", got, want)
	}
	if got := NearestAlternatives(nil, z(t, "08:00"), now, 5); len(got) != 0 {
		t.Errorf("no starts gave %v, want none", got)
	}
}

func pairSet(pairs []Candidate) []string {
	var s []string
	for _, p := range pairs {
		s = append(s, p.BayID+"/"+p.TechID)
	}
	slices.Sort(s)
	return s
}

func pairOrder(pairs []Candidate) string {
	var s string
	for _, p := range pairs {
		s += p.BayID + "/" + p.TechID + " "
	}
	return s
}
