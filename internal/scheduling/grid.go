// Package scheduling is the Scheduling domain root: the ledger's grid algebra, the booking
// types, the candidate-pair search and the data-access contract (Store, Tx) together with what
// Scheduling needs from other domains (WorkshopReader, CustomerReader). It imports only the
// standard library (implementation notes 2).
package scheduling

import (
	"fmt"
	"time"
)

// Grid is the dealership's booking grid: cells of SlotMinutes anchored to the top of the UTC
// hour (design 11.1).
type Grid struct {
	SlotMinutes int
}

// Slot is the length of one cell.
func (g Grid) Slot() time.Duration { return time.Duration(g.SlotMinutes) * time.Minute }

// Align returns t if it starts a cell (a whole multiple of the slot from the top of the UTC
// hour, zero seconds), otherwise ErrOffGrid.
func (g Grid) Align(t time.Time) (time.Time, error) {
	u := t.UTC()
	if u.Second() != 0 || u.Nanosecond() != 0 || u.Minute()%g.SlotMinutes != 0 {
		return time.Time{}, fmt.Errorf("%w: %d-minute cells, got %s", ErrOffGrid, g.SlotMinutes, u.Format(time.RFC3339Nano))
	}
	return t, nil
}

// Floor returns the start of the cell that contains t.
func (g Grid) Floor(t time.Time) time.Time {
	panic("not implemented")
}

// Ceil returns t if it starts a cell, otherwise the start of the next cell.
func (g Grid) Ceil(t time.Time) time.Time {
	panic("not implemented")
}

// CellsFor returns the number of cells needed to cover d, rounded up.
func (g Grid) CellsFor(d time.Duration) int {
	return int((d + g.Slot() - 1) / g.Slot())
}

// CellRun is a run of consecutive cells on one resource.
type CellRun struct {
	First time.Time
	Cells int
	Slot  time.Duration
}

// Last is the start of the run's last cell.
func (r CellRun) Last() time.Time { return r.First.Add(time.Duration(r.Cells-1) * r.Slot) }

// End is the end of the run's last cell (exclusive).
func (r CellRun) End() time.Time { return r.First.Add(time.Duration(r.Cells) * r.Slot) }

// Within reports whether the whole run lies inside the island [From, To).
func (r CellRun) Within(i Island) bool { return !r.First.Before(i.From) && !r.End().After(i.To) }
