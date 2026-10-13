package endpoint

import (
	"context"
	"net/http"

	kitendpoint "github.com/go-kit/kit/endpoint"

	"scheduler/internal/scheduling/service"
)

// Cancel is POST /appointments/{appointmentId}/cancel: it cancels a CONFIRMED or CHECKED_IN
// appointment and answers 200 with the record.
func Cancel(svc service.Service) EndpointSpec {
	return EndpointSpec{
		HTTPMethod:   http.MethodPost,
		PathTemplate: "/appointments/{appointmentId}/cancel",
		Endpoint:     MakeCancelEndpoint(svc),
		Decode:       decodeCancel,
		Encode:       encodeRecord,
	}
}

// MakeCancelEndpoint adapts svc.Cancel.
func MakeCancelEndpoint(svc service.Service) kitendpoint.Endpoint {
	return makeCommandEndpoint(svc.Cancel)
}

// decodeCancel carries nothing beyond TransitionRequest.
func decodeCancel(_ context.Context, r *http.Request) (any, error) { return decodePlain(r) }
