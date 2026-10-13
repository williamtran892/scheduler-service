package endpoint

import (
	"fmt"
	"time"

	"scheduler/internal/scheduling"
	"scheduler/internal/scheduling/service"
)

// The record the transport tests answer with: design Appendix B, an EV major service 09:00 to
// 11:00 London on Bay 3 with Sam, owning 10 cells from 08:45 to 11:15 on each resource. It mirrors
// the fixture of the service package's tests, which an external package cannot import.

var (
	london = mustLocation("Europe/London")
	now    = time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC)
)

const (
	tenant = "tenant-1"
	apptID = "a1b2c3d4-0000-4000-8000-000000000001"
)

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// on returns an instant on 2026-10-20 from a UTC "HH:MM" (09:00 London is "08:00").
func on(hhmm string) time.Time {
	c, err := time.Parse("15:04", hhmm)
	if err != nil {
		panic(fmt.Sprintf("bad time %q", hhmm))
	}
	return time.Date(2026, 10, 20, c.Hour(), c.Minute(), 0, 0, time.UTC)
}

func evRecord() service.Record {
	yes, no := true, false
	a := scheduling.Appointment{
		ID: apptID, TenantID: tenant, DealershipID: "man01", CustomerID: "alice", VehicleID: "yk21",
		ServiceTypeID: "ev-major", Kind: scheduling.KindDropOff, StartAt: on("08:00"), EndAt: on("10:00"),
		ServiceBayID: "bay-3", TechnicianID: "sam", Status: scheduling.StatusConfirmed,
		CustomerRequests: scheduling.CustomerRequests{Waiting: &no, CourtesyCar: &yes, Notes: "Charging port cover loose"},
		Notes:            "Customer prefers a morning call when ready.", Channel: scheduling.ChannelAdvisorConsole,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	cells := &scheduling.CellRange{From: on("07:45"), To: on("10:15"), Cells: 10}
	techCells := *cells
	return service.Record{
		Appointment: a,
		Dealership: scheduling.DealershipConfig{DealershipID: "man01", TenantID: tenant, Code: "MAN01", Name: "Manchester Central",
			Location: london, Grid: scheduling.Grid{SlotMinutes: 15}, BookingHorizonDays: 28},
		ServiceType: scheduling.ServiceRequirement{ServiceTypeID: "ev-major", TenantID: tenant, Code: "EV_MAJOR", Name: "EV_MAJOR",
			Duration: 120 * time.Minute, BufferBefore: 15 * time.Minute, BufferAfter: 15 * time.Minute,
			RequiredSkills: []string{"EV_HV_L3"}, AllowedBayTypes: []string{"EV_HV"}, OnlineBookable: true, Active: true},
		Customer:   scheduling.Customer{ID: "alice", TenantID: tenant, DisplayName: "Alice"},
		Vehicle:    scheduling.Vehicle{ID: "yk21", TenantID: tenant, CustomerID: "alice", Registration: "YK21 ABC", Make: "Volkswagen", Model: "ID.4"},
		Bay:        scheduling.ServiceBay{ID: "bay-3", Name: "Bay 3", Type: "EV_HV"},
		Technician: scheduling.Technician{ID: "sam", Name: "Sam", Skills: []string{"GENERAL_SERVICE", "EV_HV_L3"}},
		Cells:      scheduling.OwnedCells{Bay: cells, Technician: &techCells},
	}
}

// storedAppointment makes an appointment that exists already, the owner of a booking key.
func storedAppointment(serviceTypeID string, status scheduling.AppointmentStatus) scheduling.Appointment {
	return scheduling.Appointment{
		ID: "existing-1", TenantID: tenant, DealershipID: "man01", CustomerID: "alice", VehicleID: "yk21",
		ServiceTypeID: serviceTypeID, Kind: scheduling.KindDropOff, StartAt: on("08:00"), EndAt: on("08:45"),
		ServiceBayID: "bay-1", TechnicianID: "lee", Status: status,
		CustomerRequests: scheduling.CustomerRequests{Notes: "Rattle from rear left"},
		Channel:          scheduling.ChannelAdvisorConsole, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
}

// get reads a nested value of a decoded JSON object.
func get(m map[string]any, path ...string) any {
	var v any = m
	for _, p := range path {
		o, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = o[p]
	}
	return v
}
