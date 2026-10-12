package scheduling

import "time"

// Error is a scheduling failure with a stable problem code from the contract's ProblemCode
// list. platform.EncodeError maps the code to an HTTP status; this package never knows one.
type Error struct {
	Code    string
	Message string
	Parent  *Error // for errors.Is: ErrBayNotOpen is ErrCellsMissing
}

func (e *Error) Error() string       { panic("not implemented") }
func (e *Error) ProblemCode() string { panic("not implemented") }
func (e *Error) Unwrap() error       { panic("not implemented") }

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

func (e *SlotTakenError) Error() string                     { panic("not implemented") }
func (e *SlotTakenError) Is(target error) bool              { panic("not implemented") }
func (e *SlotTakenError) ProblemCode() string               { panic("not implemented") }
func (e *SlotTakenError) ProblemExtensions() map[string]any { panic("not implemented") }

// AvailabilityChangedError is the final AVAILABILITY_CHANGED answer; it is
// ErrAvailabilityChanged.
type AvailabilityChangedError struct {
	Alternatives []Slot
	Location     *time.Location
}

func (e *AvailabilityChangedError) Error() string                     { panic("not implemented") }
func (e *AvailabilityChangedError) Is(target error) bool              { panic("not implemented") }
func (e *AvailabilityChangedError) ProblemCode() string               { panic("not implemented") }
func (e *AvailabilityChangedError) ProblemExtensions() map[string]any { panic("not implemented") }

// DuplicateBookingError is DUPLICATE_BOOKING carrying the appointment that owns the key; it is
// ErrDuplicateBooking.
type DuplicateBookingError struct {
	Existing        Appointment
	ServiceTypeCode string
	Location        *time.Location
}

func (e *DuplicateBookingError) Error() string                     { panic("not implemented") }
func (e *DuplicateBookingError) Is(target error) bool              { panic("not implemented") }
func (e *DuplicateBookingError) ProblemCode() string               { panic("not implemented") }
func (e *DuplicateBookingError) ProblemExtensions() map[string]any { panic("not implemented") }
