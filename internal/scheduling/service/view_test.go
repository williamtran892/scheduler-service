package service

import (
	"encoding/json"
	"testing"

	"scheduler/internal/scheduling"
)

// The Appointment schema of api/openapi.yaml, checked on the wire form.

func viewJSON(t *testing.T, r Record, ch scheduling.Channel) map[string]any {
	t.Helper()
	b, err := json.Marshal(NewAppointmentView(r, ch))
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal view: %v", err)
	}
	return m
}

// evRecord is design Appendix B: an EV major service 09:00 to 11:00 London on Bay 3 with Sam,
// owning 10 cells from 08:45 to 11:15 on each resource.
func evRecord() Record {
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
	return Record{Appointment: a, Dealership: man01, ServiceType: evMajor, Customer: alice, Vehicle: aliceCar,
		Bay: bay3, Technician: sam, Cells: scheduling.OwnedCells{Bay: cells, Technician: &techCells}}
}

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

func TestView_StaffSeeTheWholeRecord(t *testing.T) {
	m := viewJSON(t, evRecord(), scheduling.ChannelAdvisorConsole)

	want := map[string]any{
		"id": apptID, "status": "CONFIRMED", "kind": "DROP_OFF", "channel": "ADVISOR_CONSOLE", "version": float64(1),
		"startAt": "2026-10-20T09:00:00+01:00", "endAt": "2026-10-20T11:00:00+01:00", "dealershipId": "man01",
		"createdAt": "2026-10-19T08:00:00Z", "updatedAt": "2026-10-19T08:00:00Z",
		"notes": "Customer prefers a morning call when ready.",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %v, want %v", k, m[k], v)
		}
	}
	nested := map[string]any{
		"dealership.id": "man01", "dealership.code": "MAN01", "dealership.name": "Manchester Central", "dealership.timezone": "Europe/London",
		"customer.id": "alice", "customer.displayName": "Alice",
		"vehicle.id": "yk21", "vehicle.registration": "YK21 ABC", "vehicle.make": "Volkswagen", "vehicle.model": "ID.4",
		"serviceType.id": "ev-major", "serviceType.code": "EV_MAJOR", "serviceType.name": "EV_MAJOR", "serviceType.durationMinutes": float64(120),
		"serviceBay.id": "bay-3", "serviceBay.name": "Bay 3", "serviceBay.type": "EV_HV",
		"technician.id": "sam", "technician.displayName": "Sam",
		"cells.bay.from": "2026-10-20T08:45:00+01:00", "cells.bay.to": "2026-10-20T11:15:00+01:00", "cells.bay.cells": float64(10),
		"cells.technician.from": "2026-10-20T08:45:00+01:00", "cells.technician.to": "2026-10-20T11:15:00+01:00", "cells.technician.cells": float64(10),
		"customerRequests.waiting": false, "customerRequests.courtesyCar": true, "customerRequests.notes": "Charging port cover loose",
	}
	for path, v := range nested {
		if got := get(m, splitPath(path)...); got != v {
			t.Errorf("%s = %v, want %v", path, got, v)
		}
	}
	for _, k := range []string{"actualStartAt", "actualEndAt"} {
		if v, ok := m[k]; !ok || v != nil {
			t.Errorf("%s = %v (present %v), want null", k, v, ok)
		}
	}
	if _, ok := m["events"]; ok {
		t.Error("events present without include=events")
	}
}

func TestView_CustomersSeeNoCellsNotesOrTechnicianName(t *testing.T) {
	for _, ch := range []scheduling.Channel{scheduling.ChannelCustomerWeb, scheduling.ChannelCustomerApp} {
		m := viewJSON(t, evRecord(), ch)
		if _, ok := m["cells"]; ok {
			t.Errorf("%s: cells present", ch)
		}
		if _, ok := m["notes"]; ok {
			t.Errorf("%s: notes present", ch)
		}
		if get(m, "technician", "id") != "sam" {
			t.Errorf("%s: technician.id = %v, want sam", ch, get(m, "technician", "id"))
		}
		if _, ok := m["technician"].(map[string]any)["displayName"]; ok {
			t.Errorf("%s: technician.displayName present", ch)
		}
		if get(m, "customerRequests", "notes") != "Charging port cover loose" {
			t.Errorf("%s: customer requests hidden; they are the customer's own", ch)
		}
	}
}

func TestView_EventsWhenRead(t *testing.T) {
	r := evRecord()
	r.Events = []scheduling.Event{{ID: 7, Type: "CONFIRMED", Actor: "anonymous", Channel: scheduling.ChannelAdvisorConsole,
		OccurredAt: now, Payload: json.RawMessage(`{"id":"x"}`)}}

	m := viewJSON(t, r, scheduling.ChannelAdvisorConsole)

	events, _ := m["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("events = %v, want one", m["events"])
	}
	e := events[0].(map[string]any)
	if e["id"] != float64(7) || e["type"] != "CONFIRMED" || e["actor"] != "anonymous" || e["channel"] != "ADVISOR_CONSOLE" ||
		e["occurredAt"] != "2026-10-19T08:00:00Z" || get(e, "payload", "id") != "x" {
		t.Errorf("event = %v", e)
	}
}

func splitPath(p string) []string {
	var out []string
	start := 0
	for i := 0; i < len(p); i++ {
		if p[i] == '.' {
			out = append(out, p[start:i])
			start = i + 1
		}
	}
	return append(out, p[start:])
}

func TestView_ActualTimesAreRenderedInTheDealershipOffsetOnceSet(t *testing.T) {
	r := evRecord()
	started, ended := on("08:05"), on("09:50")
	r.Appointment.ActualStartAt, r.Appointment.ActualEndAt = &started, &ended
	for _, ch := range []scheduling.Channel{scheduling.ChannelAdvisorConsole, scheduling.ChannelCustomerWeb} {
		m := viewJSON(t, r, ch)
		if got := m["actualStartAt"]; got != "2026-10-20T09:05:00+01:00" {
			t.Errorf("%s: actualStartAt = %v, want 2026-10-20T09:05:00+01:00", ch, got)
		}
		if got := m["actualEndAt"]; got != "2026-10-20T10:50:00+01:00" {
			t.Errorf("%s: actualEndAt = %v, want 2026-10-20T10:50:00+01:00", ch, got)
		}
	}

	r.Appointment.ActualEndAt = nil
	m := viewJSON(t, r, scheduling.ChannelAdvisorConsole)
	if v, ok := m["actualEndAt"]; !ok || v != nil {
		t.Errorf("actualEndAt = %v (present %v), want null before completion", v, ok)
	}
}
