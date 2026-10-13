package scheduling

import (
	"slices"
	"time"
)

// Runs returns the bay run and the technician run for a start: both begin at
// start - BufferBefore and cover BufferBefore + Duration + BufferAfter, rounded up to whole
// cells. They are identical in this release (design 5.1).
func (r ServiceRequirement) Runs(start time.Time, g Grid) (bay, tech CellRun) {
	first := g.Floor(start.Add(-r.BufferBefore)) // the cell the before-buffer begins in
	end := start.Add(r.Duration + r.BufferAfter)
	run := CellRun{First: first, Cells: g.CellsFor(end.Sub(first)), Slot: g.Slot()}
	return run, run
}

// Day returns the local day, in the dealership's time zone, that contains t.
func (c DealershipConfig) Day(t time.Time) DayWindow {
	local := t.In(c.Location)
	from := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, c.Location)
	to := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, c.Location)
	return DayWindow{LocalDate: from.Format(time.DateOnly), From: from.UTC(), To: to.UTC()}
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
