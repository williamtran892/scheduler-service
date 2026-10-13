package scheduling

import (
	"fmt"
	"time"
)

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
	return t.Truncate(g.Slot())
}

// Ceil returns t if it starts a cell, otherwise the start of the next cell.
func (g Grid) Ceil(t time.Time) time.Time {
	if f := g.Floor(t); !f.Equal(t) {
		return f.Add(g.Slot())
	}
	return t
}

// CellsFor returns the number of cells needed to cover d, rounded up.
func (g Grid) CellsFor(d time.Duration) int {
	return int((d + g.Slot() - 1) / g.Slot())
}

// Last is the start of the run's last cell.
func (r CellRun) Last() time.Time { return r.First.Add(time.Duration(r.Cells-1) * r.Slot) }

// End is the end of the run's last cell (exclusive).
func (r CellRun) End() time.Time { return r.First.Add(time.Duration(r.Cells) * r.Slot) }
