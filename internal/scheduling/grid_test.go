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

func TestCellRun_LastEndWithin(t *testing.T) {
	r := run(z(t, "07:45"), 8) // a MAJOR at 08:00Z with 15/15 buffers

	if got := r.Last(); !got.Equal(z(t, "09:30")) {
		t.Errorf("Last() = %s, want 09:30Z", got)
	}
	if got := r.End(); !got.Equal(z(t, "09:45")) {
		t.Errorf("End() = %s, want 09:45Z", got)
	}

	tests := []struct {
		name     string
		from, to string
		want     bool
	}{
		{"whole day", "07:00", "17:00", true},
		{"exact fit", "07:45", "09:45", true},
		{"island starts one cell late", "08:00", "17:00", false},
		{"island ends one cell early", "07:00", "09:30", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := Island{Kind: ResourceBay, ResourceID: "bay-1", From: z(t, tt.from), To: z(t, tt.to)}
			if got := r.Within(i); got != tt.want {
				t.Errorf("Within(%s-%s) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}
