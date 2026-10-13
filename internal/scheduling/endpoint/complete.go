package endpoint

import (
	"context"
	"net/http"

	kitendpoint "github.com/go-kit/kit/endpoint"

	"scheduler/internal/scheduling/service"
)

// Complete is POST /appointments/{appointmentId}/complete: it completes an IN_PROGRESS
// appointment and answers 200 with the record.
func Complete(svc service.Service) EndpointSpec {
	return EndpointSpec{
		HTTPMethod:   http.MethodPost,
		PathTemplate: "/appointments/{appointmentId}/complete",
		Endpoint:     MakeCompleteEndpoint(svc),
		Decode:       decodeComplete,
		Encode:       encodeRecord,
	}
}

// MakeCompleteEndpoint adapts svc.Complete.
func MakeCompleteEndpoint(svc service.Service) kitendpoint.Endpoint {
	return makeCommandEndpoint(svc.Complete)
}

func decodeComplete(_ context.Context, r *http.Request) (any, error) {
	return decodeTransition(r, func(b transitionBody, tc service.TransitionCommand) (service.CompleteCommand, error) {
		at, err := parseOptionalInstant("completedAt", b.CompletedAt)
		return service.CompleteCommand{TransitionCommand: tc, CompletedAt: at}, err
	})
}
