package service

import (
	"encoding/json"
	"errors"
	"testing"

	"scheduler/internal/scheduling"
)

func TestGet_UnknownAppointmentIsNotFound(t *testing.T) {
	f := newFixture(t)

	_, err := f.service().Get(ctx, GetQuery{AppointmentID: "nope"})

	if !errors.Is(err, scheduling.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestGet_ReturnsTheRecordWithNamesAndOwnedCells(t *testing.T) {
	f := newFixture(t)
	a := storedAppointment("interim", scheduling.StatusConfirmed)
	f.stored[a.ID] = a
	owned := scheduling.OwnedCells{Bay: &scheduling.CellRange{From: on("08:00"), To: on("08:45"), Cells: 3}}
	f.owned[a.ID] = owned

	rec, err := f.service().Get(ctx, GetQuery{AppointmentID: a.ID})

	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.Appointment.ID != a.ID || rec.Dealership.Code != "MAN01" || rec.ServiceType.Code != "INTERIM" ||
		rec.Customer != alice || rec.Vehicle != aliceCar || rec.Bay != bay1 || rec.Technician.ID != "lee" || rec.Cells != owned {
		t.Errorf("record = %+v", rec)
	}
	if rec.Events != nil {
		t.Errorf("events = %v, want none unless asked", rec.Events)
	}
}

func TestGet_ReadsTheAuditTrailOnlyWhenAsked(t *testing.T) {
	f := newFixture(t)
	a := storedAppointment("interim", scheduling.StatusConfirmed)
	f.stored[a.ID] = a
	events := []scheduling.Event{{ID: 1, Type: "CONFIRMED", Actor: "anonymous", Channel: scheduling.ChannelAdvisorConsole,
		OccurredAt: now, Payload: json.RawMessage(`{}`)}}
	f.store.EXPECT().Events(ctx, a.ID).Return(events, nil)

	rec, err := f.service().Get(ctx, GetQuery{AppointmentID: a.ID, IncludeEvents: true})

	if err != nil || len(rec.Events) != 1 || rec.Events[0].Type != "CONFIRMED" {
		t.Errorf("Get = %+v, %v; want the CONFIRMED event", rec.Events, err)
	}
}
