package endpoint

import (
	"context"
	"net/http"

	kitendpoint "github.com/go-kit/kit/endpoint"

	"scheduler/internal/scheduling/service"
)

// NoShow is POST /appointments/{appointmentId}/no-show: it marks a CONFIRMED appointment NO_SHOW
// and answers 200 with the record.
func NoShow(svc service.Service) EndpointSpec {
	return EndpointSpec{
		HTTPMethod:   http.MethodPost,
		PathTemplate: "/appointments/{appointmentId}/no-show",
		Endpoint:     MakeNoShowEndpoint(svc),
		Decode:       decodeNoShow,
		Encode:       encodeRecord,
	}
}

// MakeNoShowEndpoint adapts svc.MarkNoShow.
func MakeNoShowEndpoint(svc service.Service) kitendpoint.Endpoint {
	return makeCommandEndpoint(svc.MarkNoShow)
}

// decodeNoShow carries nothing beyond TransitionRequest.
func decodeNoShow(_ context.Context, r *http.Request) (any, error) { return decodePlain(r) }
