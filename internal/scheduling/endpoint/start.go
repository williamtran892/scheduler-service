package endpoint

import (
	"context"
	"net/http"

	kitendpoint "github.com/go-kit/kit/endpoint"

	"scheduler/internal/scheduling/service"
)

// Start is POST /appointments/{appointmentId}/start: it starts a CHECKED_IN appointment and
// answers 200 with the record.
func Start(svc service.Service) EndpointSpec {
	return EndpointSpec{
		HTTPMethod:   http.MethodPost,
		PathTemplate: "/appointments/{appointmentId}/start",
		Endpoint:     MakeStartEndpoint(svc),
		Decode:       decodeStart,
		Encode:       encodeRecord,
	}
}

// MakeStartEndpoint adapts svc.Start.
func MakeStartEndpoint(svc service.Service) kitendpoint.Endpoint {
	return makeCommandEndpoint(svc.Start)
}

func decodeStart(_ context.Context, r *http.Request) (any, error) {
	return decodeTransition(r, func(b transitionBody, tc service.TransitionCommand) (service.StartCommand, error) {
		at, err := parseOptionalInstant("startedAt", b.StartedAt)
		return service.StartCommand{TransitionCommand: tc, StartedAt: at}, err
	})
}
