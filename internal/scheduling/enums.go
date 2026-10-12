package scheduling

import "database/sql/driver"

// Enumerations stored as VARCHAR columns (deviation D3). Each type parses, validates and
// scans; the allowed values are defined here and nowhere else.

// AppointmentKind is appointment.kind. Recorded; not enforced in this release.
type AppointmentKind string

const (
	KindWaiter  AppointmentKind = "WAITER"
	KindDropOff AppointmentKind = "DROP_OFF"
	KindLoaner  AppointmentKind = "LOANER"
)

func ParseAppointmentKind(s string) (AppointmentKind, error) { panic("not implemented") }
func (k *AppointmentKind) Scan(src any) error                { panic("not implemented") }
func (k AppointmentKind) Value() (driver.Value, error)       { panic("not implemented") }

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

func ParseAppointmentStatus(s string) (AppointmentStatus, error) { panic("not implemented") }
func (s *AppointmentStatus) Scan(src any) error                  { panic("not implemented") }
func (s AppointmentStatus) Value() (driver.Value, error)         { panic("not implemented") }

// Channel is the calling channel, recorded on the appointment and its events.
type Channel string

const (
	ChannelCustomerWeb    Channel = "CUSTOMER_WEB"
	ChannelCustomerApp    Channel = "CUSTOMER_APP"
	ChannelAdvisorConsole Channel = "ADVISOR_CONSOLE"
	ChannelCallCentre     Channel = "CALL_CENTRE"
	ChannelIntegration    Channel = "INTEGRATION"
)

func ParseChannel(s string) (Channel, error)   { panic("not implemented") }
func (c *Channel) Scan(src any) error          { panic("not implemented") }
func (c Channel) Value() (driver.Value, error) { panic("not implemented") }

// IsCustomer reports whether the channel is a customer channel (CUSTOMER_WEB, CUSTOMER_APP).
func (c Channel) IsCustomer() bool { panic("not implemented") }

// ResourceKind is time_slot.resource_kind.
type ResourceKind string

const (
	ResourceBay        ResourceKind = "BAY"
	ResourceTechnician ResourceKind = "TECHNICIAN"
)

func ParseResourceKind(s string) (ResourceKind, error) { panic("not implemented") }
func (k *ResourceKind) Scan(src any) error             { panic("not implemented") }
func (k ResourceKind) Value() (driver.Value, error)    { panic("not implemented") }

// CellStatus is time_slot.status.
type CellStatus string

const (
	CellFree   CellStatus = "FREE"
	CellBooked CellStatus = "BOOKED"
)

func ParseCellStatus(s string) (CellStatus, error) { panic("not implemented") }
func (s *CellStatus) Scan(src any) error           { panic("not implemented") }
func (s CellStatus) Value() (driver.Value, error)  { panic("not implemented") }
