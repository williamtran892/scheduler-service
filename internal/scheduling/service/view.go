package service

import "scheduler/internal/scheduling"

// AppointmentView is the contract's Appointment schema as written on the wire.
type AppointmentView struct{}

// NewAppointmentView renders r for the calling channel: staff channels see the owned cells, the
// internal notes and the technician's name; customer channels do not.
func NewAppointmentView(r Record, ch scheduling.Channel) AppointmentView { panic("not implemented") }
