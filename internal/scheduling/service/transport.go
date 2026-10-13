package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-kit/kit/endpoint"
	kithttp "github.com/go-kit/kit/transport/http"
	"github.com/google/uuid"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
)

// Text limits of the contract: BookAppointmentRequest.notes, CustomerRequests.notes,
// TransitionRequest.reason and CheckInRequest.additionalConcerns.
const (
	maxNotes         = 4000
	maxRequestNotes  = 2000
	maxReason        = 500
	maxConcerns      = 4000
	channelHeader    = "X-Channel"
	appointmentsPath = "/appointments/"
)

// RegisterRoutes puts the appointment routes on mux: book, get and the six lifecycle transitions.
func RegisterRoutes(mux *http.ServeMux, e Endpoints, opts ...kithttp.ServerOption) {
	mux.Handle("POST /appointments", kithttp.NewServer(e.BookEndpoint, decodeBookRequest, encodeCreated, opts...))
	mux.Handle("GET /appointments/{appointmentId}", kithttp.NewServer(e.GetEndpoint, decodeGetRequest, encodeRecord, opts...))
	mux.Handle("POST /appointments/{appointmentId}/reschedule", kithttp.NewServer(e.RescheduleEndpoint, decodeReschedule, encodeRecord, opts...))
	mux.Handle("POST /appointments/{appointmentId}/cancel", kithttp.NewServer(e.CancelEndpoint, decodeCancel, encodeRecord, opts...))
	mux.Handle("POST /appointments/{appointmentId}/check-in", kithttp.NewServer(e.CheckInEndpoint, decodeCheckIn, encodeRecord, opts...))
	mux.Handle("POST /appointments/{appointmentId}/start", kithttp.NewServer(e.StartEndpoint, decodeStart, encodeRecord, opts...))
	mux.Handle("POST /appointments/{appointmentId}/complete", kithttp.NewServer(e.CompleteEndpoint, decodeComplete, encodeRecord, opts...))
	mux.Handle("POST /appointments/{appointmentId}/no-show", kithttp.NewServer(e.NoShowEndpoint, decodeNoShow, encodeRecord, opts...))
}

// bookBody is the wire form of BookAppointmentRequest. Identifiers and the start are strings so
// a missing field can be told from an empty one and every rejection names its field.
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

	return bookRequest{Command: BookCommand{
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
	return getRequest{Query: GetQuery{AppointmentID: id, IncludeEvents: events}, Channel: channel}, nil
}

// transitionBody is the wire form of the lifecycle request bodies (TransitionRequest and what the
// contract adds per operation). expectedVersion and mileage stay raw so a value that is not an
// integer can be told from a missing one; instants are strings so a rejection names its field.
type transitionBody struct {
	ExpectedVersion    json.RawMessage `json:"expectedVersion"`
	Reason             string          `json:"reason"`
	StartAt            string          `json:"startAt"`
	TechnicianID       string          `json:"technicianId"`
	ServiceBayID       string          `json:"serviceBayId"`
	Mileage            json.RawMessage `json:"mileage"`
	AdditionalConcerns string          `json:"additionalConcerns"`
	StartedAt          string          `json:"startedAt"`
	CompletedAt        string          `json:"completedAt"`
}

// decodeTransition parses a lifecycle request: the channel header, the appointment id in the path
// (not a UUID cannot name an appointment, so NOT_FOUND), the JSON body and its required
// expectedVersion, then build for what the operation adds. As for booking, the first problem
// found is the answer: shapes and formats (400 VALIDATION_ERROR) before ranges and lengths (422
// INVALID_VALUE).
func decodeTransition[C any](r *http.Request, build func(body transitionBody, tc TransitionCommand) (C, error)) (any, error) {
	channel, err := channelOf(r)
	if err != nil {
		return nil, err
	}
	raw := r.PathValue("appointmentId")
	id, ok := canonicalUUID(raw)
	if !ok {
		return nil, fmt.Errorf("appointment %s: %w", raw, scheduling.ErrNotFound)
	}
	var body transitionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("%w: body: %w", platform.ErrBadRequest, err)
	}
	version, err := parseInteger("expectedVersion", body.ExpectedVersion, true)
	if err != nil {
		return nil, err
	}

	cmd, err := build(body, TransitionCommand{AppointmentID: id, ExpectedVersion: *version, Reason: body.Reason, Channel: channel})
	if err != nil {
		return nil, err
	}
	if *version < 1 {
		return nil, platform.InvalidValue("expectedVersion", "must be at least 1")
	}
	if utf8.RuneCountInString(body.Reason) > maxReason {
		return nil, platform.InvalidValue("reason", fmt.Sprintf("must be at most %d characters", maxReason))
	}
	return commandRequest[C]{Command: cmd, Channel: channel}, nil
}

// Cancel and no-show carry nothing beyond TransitionRequest.
func decodeCancel(_ context.Context, r *http.Request) (any, error) { return decodePlain(r) }

func decodeNoShow(_ context.Context, r *http.Request) (any, error) { return decodePlain(r) }

func decodePlain(r *http.Request) (any, error) {
	return decodeTransition(r, func(_ transitionBody, tc TransitionCommand) (TransitionCommand, error) { return tc, nil })
}

func decodeCheckIn(_ context.Context, r *http.Request) (any, error) {
	return decodeTransition(r, func(b transitionBody, tc TransitionCommand) (CheckInCommand, error) {
		mileage, err := parseInteger("mileage", b.Mileage, false)
		if err != nil {
			return CheckInCommand{}, err
		}
		if mileage != nil && *mileage < 0 {
			return CheckInCommand{}, platform.InvalidValue("mileage", "must not be negative")
		}
		if utf8.RuneCountInString(b.AdditionalConcerns) > maxConcerns {
			return CheckInCommand{}, platform.InvalidValue("additionalConcerns", fmt.Sprintf("must be at most %d characters", maxConcerns))
		}
		return CheckInCommand{TransitionCommand: tc, Mileage: mileage, AdditionalConcerns: b.AdditionalConcerns}, nil
	})
}

func decodeStart(_ context.Context, r *http.Request) (any, error) {
	return decodeTransition(r, func(b transitionBody, tc TransitionCommand) (StartCommand, error) {
		at, err := parseOptionalInstant("startedAt", b.StartedAt)
		return StartCommand{TransitionCommand: tc, StartedAt: at}, err
	})
}

func decodeComplete(_ context.Context, r *http.Request) (any, error) {
	return decodeTransition(r, func(b transitionBody, tc TransitionCommand) (CompleteCommand, error) {
		at, err := parseOptionalInstant("completedAt", b.CompletedAt)
		return CompleteCommand{TransitionCommand: tc, CompletedAt: at}, err
	})
}

// decodeReschedule needs at least one of startAt, technicianId and serviceBayId; naming nothing
// changes nothing, so it is 400 naming startAt.
func decodeReschedule(_ context.Context, r *http.Request) (any, error) {
	return decodeTransition(r, func(b transitionBody, tc TransitionCommand) (RescheduleCommand, error) {
		cmd := RescheduleCommand{TransitionCommand: tc}
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

// parseInteger reads a JSON number that must be an integer. Missing (or null) is 400 when
// required and nil otherwise; a string, a fraction or anything else is 400.
func parseInteger(field string, raw json.RawMessage, required bool) (*int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		if required {
			return nil, platform.BadRequest(field, "is required")
		}
		return nil, nil
	}
	n, err := strconv.Atoi(string(raw))
	if err != nil {
		return nil, platform.BadRequest(field, "must be an integer")
	}
	return &n, nil
}

// parseOptionalInstant reads an RFC 3339 date-time with an offset; empty means not sent.
func parseOptionalInstant(field, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := parseInstant(field, value)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// channelOf reads the calling channel from X-Channel, the stand-in for the token's channel claim
// until authentication exists: missing is 400, unknown is 422.
func channelOf(r *http.Request) (scheduling.Channel, error) {
	v := r.Header.Get(channelHeader)
	if v == "" {
		return "", platform.BadRequest(channelHeader, "header is required")
	}
	ch, err := scheduling.ParseChannel(v)
	if err != nil {
		return "", platform.InvalidValue(channelHeader, fmt.Sprintf("unknown channel %q", v))
	}
	return ch, nil
}

// parseUUID requires a UUID in its canonical hyphenated form and returns it lower-cased.
func parseUUID(field, value string) (string, error) {
	if value == "" {
		return "", platform.BadRequest(field, "is required")
	}
	id, ok := canonicalUUID(value)
	if !ok {
		return "", platform.BadRequest(field, "must be a UUID")
	}
	return id, nil
}

func canonicalUUID(s string) (string, bool) {
	u, err := uuid.Parse(s)
	if err != nil || len(s) != len(u.String()) {
		return "", false
	}
	return u.String(), true
}

// parseStart reads the required startAt of a booking.
func parseStart(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, platform.BadRequest("startAt", "is required")
	}
	return parseInstant("startAt", value)
}

// parseInstant reads field as an RFC 3339 date-time, which carries an offset.
func parseInstant(field, value string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, platform.BadRequest(field, "must be an ISO 8601 date-time with offset")
	}
	return t, nil
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

// encodeCreated writes a confirmed booking: 201 with the record and its Location.
func encodeCreated(ctx context.Context, w http.ResponseWriter, response any) error {
	if encodedFailure(ctx, w, response) {
		return nil
	}
	appointment := response.(recordResponse).Appointment
	w.Header().Set("Location", appointmentsPath+appointment.ID)
	return writeJSON(w, http.StatusCreated, appointment)
}

// encodeRecord writes a fetched appointment: 200 with the record.
func encodeRecord(ctx context.Context, w http.ResponseWriter, response any) error {
	if encodedFailure(ctx, w, response) {
		return nil
	}
	return writeJSON(w, http.StatusOK, response.(recordResponse).Appointment)
}

// encodedFailure writes the problem and reports true when the response carries a business
// failure.
func encodedFailure(ctx context.Context, w http.ResponseWriter, response any) bool {
	f, ok := response.(endpoint.Failer)
	if !ok || f.Failed() == nil {
		return false
	}
	platform.EncodeError(ctx, f.Failed(), w)
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}
