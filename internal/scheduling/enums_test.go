package scheduling

import (
	"errors"
	"testing"
)

// Every constant parses back to itself, Scan reads it from the driver's string or []byte,
// Value writes the string, and anything else is ErrInvalidEnum (notes 5.4).

func TestEnums_EveryConstantRoundTrips(t *testing.T) {
	for _, k := range []AppointmentKind{KindWaiter, KindDropOff, KindLoaner} {
		got, err := ParseAppointmentKind(string(k))
		var scanned AppointmentKind
		scanErr := scanned.Scan([]byte(k))
		v, _ := k.Value()
		if err != nil || got != k || scanErr != nil || scanned != k || v != string(k) {
			t.Errorf("AppointmentKind %s: parse %v %v, scan %v %v, value %v", k, got, err, scanned, scanErr, v)
		}
	}
	for _, s := range []AppointmentStatus{StatusConfirmed, StatusCheckedIn, StatusInProgress, StatusCompleted, StatusCancelled, StatusNoShow} {
		got, err := ParseAppointmentStatus(string(s))
		var scanned AppointmentStatus
		scanErr := scanned.Scan(string(s))
		v, _ := s.Value()
		if err != nil || got != s || scanErr != nil || scanned != s || v != string(s) {
			t.Errorf("AppointmentStatus %s: parse %v %v, scan %v %v, value %v", s, got, err, scanned, scanErr, v)
		}
	}
	for _, c := range []Channel{ChannelCustomerWeb, ChannelCustomerApp, ChannelAdvisorConsole, ChannelCallCentre, ChannelIntegration} {
		got, err := ParseChannel(string(c))
		var scanned Channel
		scanErr := scanned.Scan([]byte(c))
		v, _ := c.Value()
		if err != nil || got != c || scanErr != nil || scanned != c || v != string(c) {
			t.Errorf("Channel %s: parse %v %v, scan %v %v, value %v", c, got, err, scanned, scanErr, v)
		}
	}
	for _, k := range []ResourceKind{ResourceBay, ResourceTechnician} {
		got, err := ParseResourceKind(string(k))
		var scanned ResourceKind
		scanErr := scanned.Scan(string(k))
		v, _ := k.Value()
		if err != nil || got != k || scanErr != nil || scanned != k || v != string(k) {
			t.Errorf("ResourceKind %s: parse %v %v, scan %v %v, value %v", k, got, err, scanned, scanErr, v)
		}
	}
	for _, s := range []CellStatus{CellFree, CellBooked} {
		got, err := ParseCellStatus(string(s))
		var scanned CellStatus
		scanErr := scanned.Scan(string(s))
		v, _ := s.Value()
		if err != nil || got != s || scanErr != nil || scanned != s || v != string(s) {
			t.Errorf("CellStatus %s: parse %v %v, scan %v %v, value %v", s, got, err, scanned, scanErr, v)
		}
	}
}

func TestEnums_UnknownValuesAreRejected(t *testing.T) {
	parsers := map[string]func(string) error{
		"AppointmentKind":   func(s string) error { _, err := ParseAppointmentKind(s); return err },
		"AppointmentStatus": func(s string) error { _, err := ParseAppointmentStatus(s); return err },
		"Channel":           func(s string) error { _, err := ParseChannel(s); return err },
		"ResourceKind":      func(s string) error { _, err := ParseResourceKind(s); return err },
		"CellStatus":        func(s string) error { _, err := ParseCellStatus(s); return err },
	}
	for name, parse := range parsers {
		for _, bad := range []string{"", "PICKUP", "drop_off", "HELD"} {
			if err := parse(bad); !errors.Is(err, ErrInvalidEnum) {
				t.Errorf("%s(%q) error = %v, want ErrInvalidEnum", name, bad, err)
			}
		}
	}
	var k AppointmentKind
	if err := k.Scan("PICKUP"); !errors.Is(err, ErrInvalidEnum) {
		t.Errorf("Scan(PICKUP) = %v, want ErrInvalidEnum", err)
	}
	if err := k.Scan(nil); err == nil {
		t.Error("Scan(nil) succeeded, want an error")
	}
}

func TestChannel_IsCustomer(t *testing.T) {
	want := map[Channel]bool{
		ChannelCustomerWeb: true, ChannelCustomerApp: true,
		ChannelAdvisorConsole: false, ChannelCallCentre: false, ChannelIntegration: false,
	}
	for c, w := range want {
		if got := c.IsCustomer(); got != w {
			t.Errorf("%s.IsCustomer() = %v, want %v", c, got, w)
		}
	}
}

// The state machine of design 4.3, as notes 5.4 tabulate it. Every one of the 36 (from, to)
// pairs is checked, so a transition cannot be added or lost silently.
func TestAppointmentStatus_CanTransitionToFollowsTheStateMachine(t *testing.T) {
	allowed := map[AppointmentStatus][]AppointmentStatus{
		StatusConfirmed:  {StatusConfirmed, StatusCheckedIn, StatusCancelled, StatusNoShow},
		StatusNoShow:     {StatusCheckedIn},
		StatusCheckedIn:  {StatusInProgress, StatusCancelled},
		StatusInProgress: {StatusCompleted},
	}
	all := []AppointmentStatus{StatusConfirmed, StatusCheckedIn, StatusInProgress, StatusCompleted, StatusCancelled, StatusNoShow}
	for _, from := range all {
		for _, to := range all {
			want := false
			for _, s := range allowed[from] {
				if s == to {
					want = true
				}
			}
			if got := from.CanTransitionTo(to); got != want {
				t.Errorf("%s → %s allowed = %v, want %v", from, to, got, want)
			}
		}
	}
}
