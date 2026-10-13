package endpoint

import (
	"context"
	"fmt"
	"net/http"
	"unicode/utf8"

	kitendpoint "github.com/go-kit/kit/endpoint"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling/service"
)

// maxConcerns is the text limit of the contract's CheckInRequest.additionalConcerns.
const maxConcerns = 4000

// CheckIn is POST /appointments/{appointmentId}/check-in: it checks a CONFIRMED appointment in, or
// a NO_SHOW one in as a late arrival, and answers 200 with the record.
func CheckIn(svc service.Service) EndpointSpec {
	return EndpointSpec{
		HTTPMethod:   http.MethodPost,
		PathTemplate: "/appointments/{appointmentId}/check-in",
		Endpoint:     MakeCheckInEndpoint(svc),
		Decode:       decodeCheckIn,
		Encode:       encodeRecord,
	}
}

// MakeCheckInEndpoint adapts svc.CheckIn.
func MakeCheckInEndpoint(svc service.Service) kitendpoint.Endpoint {
	return makeCommandEndpoint(svc.CheckIn)
}

func decodeCheckIn(_ context.Context, r *http.Request) (any, error) {
	return decodeTransition(r, func(b transitionBody, tc service.TransitionCommand) (service.CheckInCommand, error) {
		mileage, err := parseInteger("mileage", b.Mileage, false)
		if err != nil {
			return service.CheckInCommand{}, err
		}
		if mileage != nil && *mileage < 0 {
			return service.CheckInCommand{}, platform.InvalidValue("mileage", "must not be negative")
		}
		if utf8.RuneCountInString(b.AdditionalConcerns) > maxConcerns {
			return service.CheckInCommand{}, platform.InvalidValue("additionalConcerns", fmt.Sprintf("must be at most %d characters", maxConcerns))
		}
		return service.CheckInCommand{TransitionCommand: tc, Mileage: mileage, AdditionalConcerns: b.AdditionalConcerns}, nil
	})
}
