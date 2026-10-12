package scheduling

import (
	"testing"
	"time"
)

func TestServiceRequirement_RunsIncludeBuffersRoundedToCells(t *testing.T) {
	tests := []struct {
		name      string
		req       ServiceRequirement
		start     string
		wantFirst string
		wantCells int
	}{
		{"INTERIM 45 min, no buffers", ServiceRequirement{Duration: 45 * time.Minute}, "08:00", "08:00", 3},
		{"MAJOR 90 min, 15/15", ServiceRequirement{Duration: 90 * time.Minute, BufferBefore: 15 * time.Minute, BufferAfter: 15 * time.Minute}, "10:00", "09:45", 8},
		{"EV_MAJOR 120 min, 15/15", ServiceRequirement{Duration: 120 * time.Minute, BufferBefore: 15 * time.Minute, BufferAfter: 15 * time.Minute}, "08:00", "07:45", 10},
		{"DIAGNOSTIC 50 min rounds up", ServiceRequirement{Duration: 50 * time.Minute}, "13:00", "13:00", 4},
		{"10-minute buffers round up to whole cells", ServiceRequirement{Duration: 90 * time.Minute, BufferBefore: 10 * time.Minute, BufferAfter: 10 * time.Minute}, "08:00", "07:45", 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bay, tech := tt.req.Runs(z(t, tt.start), grid15)
			want := run(z(t, tt.wantFirst), tt.wantCells)
			if !bay.First.Equal(want.First) || bay.Cells != want.Cells || bay.Slot != want.Slot {
				t.Errorf("bay run = %+v, want %+v", bay, want)
			}
			if !tech.First.Equal(bay.First) || tech.Cells != bay.Cells || tech.Slot != bay.Slot {
				t.Errorf("technician run = %+v, want it equal to the bay run %+v", tech, bay)
			}
		})
	}
}

func TestDealershipConfig_DayIsTheLocalDay(t *testing.T) {
	cfg := DealershipConfig{Location: london, Grid: grid15}
	tests := []struct {
		name     string
		at       time.Time
		date     string
		from, to time.Time
	}{
		{"summer time", time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC), "2026-10-20",
			time.Date(2026, 10, 19, 23, 0, 0, 0, time.UTC), time.Date(2026, 10, 20, 23, 0, 0, 0, time.UTC)},
		{"after 23:00Z in summer is the next local day", time.Date(2026, 10, 20, 23, 30, 0, 0, time.UTC), "2026-10-21",
			time.Date(2026, 10, 20, 23, 0, 0, 0, time.UTC), time.Date(2026, 10, 21, 23, 0, 0, 0, time.UTC)},
		{"the 25-hour change day", time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC), "2026-10-25",
			time.Date(2026, 10, 24, 23, 0, 0, 0, time.UTC), time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)},
		{"winter time", time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC), "2026-11-02",
			time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cfg.Day(tt.at)
			if got.LocalDate != tt.date || !got.From.Equal(tt.from) || !got.To.Equal(tt.to) {
				t.Errorf("Day(%s) = %+v, want %s [%s, %s)", tt.at, got, tt.date, tt.from, tt.to)
			}
		})
	}
}

func TestTechnician_HasAllRequiredSkills(t *testing.T) {
	sam := Technician{ID: "sam", Skills: []string{"GENERAL_SERVICE", "EV_HV_L3"}}
	tests := []struct {
		skills []string
		want   bool
	}{
		{nil, true},
		{[]string{"GENERAL_SERVICE"}, true},
		{[]string{"EV_HV_L3", "GENERAL_SERVICE"}, true},
		{[]string{"MOT_TESTER"}, false},
		{[]string{"GENERAL_SERVICE", "MOT_TESTER"}, false},
	}
	for _, tt := range tests {
		if got := sam.HasAll(tt.skills); got != tt.want {
			t.Errorf("HasAll(%v) = %v, want %v", tt.skills, got, tt.want)
		}
	}
}
