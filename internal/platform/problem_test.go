package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// encode runs EncodeError and decodes what it wrote.
func encode(t *testing.T, err error) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	EncodeError(context.Background(), err, rec)
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return rec, body
}

func TestEncodeError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantType   string
		wantDetail bool
	}{
		{"decoder failure is a 400 VALIDATION_ERROR", fmt.Errorf("%w: body: unexpected EOF", ErrBadRequest),
			http.StatusBadRequest, "VALIDATION_ERROR", ProblemTypeBase + "VALIDATION_ERROR", true},
		{"unknown error is a 500 without detail", errors.New("connection reset"),
			http.StatusInternalServerError, "INTERNAL", "about:blank", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, body := encode(t, tt.err)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if body["code"] != tt.wantCode || body["type"] != tt.wantType || body["status"] != float64(tt.wantStatus) {
				t.Errorf("problem = %v, want code %s type %s status %d", body, tt.wantCode, tt.wantType, tt.wantStatus)
			}
			if _, has := body["detail"]; has != tt.wantDetail {
				t.Errorf("detail present = %v, want %v", has, tt.wantDetail)
			}
		})
	}
}

// coded is a domain-style error: a problem code and optional extension members.
type coded struct {
	code string
	ext  map[string]any
}

func (e coded) Error() string                     { return "coded: " + e.code }
func (e coded) ProblemCode() string               { return e.code }
func (e coded) ProblemExtensions() map[string]any { return e.ext }

func TestEncodeError_CodedErrorGetsItsStatusTypeAndExtensions(t *testing.T) {
	alternatives := []any{map[string]any{"startAt": "2026-10-20T09:15:00+01:00", "endAt": "2026-10-20T10:00:00+01:00"}}
	err := fmt.Errorf("book: %w", coded{code: "SLOT_TAKEN", ext: map[string]any{"alternatives": alternatives}})

	rec, body := encode(t, err)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
	if body["code"] != "SLOT_TAKEN" || body["type"] != ProblemTypeBase+"SLOT_TAKEN" || body["title"] == "" {
		t.Errorf("problem = %v", body)
	}
	got, _ := body["alternatives"].([]any)
	if len(got) != 1 {
		t.Errorf("alternatives = %v, want the one given", body["alternatives"])
	}
}

func TestEncodeError_DuplicateBookingCarriesTheExistingAppointment(t *testing.T) {
	existing := map[string]any{"id": "8f1c3b7a-5d2e-4c1f-9a6b-2e7d1c4f8a90", "status": "CONFIRMED",
		"startAt": "2026-10-20T09:00:00+01:00", "endAt": "2026-10-20T09:45:00+01:00", "serviceTypeCode": "INTERIM"}

	rec, body := encode(t, coded{code: "DUPLICATE_BOOKING", ext: map[string]any{"existing": existing}})

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
	got, _ := body["existing"].(map[string]any)
	if got["id"] != existing["id"] || got["serviceTypeCode"] != "INTERIM" {
		t.Errorf("existing = %v, want %v", body["existing"], existing)
	}
}

func TestEncodeError_FieldErrorsNameTheField(t *testing.T) {
	tests := []struct {
		name       string
		err        func() error
		wantStatus int
		wantCode   string
		wantField  string
	}{
		{"malformed field", func() error { return BadRequest("startAt", "must be an ISO 8601 date-time with offset") },
			http.StatusBadRequest, "VALIDATION_ERROR", "startAt"},
		{"out-of-range value", func() error { return InvalidValue("kind", `unknown kind "PICKUP"`) },
			http.StatusUnprocessableEntity, "INVALID_VALUE", "kind"},
		{"wrapped field error", func() error { return fmt.Errorf("decode: %w", InvalidValue("X-Channel", "unknown channel")) },
			http.StatusUnprocessableEntity, "INVALID_VALUE", "X-Channel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, body := encode(t, tt.err())
			if rec.Code != tt.wantStatus || body["code"] != tt.wantCode {
				t.Errorf("got %d %v, want %d %s", rec.Code, body["code"], tt.wantStatus, tt.wantCode)
			}
			errs, _ := body["errors"].([]any)
			if len(errs) != 1 || errs[0].(map[string]any)["field"] != tt.wantField || errs[0].(map[string]any)["message"] == "" {
				t.Errorf("errors = %v, want one entry naming %s with a message", body["errors"], tt.wantField)
			}
		})
	}
}

func TestBadRequest_IsErrBadRequest(t *testing.T) {
	if !errors.Is(BadRequest("startAt", "missing"), ErrBadRequest) {
		t.Error("BadRequest does not match ErrBadRequest")
	}
	if errors.Is(InvalidValue("kind", "unknown"), ErrBadRequest) {
		t.Error("InvalidValue matches ErrBadRequest; it is a 422, not a 400")
	}
}

// TestStatusFor_EveryContractCode covers the ProblemCode list of api/openapi.yaml.
func TestStatusFor_EveryContractCode(t *testing.T) {
	want := map[string]int{
		"SLOT_TAKEN":                  http.StatusConflict,
		"AVAILABILITY_CHANGED":        http.StatusConflict,
		"VERSION_CONFLICT":            http.StatusConflict,
		"INVALID_TRANSITION":          http.StatusConflict,
		"DUPLICATE":                   http.StatusConflict,
		"DUPLICATE_BOOKING":           http.StatusConflict,
		"NO_QUALIFIED_TECHNICIAN":     http.StatusUnprocessableEntity,
		"OUTSIDE_OPENING_HOURS":       http.StatusUnprocessableEntity,
		"OFF_GRID_START":              http.StatusUnprocessableEntity,
		"SERVICE_NOT_BOOKABLE_ONLINE": http.StatusUnprocessableEntity,
		"INVALID_VALUE":               http.StatusUnprocessableEntity,
		"VALIDATION_ERROR":            http.StatusBadRequest,
		"UNAUTHORIZED":                http.StatusUnauthorized,
		"FORBIDDEN":                   http.StatusForbidden,
		"NOT_FOUND":                   http.StatusNotFound,
		"NO_SUCH_CODE":                http.StatusInternalServerError,
	}
	for code, status := range want {
		if got := StatusFor(code); got != status {
			t.Errorf("StatusFor(%s) = %d, want %d", code, got, status)
		}
	}
}
