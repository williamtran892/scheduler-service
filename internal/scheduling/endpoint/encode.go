package endpoint

import (
	"context"
	"encoding/json"
	"net/http"

	kitendpoint "github.com/go-kit/kit/endpoint"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling/service"
)

const appointmentsPath = "/appointments/"

// recordResponse is the answer of every endpoint: the appointment as the caller's channel sees
// it, or the business failure.
type recordResponse struct {
	Appointment service.AppointmentView
	Err         error
}

// Failed implements endpoint.Failer.
func (r recordResponse) Failed() error { return r.Err }

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
	f, ok := response.(kitendpoint.Failer)
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
