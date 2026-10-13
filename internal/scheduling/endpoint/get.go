package endpoint

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	kitendpoint "github.com/go-kit/kit/endpoint"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
	"scheduler/internal/scheduling/service"
)

// Get is GET /appointments/{appointmentId}: it reads one appointment, with its audit trail when
// ?include=events, and answers 200 with the record.
func Get(svc service.Service) EndpointSpec {
	return EndpointSpec{
		HTTPMethod:   http.MethodGet,
		PathTemplate: "/appointments/{appointmentId}",
		Endpoint:     MakeGetEndpoint(svc),
		Decode:       decodeGetRequest,
		Encode:       encodeRecord,
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

// decodeGetRequest parses GET /appointments/{appointmentId}[?include=events]. An id that is not a
// UUID cannot name an appointment, so it is NOT_FOUND without asking the service.
func decodeGetRequest(_ context.Context, r *http.Request) (any, error) {
	channel, err := channelOf(r)
	if err != nil {
		return nil, err
	}
	raw := r.PathValue("appointmentId")
	id, ok := canonicalUUID(raw)
	if !ok {
		return nil, fmt.Errorf("appointment %s: %w", raw, scheduling.ErrNotFound)
	}
	events, err := includesEvents(r.URL.Query().Get("include"))
	if err != nil {
		return nil, err
	}
	return getRequest{Query: service.GetQuery{AppointmentID: id, IncludeEvents: events}, Channel: channel}, nil
}

// includesEvents reads the include query parameter, a comma-separated list whose only value is
// events.
func includesEvents(include string) (bool, error) {
	events := false
	for _, v := range strings.Split(include, ",") {
		switch v {
		case "":
		case "events":
			events = true
		default:
			return false, platform.InvalidValue("include", fmt.Sprintf("unknown value %q (want events)", v))
		}
	}
	return events, nil
}
