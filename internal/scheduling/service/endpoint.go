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
}

// MakeServerEndpoints adapts s to endpoints. Business failures travel as the response's Err.
func MakeServerEndpoints(s Service) Endpoints {
	return Endpoints{BookEndpoint: makeBookEndpoint(s), GetEndpoint: makeGetEndpoint(s)}
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

// recordResponse is the answer of both endpoints: the appointment as the caller's channel sees
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
