package scheduling

import (
	"errors"
	"testing"
	"time"
)

func TestGrid_AlignAcceptsOnlyCellStarts(t *testing.T) {
	tests := []struct {
		name  string
		grid  Grid
		start time.Time
		ok    bool
	}{
		{"top of the hour", grid15, z(t, "08:00"), true},
		{"quarter past", grid15, z(t, "08:15"), true},
		{"local offset on the grid", grid15, time.Date(2026, 10, 20, 9, 45, 0, 0, london), true},
		{"five past", grid15, z(t, "08:05"), false},
		{"seconds", grid15, z(t, "08:00").Add(30 * time.Second), false},
		{"fraction of a second", grid15, z(t, "08:00").Add(time.Millisecond), false},
		{"quarter past on a 30-minute grid", Grid{SlotMinutes: 30}, z(t, "08:15"), false},
		{"half past on a 30-minute grid", Grid{SlotMinutes: 30}, z(t, "08:30"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.grid.Align(tt.start)
			if tt.ok {
				if err != nil || !got.Equal(tt.start) {
					t.Errorf("Align(%s) = %s, %v; want the same instant, nil", tt.start, got, err)
				}
				return
			}
			if !errors.Is(err, ErrOffGrid) {
				t.Errorf("Align(%s) error = %v, want ErrOffGrid", tt.start, err)
			}
		})
	}
}

func TestGrid_CellsForRoundsUp(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want int
	}{
		{0, 0}, {time.Minute, 1}, {15 * time.Minute, 1}, {45 * time.Minute, 3},
		{50 * time.Minute, 4}, {78 * time.Minute, 6}, {150 * time.Minute, 10},
	}
	for _, tt := range tests {
		if got := grid15.CellsFor(tt.d); got != tt.want {
			t.Errorf("CellsFor(%s) = %d, want %d", tt.d, got, tt.want)
		}
	}
	if got := grid15.Slot(); got != 15*time.Minute {
		t.Errorf("Slot() = %s, want 15m", got)
	}
}

func TestCellRun_LastAndEnd(t *testing.T) {
	r := run(z(t, "07:45"), 8) // a MAJOR at 08:00Z with 15/15 buffers

	if got := r.Last(); !got.Equal(z(t, "09:30")) {
		t.Errorf("Last() = %s, want 09:30Z", got)
	}
	if got := r.End(); !got.Equal(z(t, "09:45")) {
		t.Errorf("End() = %s, want 09:45Z", got)
	}
}

func TestGrid_FloorIsTheStartOfTheCellContainingT(t *testing.T) {
	g := Grid{SlotMinutes: 15}
	at := func(hhmm string, sec, nsec int) time.Time {
		c, _ := time.Parse("15:04", hhmm)
		return time.Date(2026, 10, 20, c.Hour(), c.Minute(), sec, nsec, time.UTC)
	}
	tests := []struct {
		name string
		t    time.Time
		want time.Time
	}{
		{"inside a cell", at("09:40", 0, 0), at("09:30", 0, 0)},
		{"a cell start is its own floor", at("09:30", 0, 0), at("09:30", 0, 0)},
		{"just before the next cell", at("09:44", 59, 999_999_999), at("09:30", 0, 0)},
		{"in another offset, the same instant", at("09:40", 0, 0).In(time.FixedZone("BST", 3600)), at("09:30", 0, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := g.Floor(tt.t); !got.Equal(tt.want) {
				t.Errorf("Floor(%s) = %s, want %s", tt.t, got, tt.want)
			}
		})
	}
}

func TestGrid_CeilIsTOnTheGridElseTheNextCellStart(t *testing.T) {
	g := Grid{SlotMinutes: 15}
	at := func(hhmm string, sec, nsec int) time.Time {
		c, _ := time.Parse("15:04", hhmm)
		return time.Date(2026, 10, 20, c.Hour(), c.Minute(), sec, nsec, time.UTC)
	}
	tests := []struct {
		name string
		t    time.Time
		want time.Time
	}{
		{"inside a cell rounds up (design 11.3: complete at 09:40)", at("09:40", 0, 0), at("09:45", 0, 0)},
		{"a cell start stays", at("09:45", 0, 0), at("09:45", 0, 0)},
		{"half a second past a cell start rounds up", at("09:45", 0, 500_000_000), at("10:00", 0, 0)},
		{"across the hour", at("09:59", 0, 0), at("10:00", 0, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := g.Ceil(tt.t); !got.Equal(tt.want) {
				t.Errorf("Ceil(%s) = %s, want %s", tt.t, got, tt.want)
			}
		})
	}
}
