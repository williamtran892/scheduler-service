package scheduling

import (
	"testing"
	"time"
)

var london = func() *time.Location {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		panic(err)
	}
	return loc
}()

// z returns an instant on 2026-10-20 (a BST day: local = UTC + 1) from a UTC "HH:MM".
func z(t *testing.T, hhmm string) time.Time {
	t.Helper()
	c, err := time.Parse("15:04", hhmm)
	if err != nil {
		t.Fatalf("bad time %q", hhmm)
	}
	return time.Date(2026, 10, 20, c.Hour(), c.Minute(), 0, 0, time.UTC)
}

var grid15 = Grid{SlotMinutes: 15}

func run(first time.Time, cells int) CellRun {
	return CellRun{First: first, Cells: cells, Slot: 15 * time.Minute}
}
