package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"unicode/utf8"

	kitendpoint "github.com/go-kit/kit/endpoint"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
	"scheduler/internal/scheduling/service"
)

// The six lifecycle operations share this mechanism: one request shape, one endpoint adapter and
// one decoder that each operation completes with what it adds to the transition.

// maxReason is the text limit of the contract's TransitionRequest.reason.
const maxReason = 500

// commandRequest is a decoded lifecycle request: the command, and the calling channel that decides
// the view the response is rendered in.
type commandRequest[C any] struct {
	Command C
	Channel scheduling.Channel
}

// makeCommandEndpoint adapts one lifecycle method: every transition answers with the appointment
// after it, in the calling channel's view.
func makeCommandEndpoint[C any](call func(context.Context, C) (service.Record, error)) kitendpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(commandRequest[C])
		rec, err := call(ctx, req.Command)
		if err != nil {
			return recordResponse{Err: err}, nil
		}
		return recordResponse{Appointment: service.NewAppointmentView(rec, req.Channel)}, nil
	}
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
func decodeTransition[C any](r *http.Request, build func(body transitionBody, tc service.TransitionCommand) (C, error)) (any, error) {
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

	cmd, err := build(body, service.TransitionCommand{AppointmentID: id, ExpectedVersion: *version, Reason: body.Reason, Channel: channel})
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

// decodePlain decodes a transition that carries nothing beyond TransitionRequest: cancel and
// no-show.
func decodePlain(r *http.Request) (any, error) {
	return decodeTransition(r, func(_ transitionBody, tc service.TransitionCommand) (service.TransitionCommand, error) {
		return tc, nil
	})
}
