package service

import (
	"encoding/json"
	"time"

	"scheduler/internal/scheduling"
)

// AppointmentView is the contract's Appointment schema as written on the wire. Times of the
// appointment and its cells are in the dealership's offset; createdAt, updatedAt and the events
// are in UTC.
type AppointmentView struct {
	ID               string                       `json:"id"`
	Status           scheduling.AppointmentStatus `json:"status"`
	Kind             scheduling.AppointmentKind   `json:"kind"`
	StartAt          time.Time                    `json:"startAt"`
	EndAt            time.Time                    `json:"endAt"`
	DealershipID     string                       `json:"dealershipId"`
	Dealership       dealershipView               `json:"dealership"`
	Customer         customerView                 `json:"customer"`
	Vehicle          vehicleView                  `json:"vehicle"`
	ServiceType      serviceTypeView              `json:"serviceType"`
	ServiceBay       bayView                      `json:"serviceBay"`
	Technician       technicianView               `json:"technician"`
	Cells            *cellsView                   `json:"cells,omitempty"`
	CustomerRequests scheduling.CustomerRequests  `json:"customerRequests"`
	Notes            string                       `json:"notes,omitempty"`
	Channel          scheduling.Channel           `json:"channel"`
	ActualStartAt    *time.Time                   `json:"actualStartAt"` // null until the job starts
	ActualEndAt      *time.Time                   `json:"actualEndAt"`   // null until the job is completed
	Version          int                          `json:"version"`
	CreatedAt        time.Time                    `json:"createdAt"`
	UpdatedAt        time.Time                    `json:"updatedAt"`
	Events           []eventView                  `json:"events,omitempty"`
}

type dealershipView struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
}

type customerView struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type vehicleView struct {
	ID           string `json:"id"`
	Registration string `json:"registration"`
	Make         string `json:"make,omitempty"`
	Model        string `json:"model,omitempty"`
}

type serviceTypeView struct {
	ID              string `json:"id"`
	Code            string `json:"code"`
	Name            string `json:"name"`
	DurationMinutes int    `json:"durationMinutes"`
}

type bayView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type technicianView struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
}

type cellsView struct {
	Bay        *cellRangeView `json:"bay,omitempty"`
	Technician *cellRangeView `json:"technician,omitempty"`
}

type cellRangeView struct {
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
	Cells int       `json:"cells"`
}

type eventView struct {
	ID         int64              `json:"id"`
	Type       string             `json:"type"`
	Actor      string             `json:"actor"`
	Channel    scheduling.Channel `json:"channel,omitempty"`
	OccurredAt time.Time          `json:"occurredAt"`
	Payload    json.RawMessage    `json:"payload,omitempty"`
}

// NewAppointmentView renders r for the calling channel: staff channels see the owned cells, the
// internal notes, the technician's name and the events' payloads; customer channels do not.
func NewAppointmentView(r Record, ch scheduling.Channel) AppointmentView {
	return newView(r, !ch.IsCustomer())
}

// newView renders r with or without the staff-only fields.
func newView(r Record, staff bool) AppointmentView {
	a, loc := r.Appointment, r.Dealership.Location
	v := AppointmentView{
		ID:           a.ID,
		Status:       a.Status,
		Kind:         a.Kind,
		StartAt:      a.StartAt.In(loc),
		EndAt:        a.EndAt.In(loc),
		DealershipID: a.DealershipID,
		Dealership: dealershipView{ID: r.Dealership.DealershipID, Code: r.Dealership.Code,
			Name: r.Dealership.Name, Timezone: loc.String()},
		Customer: customerView{ID: r.Customer.ID, DisplayName: r.Customer.DisplayName},
		Vehicle: vehicleView{ID: r.Vehicle.ID, Registration: r.Vehicle.Registration,
			Make: r.Vehicle.Make, Model: r.Vehicle.Model},
		ServiceType: serviceTypeView{ID: r.ServiceType.ServiceTypeID, Code: r.ServiceType.Code,
			Name: r.ServiceType.Name, DurationMinutes: int(r.ServiceType.Duration / time.Minute)},
		ServiceBay:       bayView{ID: r.Bay.ID, Name: r.Bay.Name, Type: r.Bay.Type},
		Technician:       technicianView{ID: r.Technician.ID},
		CustomerRequests: a.CustomerRequests,
		Channel:          a.Channel,
		ActualStartAt:    inZone(a.ActualStartAt, loc),
		ActualEndAt:      inZone(a.ActualEndAt, loc),
		Version:          a.Version,
		CreatedAt:        a.CreatedAt.UTC(),
		UpdatedAt:        a.UpdatedAt.UTC(),
	}
	if staff {
		v.Technician.DisplayName = r.Technician.Name
		v.Notes = a.Notes
		v.Cells = &cellsView{Bay: rangeView(r.Cells.Bay, loc), Technician: rangeView(r.Cells.Technician, loc)}
	}
	for _, e := range r.Events {
		ev := eventView{ID: e.ID, Type: e.Type, Actor: e.Actor, Channel: e.Channel, OccurredAt: e.OccurredAt.UTC()}
		if staff {
			ev.Payload = e.Payload // the payload is a staff snapshot: it holds cells and notes
		}
		v.Events = append(v.Events, ev)
	}
	return v
}

// inZone renders an optional instant in loc.
func inZone(t *time.Time, loc *time.Location) *time.Time {
	if t == nil {
		return nil
	}
	v := t.In(loc)
	return &v
}

func rangeView(r *scheduling.CellRange, loc *time.Location) *cellRangeView {
	if r == nil {
		return nil
	}
	return &cellRangeView{From: r.From.In(loc), To: r.To.In(loc), Cells: r.Cells}
}
