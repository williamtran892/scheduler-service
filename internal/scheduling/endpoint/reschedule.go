package endpoint

import (
	"context"
	"net/http"

	kitendpoint "github.com/go-kit/kit/endpoint"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling/service"
)

// Reschedule is POST /appointments/{appointmentId}/reschedule: it moves a CONFIRMED appointment in
// time, to other resources, or both, and answers 200 with the record.
func Reschedule(svc service.Service) EndpointSpec {
	return EndpointSpec{
		HTTPMethod:   http.MethodPost,
		PathTemplate: "/appointments/{appointmentId}/reschedule",
		Endpoint:     MakeRescheduleEndpoint(svc),
		Decode:       decodeReschedule,
		Encode:       encodeRecord,
	}
}

// MakeRescheduleEndpoint adapts svc.Reschedule.
func MakeRescheduleEndpoint(svc service.Service) kitendpoint.Endpoint {
	return makeCommandEndpoint(svc.Reschedule)
}

// decodeReschedule needs at least one of startAt, technicianId and serviceBayId; naming nothing
// changes nothing, so it is 400 naming startAt.
func decodeReschedule(_ context.Context, r *http.Request) (any, error) {
	return decodeTransition(r, func(b transitionBody, tc service.TransitionCommand) (service.RescheduleCommand, error) {
		cmd := service.RescheduleCommand{TransitionCommand: tc}
		var err error
		if cmd.StartAt, err = parseOptionalInstant("startAt", b.StartAt); err != nil {
			return cmd, err
		}
		if b.TechnicianID != "" {
			if cmd.TechnicianID, err = parseUUID("technicianId", b.TechnicianID); err != nil {
				return cmd, err
			}
		}
		if b.ServiceBayID != "" {
			if cmd.ServiceBayID, err = parseUUID("serviceBayId", b.ServiceBayID); err != nil {
				return cmd, err
			}
		}
		if cmd.StartAt == nil && cmd.TechnicianID == "" && cmd.ServiceBayID == "" {
			return cmd, platform.BadRequest("startAt", "at least one of startAt, technicianId and serviceBayId is required")
		}
		return cmd, nil
	})
}
