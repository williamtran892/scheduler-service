// Package endpoint is the HTTP surface of the Scheduling service, in two files. endpoint.go holds
// the route table (Specs), Register, and the go-kit endpoint of every operation; facade.go holds
// what faces the wire: the request decoders, the response encoders and the parsing they share. No
// route is registered anywhere else.
package endpoint

import (
	"context"
	"net/http"

	kitendpoint "github.com/go-kit/kit/endpoint"
	kithttp "github.com/go-kit/kit/transport/http"

	"scheduler/internal/scheduling"
	"scheduler/internal/scheduling/service"
)

//go:generate go tool mockgen -destination=mock_service_test.go -package=endpoint scheduler/internal/scheduling/service Service

// EndpointSpec describes one HTTP operation: where it is mounted and how a request becomes a call
// of the service and its answer a response.
type EndpointSpec struct {
	HTTPMethod   string
	PathTemplate string // an http.ServeMux pattern, for example /appointments/{appointmentId}/check-in
	Endpoint     kitendpoint.Endpoint
	Decode       kithttp.DecodeRequestFunc
	Encode       kithttp.EncodeResponseFunc
}

// Specs returns every operation of the Scheduling service: book, get and the six lifecycle
// transitions, one row per route. Business failures travel as the response's Err.
func Specs(svc service.Service) []EndpointSpec {
	return []EndpointSpec{
		// Confirms an appointment and answers 201 with the record and its Location.
		{
			HTTPMethod:   http.MethodPost,
			PathTemplate: "/appointments",
			Endpoint:     MakeBookEndpoint(svc),
			Decode:       decodeBookRequest,
			Encode:       encodeCreated,
		},

		// Reads one appointment, with its audit trail when ?include=events, and answers 200 with the
		// record.
		{
			HTTPMethod:   http.MethodGet,
			PathTemplate: "/appointments/{appointmentId}",
			Endpoint:     MakeGetEndpoint(svc),
			Decode:       decodeGetRequest,
			Encode:       encodeRecord,
		},

		// Moves a CONFIRMED appointment in time, to other resources, or both, and answers 200 with the
		// record.
		{
			HTTPMethod:   http.MethodPost,
			PathTemplate: "/appointments/{appointmentId}/reschedule",
			Endpoint:     MakeRescheduleEndpoint(svc),
			Decode:       decodeReschedule,
			Encode:       encodeRecord,
		},

		// Cancels a CONFIRMED or CHECKED_IN appointment and answers 200 with the record.
		{
			HTTPMethod:   http.MethodPost,
			PathTemplate: "/appointments/{appointmentId}/cancel",
			Endpoint:     MakeCancelEndpoint(svc),
			Decode:       decodeCancel,
			Encode:       encodeRecord,
		},

		// Checks a CONFIRMED appointment in, or a NO_SHOW one in as a late arrival, and answers 200
		// with the record.
		{
			HTTPMethod:   http.MethodPost,
			PathTemplate: "/appointments/{appointmentId}/check-in",
			Endpoint:     MakeCheckInEndpoint(svc),
			Decode:       decodeCheckIn,
			Encode:       encodeRecord,
		},

		// Starts a CHECKED_IN appointment and answers 200 with the record.
		{
			HTTPMethod:   http.MethodPost,
			PathTemplate: "/appointments/{appointmentId}/start",
			Endpoint:     MakeStartEndpoint(svc),
			Decode:       decodeStart,
			Encode:       encodeRecord,
		},

		// Completes an IN_PROGRESS appointment and answers 200 with the record.
		{
			HTTPMethod:   http.MethodPost,
			PathTemplate: "/appointments/{appointmentId}/complete",
			Endpoint:     MakeCompleteEndpoint(svc),
			Decode:       decodeComplete,
			Encode:       encodeRecord,
		},

		// Marks a CONFIRMED appointment NO_SHOW and answers 200 with the record.
		{
			HTTPMethod:   http.MethodPost,
			PathTemplate: "/appointments/{appointmentId}/no-show",
			Endpoint:     MakeNoShowEndpoint(svc),
			Decode:       decodeNoShow,
			Encode:       encodeRecord,
		},
	}
}

// Register mounts every spec on mux at its method and path. Decoder failures are problems too, so
// the caller passes the server option that encodes errors.
func Register(mux *http.ServeMux, specs []EndpointSpec, opts ...kithttp.ServerOption) {
	for _, spec := range specs {
		mux.Handle(spec.HTTPMethod+" "+spec.PathTemplate, kithttp.NewServer(spec.Endpoint, spec.Decode, spec.Encode, opts...))
	}
}

// bookRequest is a decoded POST /appointments; the command carries the calling channel.
type bookRequest struct {
	Command service.BookCommand
}

// MakeBookEndpoint adapts svc.Book. Business failures travel as the response's Err.
func MakeBookEndpoint(svc service.Service) kitendpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(bookRequest)
		rec, err := svc.Book(ctx, req.Command)
		if err != nil {
			return recordResponse{Err: err}, nil
		}
		return recordResponse{Appointment: service.NewAppointmentView(rec, req.Command.Channel)}, nil
	}
}

// getRequest is a decoded GET /appointments/{appointmentId}. The channel decides the view the
// response is rendered in; the service does not need it to read the record.
type getRequest struct {
	Query   service.GetQuery
	Channel scheduling.Channel
}

// MakeGetEndpoint adapts svc.Get. Business failures travel as the response's Err.
func MakeGetEndpoint(svc service.Service) kitendpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(getRequest)
		rec, err := svc.Get(ctx, req.Query)
		if err != nil {
			return recordResponse{Err: err}, nil
		}
		return recordResponse{Appointment: service.NewAppointmentView(rec, req.Channel)}, nil
	}
}

// The six lifecycle operations share one request shape and one endpoint adapter; each Make
// function below only names the service method to adapt. Their decoder is in facade.go.

// commandRequest is a decoded lifecycle request: the command, and the calling channel that decides
// the view the response is rendered in.
type commandRequest[C any] struct {
	Command C
	Channel scheduling.Channel
}

// makeCommandEndpoint adapts one lifecycle method: every transition answers with the appointment
// after it, in the calling channel's view.
func makeCommandEndpoint[C any](call func(context.Context, C) (service.Record, error)) kitendpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(commandRequest[C])
		rec, err := call(ctx, req.Command)
		if err != nil {
			return recordResponse{Err: err}, nil
		}
		return recordResponse{Appointment: service.NewAppointmentView(rec, req.Channel)}, nil
	}
}

// MakeRescheduleEndpoint adapts svc.Reschedule.
func MakeRescheduleEndpoint(svc service.Service) kitendpoint.Endpoint {
	return makeCommandEndpoint(svc.Reschedule)
}

// MakeCancelEndpoint adapts svc.Cancel.
func MakeCancelEndpoint(svc service.Service) kitendpoint.Endpoint {
	return makeCommandEndpoint(svc.Cancel)
}

// MakeCheckInEndpoint adapts svc.CheckIn.
func MakeCheckInEndpoint(svc service.Service) kitendpoint.Endpoint {
	return makeCommandEndpoint(svc.CheckIn)
}

// MakeStartEndpoint adapts svc.Start.
func MakeStartEndpoint(svc service.Service) kitendpoint.Endpoint {
	return makeCommandEndpoint(svc.Start)
}

// MakeCompleteEndpoint adapts svc.Complete.
func MakeCompleteEndpoint(svc service.Service) kitendpoint.Endpoint {
	return makeCommandEndpoint(svc.Complete)
}

// MakeNoShowEndpoint adapts svc.MarkNoShow.
func MakeNoShowEndpoint(svc service.Service) kitendpoint.Endpoint {
	return makeCommandEndpoint(svc.MarkNoShow)
}
