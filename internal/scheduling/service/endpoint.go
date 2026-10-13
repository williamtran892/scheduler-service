package service

import (
	"context"

	"github.com/go-kit/kit/endpoint"

	"scheduler/internal/scheduling"
)

// Endpoints holds one go-kit endpoint per Service method.
type Endpoints struct {
	BookEndpoint endpoint.Endpoint
	GetEndpoint  endpoint.Endpoint

	RescheduleEndpoint endpoint.Endpoint
	CancelEndpoint     endpoint.Endpoint
	CheckInEndpoint    endpoint.Endpoint
	StartEndpoint      endpoint.Endpoint
	CompleteEndpoint   endpoint.Endpoint
	NoShowEndpoint     endpoint.Endpoint
}

// MakeServerEndpoints adapts s to endpoints. Business failures travel as the response's Err.
func MakeServerEndpoints(s Service) Endpoints {
	return Endpoints{
		BookEndpoint:       makeBookEndpoint(s),
		GetEndpoint:        makeGetEndpoint(s),
		RescheduleEndpoint: makeCommandEndpoint(s.Reschedule),
		CancelEndpoint:     makeCommandEndpoint(s.Cancel),
		CheckInEndpoint:    makeCommandEndpoint(s.CheckIn),
		StartEndpoint:      makeCommandEndpoint(s.Start),
		CompleteEndpoint:   makeCommandEndpoint(s.Complete),
		NoShowEndpoint:     makeCommandEndpoint(s.MarkNoShow),
	}
}

// bookRequest is a decoded POST /appointments; the command carries the calling channel.
type bookRequest struct {
	Command BookCommand
}

// getRequest is a decoded GET /appointments/{appointmentId}. The channel decides the view the
// response is rendered in; the service does not need it to read the record.
type getRequest struct {
	Query   GetQuery
	Channel scheduling.Channel
}

// commandRequest is a decoded lifecycle request: the command, and the calling channel that decides
// the view the response is rendered in.
type commandRequest[C any] struct {
	Command C
	Channel scheduling.Channel
}

// recordResponse is the answer of every endpoint: the appointment as the caller's channel sees
// it, or the business failure.
type recordResponse struct {
	Appointment AppointmentView
	Err         error
}

// Failed implements endpoint.Failer.
func (r recordResponse) Failed() error { return r.Err }

func makeBookEndpoint(s Service) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(bookRequest)
		rec, err := s.Book(ctx, req.Command)
		if err != nil {
			return recordResponse{Err: err}, nil
		}
		return recordResponse{Appointment: NewAppointmentView(rec, req.Command.Channel)}, nil
	}
}

func makeGetEndpoint(s Service) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(getRequest)
		rec, err := s.Get(ctx, req.Query)
		if err != nil {
			return recordResponse{Err: err}, nil
		}
		return recordResponse{Appointment: NewAppointmentView(rec, req.Channel)}, nil
	}
}

// makeCommandEndpoint adapts one lifecycle method: every transition answers with the appointment
// after it, in the calling channel's view.
func makeCommandEndpoint[C any](call func(context.Context, C) (Record, error)) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(commandRequest[C])
		rec, err := call(ctx, req.Command)
		if err != nil {
			return recordResponse{Err: err}, nil
		}
		return recordResponse{Appointment: NewAppointmentView(rec, req.Channel)}, nil
	}
}
