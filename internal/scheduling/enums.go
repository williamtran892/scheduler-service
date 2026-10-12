package scheduling

import (
	"database/sql/driver"
	"fmt"
)

// Enumerations stored as VARCHAR columns (deviation D3). Each type parses, validates and
// scans; the allowed values are defined here and nowhere else.

// AppointmentKind is appointment.kind. Recorded; not enforced in this release.
type AppointmentKind string

const (
	KindWaiter  AppointmentKind = "WAITER"
	KindDropOff AppointmentKind = "DROP_OFF"
	KindLoaner  AppointmentKind = "LOANER"
)

func ParseAppointmentKind(s string) (AppointmentKind, error) {
	switch k := AppointmentKind(s); k {
	case KindWaiter, KindDropOff, KindLoaner:
		return k, nil
	}
	return "", fmt.Errorf("%w: appointment kind %q", ErrInvalidEnum, s)
}

func (k *AppointmentKind) Scan(src any) error { return scanEnum(src, k, ParseAppointmentKind) }

func (k AppointmentKind) Value() (driver.Value, error) { return string(k), nil }

// AppointmentStatus is appointment.status.
type AppointmentStatus string

const (
	StatusConfirmed  AppointmentStatus = "CONFIRMED"
	StatusCheckedIn  AppointmentStatus = "CHECKED_IN"
	StatusInProgress AppointmentStatus = "IN_PROGRESS"
	StatusCompleted  AppointmentStatus = "COMPLETED"
	StatusCancelled  AppointmentStatus = "CANCELLED"
	StatusNoShow     AppointmentStatus = "NO_SHOW"
)

func ParseAppointmentStatus(s string) (AppointmentStatus, error) {
	switch v := AppointmentStatus(s); v {
	case StatusConfirmed, StatusCheckedIn, StatusInProgress, StatusCompleted, StatusCancelled, StatusNoShow:
		return v, nil
	}
	return "", fmt.Errorf("%w: appointment status %q", ErrInvalidEnum, s)
}

func (s *AppointmentStatus) Scan(src any) error { return scanEnum(src, s, ParseAppointmentStatus) }

func (s AppointmentStatus) Value() (driver.Value, error) { return string(s), nil }

// Channel is the calling channel, recorded on the appointment and its events.
type Channel string

const (
	ChannelCustomerWeb    Channel = "CUSTOMER_WEB"
	ChannelCustomerApp    Channel = "CUSTOMER_APP"
	ChannelAdvisorConsole Channel = "ADVISOR_CONSOLE"
	ChannelCallCentre     Channel = "CALL_CENTRE"
	ChannelIntegration    Channel = "INTEGRATION"
)

func ParseChannel(s string) (Channel, error) {
	switch c := Channel(s); c {
	case ChannelCustomerWeb, ChannelCustomerApp, ChannelAdvisorConsole, ChannelCallCentre, ChannelIntegration:
		return c, nil
	}
	return "", fmt.Errorf("%w: channel %q", ErrInvalidEnum, s)
}

func (c *Channel) Scan(src any) error { return scanEnum(src, c, ParseChannel) }

func (c Channel) Value() (driver.Value, error) { return string(c), nil }

// IsCustomer reports whether the channel is a customer channel (CUSTOMER_WEB, CUSTOMER_APP).
func (c Channel) IsCustomer() bool { return c == ChannelCustomerWeb || c == ChannelCustomerApp }

// ResourceKind is time_slot.resource_kind.
type ResourceKind string

const (
	ResourceBay        ResourceKind = "BAY"
	ResourceTechnician ResourceKind = "TECHNICIAN"
)

func ParseResourceKind(s string) (ResourceKind, error) {
	switch k := ResourceKind(s); k {
	case ResourceBay, ResourceTechnician:
		return k, nil
	}
	return "", fmt.Errorf("%w: resource kind %q", ErrInvalidEnum, s)
}

func (k *ResourceKind) Scan(src any) error { return scanEnum(src, k, ParseResourceKind) }

func (k ResourceKind) Value() (driver.Value, error) { return string(k), nil }

// CellStatus is time_slot.status.
type CellStatus string

const (
	CellFree   CellStatus = "FREE"
	CellBooked CellStatus = "BOOKED"
)

func ParseCellStatus(s string) (CellStatus, error) {
	switch v := CellStatus(s); v {
	case CellFree, CellBooked:
		return v, nil
	}
	return "", fmt.Errorf("%w: cell status %q", ErrInvalidEnum, s)
}

func (s *CellStatus) Scan(src any) error { return scanEnum(src, s, ParseCellStatus) }

func (s CellStatus) Value() (driver.Value, error) { return string(s), nil }

// scanEnum reads a database value into *dst, rejecting what parse rejects, so a bad row fails
// loudly at read time (notes 5.4). Text arrives from the driver as []byte or string.
func scanEnum[T any](src any, dst *T, parse func(string) (T, error)) error {
	var s string
	switch v := src.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("scan enumeration: cannot read a %T", src)
	}
	v, err := parse(s)
	if err != nil {
		return err
	}
	*dst = v
	return nil
}
