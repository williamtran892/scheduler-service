package scheduling

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestErrors_CarryTheirContractCode(t *testing.T) {
	want := map[*Error]string{
		ErrInvalidEnum:           "INVALID_VALUE",
		ErrNotFound:              "NOT_FOUND",
		ErrOffGrid:               "OFF_GRID_START",
		ErrOutsideOpeningHours:   "OUTSIDE_OPENING_HOURS",
		ErrNoQualifiedTechnician: "NO_QUALIFIED_TECHNICIAN",
		ErrNotBookableOnline:     "SERVICE_NOT_BOOKABLE_ONLINE",
		ErrSlotTaken:             "SLOT_TAKEN",
		ErrAvailabilityChanged:   "AVAILABILITY_CHANGED",
		ErrDuplicateBooking:      "DUPLICATE_BOOKING",
	}
	for err, code := range want {
		if got := err.ProblemCode(); got != code {
			t.Errorf("%v: ProblemCode() = %s, want %s", err.Message, got, code)
		}
		if err.Error() == "" {
			t.Errorf("%s: empty Error()", code)
		}
	}
}

func TestErrors_MissingCellsAreOneFamily(t *testing.T) {
	for _, err := range []error{ErrBayNotOpen, ErrTechnicianNotWorking, fmt.Errorf("claim: %w", ErrBayNotOpen)} {
		if !errors.Is(err, ErrCellsMissing) {
			t.Errorf("%v is not ErrCellsMissing", err)
		}
	}
	if errors.Is(ErrSlotTaken, ErrCellsMissing) || errors.Is(ErrBayNotOpen, ErrSlotTaken) {
		t.Error("slot taken and missing cells must stay distinct")
	}
}

func TestSlotTakenError_IsSlotTakenAndAddsNothingToTheBody(t *testing.T) {
	err := error(&SlotTakenError{})
	if !errors.Is(fmt.Errorf("book: %w", err), ErrSlotTaken) {
		t.Error("SlotTakenError is not ErrSlotTaken")
	}
	var coded interface{ ProblemCode() string }
	if !errors.As(err, &coded) || coded.ProblemCode() != "SLOT_TAKEN" {
		t.Errorf("ProblemCode = %v, want SLOT_TAKEN", coded)
	}
	var extended interface{ ProblemExtensions() map[string]any }
	if errors.As(err, &extended) {
		t.Errorf("SlotTakenError has extensions %v, want none", extended.ProblemExtensions())
	}
}

func TestAvailabilityChangedError_IsAvailabilityChangedAndAddsNothingToTheBody(t *testing.T) {
	err := &AvailabilityChangedError{}
	if !errors.Is(err, ErrAvailabilityChanged) || err.ProblemCode() != "AVAILABILITY_CHANGED" {
		t.Errorf("Is ErrAvailabilityChanged = %v, code %s", errors.Is(err, ErrAvailabilityChanged), err.ProblemCode())
	}
	var extended interface{ ProblemExtensions() map[string]any }
	if errors.As(error(err), &extended) {
		t.Errorf("AvailabilityChangedError has extensions %v, want none", extended.ProblemExtensions())
	}
}

func TestDuplicateBookingError_CarriesTheExistingAppointment(t *testing.T) {
	existing := Appointment{
		ID: "8f1c3b7a-5d2e-4c1f-9a6b-2e7d1c4f8a90", Status: StatusConfirmed,
		StartAt: z(t, "08:00"), EndAt: z(t, "08:45"),
	}
	err := &DuplicateBookingError{Existing: existing, ServiceTypeCode: "INTERIM", Location: london}

	if !errors.Is(err, ErrDuplicateBooking) || err.ProblemCode() != "DUPLICATE_BOOKING" {
		t.Errorf("Is ErrDuplicateBooking = %v, code %s", errors.Is(err, ErrDuplicateBooking), err.ProblemCode())
	}
	want := `{"endAt":"2026-10-20T09:45:00+01:00","id":"8f1c3b7a-5d2e-4c1f-9a6b-2e7d1c4f8a90",` +
		`"serviceTypeCode":"INTERIM","startAt":"2026-10-20T09:00:00+01:00","status":"CONFIRMED"}`
	if got := extensionJSON(t, err.ProblemExtensions(), "existing"); got != want {
		t.Errorf("existing = %s, want %s", got, want)
	}
}

// extensionJSON marshals one extension member, so the test checks the wire form, not a type.
func extensionJSON(t *testing.T, ext map[string]any, key string) string {
	t.Helper()
	v, ok := ext[key]
	if !ok {
		t.Fatalf("extensions %v have no %q", ext, key)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %s: %v", key, err)
	}
	return string(b)
}

func TestErrors_LifecycleErrorsCarryTheirContractCode(t *testing.T) {
	want := map[*Error]string{
		ErrStaleVersion:      "VERSION_CONFLICT",
		ErrInvalidTransition: "INVALID_TRANSITION",
		ErrForbidden:         "FORBIDDEN",
	}
	for err, code := range want {
		if err.ProblemCode() != code {
			t.Errorf("%q has code %q, want %s", err.Error(), err.ProblemCode(), code)
		}
	}
}

func TestVersionConflictError_IsAStaleVersionWithBothVersionsInItsDetail(t *testing.T) {
	err := error(&VersionConflictError{Current: 3, Sent: 2})

	if !errors.Is(err, ErrStaleVersion) {
		t.Error("VersionConflictError is not ErrStaleVersion")
	}
	var coded interface{ ProblemCode() string }
	if !errors.As(err, &coded) || coded.ProblemCode() != "VERSION_CONFLICT" {
		t.Errorf("code = %v, want VERSION_CONFLICT", coded)
	}
	if got, want := err.Error(), "Appointment is at version 3; you sent 2."; got != want {
		t.Errorf("Error() = %q, want %q (contract VERSION_CONFLICT detail)", got, want)
	}
	if !errors.Is(fmt.Errorf("start appointment a1: %w", err), ErrStaleVersion) {
		t.Error("a wrapped VersionConflictError is not ErrStaleVersion")
	}
}
