package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	kitendpoint "github.com/go-kit/kit/endpoint"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
	"scheduler/internal/scheduling/service"
)

// Text limits of the contract: BookAppointmentRequest.notes and CustomerRequests.notes.
const (
	maxNotes        = 4000
	maxRequestNotes = 2000
)

// Book is POST /appointments: it confirms an appointment and answers 201 with the record and its
// Location.
func Book(svc service.Service) EndpointSpec {
	return EndpointSpec{
		HTTPMethod:   http.MethodPost,
		PathTemplate: "/appointments",
		Endpoint:     MakeBookEndpoint(svc),
		Decode:       decodeBookRequest,
		Encode:       encodeCreated,
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

// bookBody is the wire form of BookAppointmentRequest. Identifiers and the start are strings so
// the decoder can check each one and every rejection names its field. A missing string field and
// an empty one both decode to "" and are both answered as required.
type bookBody struct {
	DealershipID     string                      `json:"dealershipId"`
	ServiceTypeID    string                      `json:"serviceTypeId"`
	CustomerID       string                      `json:"customerId"`
	VehicleID        string                      `json:"vehicleId"`
	StartAt          string                      `json:"startAt"`
	Kind             string                      `json:"kind"`
	CustomerRequests scheduling.CustomerRequests `json:"customerRequests"`
	Notes            string                      `json:"notes"`
}

// decodeBookRequest parses and validates a booking request. The first problem found is the
// answer: the channel header, the JSON shape, required fields, identifier and start formats (all
// 400 VALIDATION_ERROR), then enumerations and text lengths (422 INVALID_VALUE).
func decodeBookRequest(_ context.Context, r *http.Request) (any, error) {
	channel, err := channelOf(r)
	if err != nil {
		return nil, err
	}
	var body bookBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("%w: body: %w", platform.ErrBadRequest, err)
	}

	dealershipID, err := parseUUID("dealershipId", body.DealershipID)
	if err != nil {
		return nil, err
	}
	serviceTypeID, err := parseUUID("serviceTypeId", body.ServiceTypeID)
	if err != nil {
		return nil, err
	}
	customerID, err := parseUUID("customerId", body.CustomerID)
	if err != nil {
		return nil, err
	}
	vehicleID, err := parseUUID("vehicleId", body.VehicleID)
	if err != nil {
		return nil, err
	}
	start, err := parseStart(body.StartAt)
	if err != nil {
		return nil, err
	}
	if body.Kind == "" {
		return nil, platform.BadRequest("kind", "is required")
	}

	kind, err := scheduling.ParseAppointmentKind(body.Kind)
	if err != nil {
		return nil, platform.InvalidValue("kind", fmt.Sprintf("unknown kind %q", body.Kind))
	}
	if utf8.RuneCountInString(body.Notes) > maxNotes {
		return nil, platform.InvalidValue("notes", fmt.Sprintf("must be at most %d characters", maxNotes))
	}
	if utf8.RuneCountInString(body.CustomerRequests.Notes) > maxRequestNotes {
		return nil, platform.InvalidValue("customerRequests.notes", fmt.Sprintf("must be at most %d characters", maxRequestNotes))
	}

	return bookRequest{Command: service.BookCommand{
		DealershipID:     dealershipID,
		ServiceTypeID:    serviceTypeID,
		CustomerID:       customerID,
		VehicleID:        vehicleID,
		StartAt:          start,
		Kind:             kind,
		CustomerRequests: body.CustomerRequests,
		Notes:            body.Notes,
		Channel:          channel,
	}}, nil
}

// parseStart reads the required startAt of a booking.
func parseStart(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, platform.BadRequest("startAt", "is required")
	}
	return parseInstant("startAt", value)
}
