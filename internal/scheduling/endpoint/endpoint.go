// Package endpoint is the HTTP surface of the Scheduling service. Each operation has one file
// holding its go-kit endpoint, its request decoder and the EndpointSpec that describes its route;
// decode.go and encode.go hold what the operations share. Specs lists every operation and
// Register mounts them, so no route is registered anywhere else.
package endpoint

import (
	"net/http"

	kitendpoint "github.com/go-kit/kit/endpoint"
	kithttp "github.com/go-kit/kit/transport/http"

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
// transitions. Business failures travel as the response's Err.
func Specs(svc service.Service) []EndpointSpec {
	return []EndpointSpec{
		Book(svc),
		Get(svc),
		Reschedule(svc),
		Cancel(svc),
		CheckIn(svc),
		Start(svc),
		Complete(svc),
		NoShow(svc),
	}
}

// Register mounts every spec on mux at its method and path. Decoder failures are problems too, so
// the caller passes the server option that encodes errors.
func Register(mux *http.ServeMux, specs []EndpointSpec, opts ...kithttp.ServerOption) {
	for _, spec := range specs {
		mux.Handle(spec.HTTPMethod+" "+spec.PathTemplate, kithttp.NewServer(spec.Endpoint, spec.Decode, spec.Encode, opts...))
	}
}
