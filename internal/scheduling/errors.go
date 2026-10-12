package scheduling

import (
	"fmt"
	"time"
)

// Error is a scheduling failure with a stable problem code from the contract's ProblemCode
// list. platform.EncodeError maps the code to an HTTP status; this package never knows one.
type Error struct {
	Code    string
	Message string
	Parent  *Error // for errors.Is: ErrBayNotOpen is ErrCellsMissing
}

func (e *Error) Error() string       { return e.Message }
func (e *Error) ProblemCode() string { return e.Code }

// Unwrap returns the parent family, so errors.Is(ErrBayNotOpen, ErrCellsMissing) holds.
func (e *Error) Unwrap() error {
	if e.Parent == nil {
		return nil
	}
	return e.Parent
}

var (
	ErrInvalidEnum           = &Error{Code: "INVALID_VALUE", Message: "invalid enumeration value"}
	ErrNotFound              = &Error{Code: "NOT_FOUND", Message: "not found"}
	ErrOffGrid               = &Error{Code: "OFF_GRID_START", Message: "start is not on the booking grid"}
	ErrOutsideOpeningHours   = &Error{Code: "OUTSIDE_OPENING_HOURS", Message: "outside opening hours"}
	ErrNoQualifiedTechnician = &Error{Code: "NO_QUALIFIED_TECHNICIAN", Message: "no qualified technician"}
	ErrNotBookableOnline     = &Error{Code: "SERVICE_NOT_BOOKABLE_ONLINE", Message: "service type is staff only"}
	ErrSlotTaken             = &Error{Code: "SLOT_TAKEN", Message: "slot taken"}
	ErrCellsMissing          = &Error{Code: "AVAILABILITY_CHANGED", Message: "ledger cells missing"}
	ErrBayNotOpen            = &Error{Code: "AVAILABILITY_CHANGED", Message: "bay cells missing", Parent: ErrCellsMissing}
	ErrTechnicianNotWorking  = &Error{Code: "AVAILABILITY_CHANGED", Message: "technician cells missing", Parent: ErrCellsMissing}
	ErrAvailabilityChanged   = &Error{Code: "AVAILABILITY_CHANGED", Message: "availability changed"}
	ErrDuplicateBooking      = &Error{Code: "DUPLICATE_BOOKING", Message: "booking key already has an appointment"}
	ErrDeadlockVictim        = &Error{Code: "SLOT_TAKEN", Message: "deadlock victim"}
)

// Slot is one alternative start offered with SLOT_TAKEN or AVAILABILITY_CHANGED.
type Slot struct {
	StartAt, EndAt time.Time
}

// SlotTakenError is the final SLOT_TAKEN answer; it is ErrSlotTaken. Location renders the
// alternatives in the dealership's offset.
type SlotTakenError struct {
	Alternatives []Slot
	Location     *time.Location
}

func (e *SlotTakenError) Error() string {
	return "no compatible bay and qualified technician are both free at the requested start"
}
func (e *SlotTakenError) Is(target error) bool { return target == ErrSlotTaken }
func (e *SlotTakenError) ProblemCode() string  { return ErrSlotTaken.Code }
func (e *SlotTakenError) ProblemExtensions() map[string]any {
	return map[string]any{"alternatives": slotsJSON(e.Alternatives, e.Location)}
}

// AvailabilityChangedError is the final AVAILABILITY_CHANGED answer; it is
// ErrAvailabilityChanged.
type AvailabilityChangedError struct {
	Alternatives []Slot
	Location     *time.Location
}

func (e *AvailabilityChangedError) Error() string {
	return "the capacity shown is no longer current; alternatives were recomputed"
}
func (e *AvailabilityChangedError) Is(target error) bool { return target == ErrAvailabilityChanged }
func (e *AvailabilityChangedError) ProblemCode() string  { return ErrAvailabilityChanged.Code }
func (e *AvailabilityChangedError) ProblemExtensions() map[string]any {
	return map[string]any{"alternatives": slotsJSON(e.Alternatives, e.Location)}
}

// DuplicateBookingError is DUPLICATE_BOOKING carrying the appointment that owns the key; it is
// ErrDuplicateBooking.
type DuplicateBookingError struct {
	Existing        Appointment
	ServiceTypeCode string
	Location        *time.Location
}

func (e *DuplicateBookingError) Error() string {
	return fmt.Sprintf("appointment %s (%s) already owns this vehicle, customer, start and kind", e.Existing.ID, e.ServiceTypeCode)
}
func (e *DuplicateBookingError) Is(target error) bool { return target == ErrDuplicateBooking }
func (e *DuplicateBookingError) ProblemCode() string  { return ErrDuplicateBooking.Code }
func (e *DuplicateBookingError) ProblemExtensions() map[string]any {
	return map[string]any{"existing": map[string]string{
		"id":              e.Existing.ID,
		"status":          string(e.Existing.Status),
		"startAt":         formatIn(e.Existing.StartAt, e.Location),
		"endAt":           formatIn(e.Existing.EndAt, e.Location),
		"serviceTypeCode": e.ServiceTypeCode,
	}}
}

// slotsJSON renders alternatives for the problem body; an empty list is [], not null.
func slotsJSON(slots []Slot, loc *time.Location) []map[string]string {
	out := make([]map[string]string, 0, len(slots))
	for _, s := range slots {
		out = append(out, map[string]string{"startAt": formatIn(s.StartAt, loc), "endAt": formatIn(s.EndAt, loc)})
	}
	return out
}

// formatIn renders t as RFC 3339 in loc, or in UTC when there is none.
func formatIn(t time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	return t.In(loc).Format(time.RFC3339)
}
