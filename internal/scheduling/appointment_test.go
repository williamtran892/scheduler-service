package scheduling

import (
	"testing"
	"time"
)

// Keep is the filter ReleaseCells applies to an appointment's cells (research R3).
func TestKeep_KeepsOnlyWhatItNames(t *testing.T) {
	day := func(hhmm string) time.Time {
		c, _ := time.Parse("15:04", hhmm)
		return time.Date(2026, 10, 20, c.Hour(), c.Minute(), 0, 0, time.UTC)
	}
	run := func(first string, cells int) CellRun {
		return CellRun{First: day(first), Cells: cells, Slot: 15 * time.Minute}
	}
	reschedule := Keep{
		Bay:        &ResourceRun{ResourceID: "bay-2", Run: run("09:30", 3)},
		Technician: &ResourceRun{ResourceID: "lee", Run: run("09:30", 3)},
	}
	complete := Keep{Before: day("09:45")} // completed at 09:40: Ceil is 09:45

	tests := []struct {
		name  string
		keep  Keep
		kind  ResourceKind
		id    string
		start string
		want  bool
	}{
		{"zero Keep releases a bay cell", Keep{}, ResourceBay, "bay-2", "09:00", false},
		{"zero Keep releases a technician cell", Keep{}, ResourceTechnician, "priya", "09:00", false},

		{"reschedule keeps the new bay run's first cell", reschedule, ResourceBay, "bay-2", "09:30", true},
		{"reschedule keeps the new bay run's last cell", reschedule, ResourceBay, "bay-2", "10:00", true},
		{"reschedule releases the bay cell after the run", reschedule, ResourceBay, "bay-2", "10:15", false},
		{"reschedule releases the bay cell before the run", reschedule, ResourceBay, "bay-2", "09:15", false},
		{"reschedule releases the same time on another bay", reschedule, ResourceBay, "bay-1", "09:30", false},
		{"reschedule keeps the new technician's cell", reschedule, ResourceTechnician, "lee", "09:45", true},
		{"reassign releases the previous technician at the same time", reschedule, ResourceTechnician, "priya", "09:45", false},
		{"a bay id under the technician kind is not kept", reschedule, ResourceTechnician, "bay-2", "09:45", false},

		{"complete keeps a past cell", complete, ResourceBay, "bay-1", "09:00", true},
		{"complete keeps the cell now falls in", complete, ResourceTechnician, "sam", "09:30", true},
		{"complete releases the cell starting at Ceil(now)", complete, ResourceBay, "bay-1", "09:45", false},
		{"complete releases a later cell", complete, ResourceTechnician, "sam", "10:00", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.keep.Keeps(tt.kind, tt.id, day(tt.start)); got != tt.want {
				t.Errorf("Keeps(%s %s %s) = %v, want %v", tt.kind, tt.id, tt.start, got, tt.want)
			}
		})
	}
}
