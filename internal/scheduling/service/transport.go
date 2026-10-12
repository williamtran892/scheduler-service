package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-kit/kit/endpoint"
	kithttp "github.com/go-kit/kit/transport/http"
	"github.com/google/uuid"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
)

// Text limits of the contract: BookAppointmentRequest.notes and CustomerRequests.notes.
const (
	maxNotes         = 4000
	maxRequestNotes  = 2000
	channelHeader    = "X-Channel"
	appointmentsPath = "/appointments/"
)

// RegisterRoutes puts POST /appointments and GET /appointments/{appointmentId} on mux.
func RegisterRoutes(mux *http.ServeMux, e Endpoints, opts ...kithttp.ServerOption) {
	mux.Handle("POST /appointments", kithttp.NewServer(e.BookEndpoint, decodeBookRequest, encodeCreated, opts...))
	mux.Handle("GET /appointments/{appointmentId}", kithttp.NewServer(e.GetEndpoint, decodeGetRequest, encodeRecord, opts...))
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
		return nil, fmt.Errorf("%w: body: %v", platform.ErrBadRequest, err)
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

// channelOf reads the calling channel from X-Channel, the stand-in for the token's channel claim
// until authentication exists (research R8): missing is 400, unknown is 422.
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

// parseStart reads startAt as an RFC 3339 date-time, which carries an offset.
func parseStart(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, platform.BadRequest("startAt", "is required")
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, platform.BadRequest("startAt", "must be an ISO 8601 date-time with offset")
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
