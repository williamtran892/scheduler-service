package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
)

// The booking fixture the service tests share. It mirrors local/mysql/data.sql in miniature:
// MAN01 in Europe/London on a 15-minute grid, three bays, three technicians, the catalogue, and
// a ledger with every resource free 07:00Z to 17:00Z on every day. The clock reads
// 2026-10-19T08:00Z; bookings default to 2026-10-20 09:00 London (08:00Z).

var (
	ctx    = context.Background()
	london = mustLocation("Europe/London")
	now    = time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC)
)

const (
	tenant      = "tenant-1"
	otherTenant = "tenant-2"
	apptID      = "a1b2c3d4-0000-4000-8000-000000000001"
)

var (
	man01 = scheduling.DealershipConfig{DealershipID: "man01", TenantID: tenant, Code: "MAN01", Name: "Manchester Central",
		Location: london, Grid: scheduling.Grid{SlotMinutes: 15}, BookingHorizonDays: 28}
	lds01 = scheduling.DealershipConfig{DealershipID: "lds01", TenantID: tenant, Code: "LDS01", Name: "Leeds North",
		Location: london, Grid: scheduling.Grid{SlotMinutes: 15}, BookingHorizonDays: 28}

	bay1  = scheduling.ServiceBay{ID: "bay-1", Name: "Bay 1", Type: "GENERAL"}
	bay2  = scheduling.ServiceBay{ID: "bay-2", Name: "Bay 2", Type: "MOT"}
	bay3  = scheduling.ServiceBay{ID: "bay-3", Name: "Bay 3", Type: "EV_HV"}
	sam   = scheduling.Technician{ID: "sam", Name: "Sam", Skills: []string{"GENERAL_SERVICE", "EV_HV_L3"}}
	priya = scheduling.Technician{ID: "priya", Name: "Priya", Skills: []string{"GENERAL_SERVICE", "MOT_TESTER"}}
	lee   = scheduling.Technician{ID: "lee", Name: "Lee", Skills: []string{"GENERAL_SERVICE"}}

	interim    = requirement("interim", "INTERIM", 45, 0, 0, []string{"GENERAL_SERVICE"}, []string{"GENERAL", "MOT"}, true)
	major      = requirement("major", "MAJOR", 90, 15, 15, []string{"GENERAL_SERVICE"}, []string{"GENERAL"}, true)
	mot        = requirement("mot", "MOT", 45, 0, 0, []string{"MOT_TESTER"}, []string{"MOT"}, true)
	evMajor    = requirement("ev-major", "EV_MAJOR", 120, 15, 15, []string{"EV_HV_L3"}, []string{"EV_HV"}, true)
	diagnostic = requirement("diagnostic", "DIAGNOSTIC", 50, 0, 0, []string{"GENERAL_SERVICE"}, []string{"GENERAL"}, false)
	aircon     = requirement("aircon", "AIRCON", 30, 0, 0, []string{"AIRCON_FGAS"}, []string{"GENERAL"}, true)

	alice    = scheduling.Customer{ID: "alice", TenantID: tenant, DisplayName: "Alice"}
	bob      = scheduling.Customer{ID: "bob", TenantID: tenant, DisplayName: "Bob"}
	aliceCar = scheduling.Vehicle{ID: "yk21", TenantID: tenant, CustomerID: "alice", Registration: "YK21 ABC", Make: "Volkswagen", Model: "ID.4"}
	bobCar   = scheduling.Vehicle{ID: "bd70", TenantID: tenant, CustomerID: "bob", Registration: "BD70 XYZ", Make: "Ford", Model: "Focus"}
)

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func requirement(id, code string, minutes, before, after int, skills, bayTypes []string, online bool) scheduling.ServiceRequirement {
	return scheduling.ServiceRequirement{ServiceTypeID: id, TenantID: tenant, Code: code, Name: code,
		Duration: time.Duration(minutes) * time.Minute, BufferBefore: time.Duration(before) * time.Minute,
		BufferAfter: time.Duration(after) * time.Minute, RequiredSkills: skills, AllowedBayTypes: bayTypes,
		OnlineBookable: online, Active: true}
}

// on returns an instant on 2026-10-20 from a UTC "HH:MM" (09:00 London is "08:00").
func on(hhmm string) time.Time {
	return onDay(20, hhmm)
}

func onDay(day int, hhmm string) time.Time {
	c, err := time.Parse("15:04", hhmm)
	if err != nil {
		panic(fmt.Sprintf("bad time %q", hhmm))
	}
	return time.Date(2026, 10, day, c.Hour(), c.Minute(), 0, 0, time.UTC)
}

func cellRun(first time.Time, cells int) scheduling.CellRun {
	return scheduling.CellRun{First: first, Cells: cells, Slot: 15 * time.Minute}
}

// booking is Alice's default request: INTERIM for YK21 ABC at MAN01, 2026-10-20 09:00 London,
// drop-off, through the advisor console.
func booking() BookCommand {
	return BookCommand{DealershipID: "man01", ServiceTypeID: "interim", CustomerID: "alice", VehicleID: "yk21",
		StartAt: on("08:00"), Kind: scheduling.KindDropOff, Channel: scheduling.ChannelAdvisorConsole}
}

// fixedID is a platform.IDs that always returns the same id.
type fixedID string

func (f fixedID) New() string { return string(f) }

// fixture holds the mocks and the data behind them. Tests change the data, then call
// service(), which registers the read expectations (any number of calls, answered from the
// data) and builds the Service. Transactions are not registered here: a test that expects one
// calls expectTx, so an unexpected transaction fails the test.
type fixture struct {
	t         *testing.T
	store     *MockStore
	tx        *MockTx
	workshop  *MockWorkshopReader
	customers *MockCustomerReader
	clock     *platform.FakeClock

	dealerships  map[string]scheduling.DealershipConfig
	requirements map[string]scheduling.ServiceRequirement
	people       map[string]scheduling.Customer
	vehicles     map[string]scheduling.Vehicle
	bays         []scheduling.ServiceBay
	techs        []scheduling.Technician

	// islands answers DayLedger; call counts the calls for that local date, from 1.
	islands     func(day scheduling.DayWindow, call int) []scheduling.Island
	loads       scheduling.Loads
	ledgerCalls map[string]int

	// existing owns the booking key from the existingFrom-th lookup on (1-based); 0 never.
	existing     scheduling.Appointment
	existingFrom int
	lookups      int

	stored map[string]scheduling.Appointment
	owned  map[string]scheduling.OwnedCells
	events map[string][]scheduling.Event

	resourcesAsked []string // localDate of each Resources call
}

func newFixture(t *testing.T) *fixture {
	ctrl := gomock.NewController(t)
	f := &fixture{
		t: t, store: NewMockStore(ctrl), tx: NewMockTx(ctrl),
		workshop: NewMockWorkshopReader(ctrl), customers: NewMockCustomerReader(ctrl),
		clock:       platform.NewFakeClock(now),
		dealerships: map[string]scheduling.DealershipConfig{"man01": man01, "lds01": lds01},
		requirements: map[string]scheduling.ServiceRequirement{"interim": interim, "major": major, "mot": mot,
			"ev-major": evMajor, "diagnostic": diagnostic, "aircon": aircon},
		people:      map[string]scheduling.Customer{"alice": alice, "bob": bob},
		vehicles:    map[string]scheduling.Vehicle{"yk21": aliceCar, "bd70": bobCar},
		bays:        []scheduling.ServiceBay{bay1, bay2, bay3},
		techs:       []scheduling.Technician{sam, priya, lee},
		loads:       scheduling.Loads{},
		ledgerCalls: map[string]int{},
		stored:      map[string]scheduling.Appointment{},
		owned:       map[string]scheduling.OwnedCells{},
		events:      map[string][]scheduling.Event{},
	}
	f.islands = func(day scheduling.DayWindow, _ int) []scheduling.Island { return f.wholeDay(day) }
	return f
}

// wholeDay is every bay and technician free 07:00Z to 17:00Z on the day's UTC date(s).
func (f *fixture) wholeDay(day scheduling.DayWindow) []scheduling.Island {
	var is []scheduling.Island
	for d := day.From.Truncate(24 * time.Hour); d.Before(day.To); d = d.Add(24 * time.Hour) {
		open, closing := d.Add(7*time.Hour), d.Add(17*time.Hour)
		if open.Before(day.From) || closing.After(day.To) {
			continue
		}
		for _, b := range f.bays {
			is = append(is, scheduling.Island{Kind: scheduling.ResourceBay, ResourceID: b.ID, From: open, To: closing})
		}
		for _, t := range f.techs {
			is = append(is, scheduling.Island{Kind: scheduling.ResourceTechnician, ResourceID: t.ID, From: open, To: closing})
		}
	}
	return is
}

// workingCells counts the grid cells in [from, to) that fall 07:00Z to 17:00Z, the fixture's
// working time.
func workingCells(from, to time.Time) int {
	n := 0
	for c := from; c.Before(to); c = c.Add(15 * time.Minute) {
		if h := c.UTC().Hour(); h >= 7 && h < 17 {
			n++
		}
	}
	return n
}

func notFound(what, id string) error {
	return fmt.Errorf("%s %s: %w", what, id, scheduling.ErrNotFound)
}

// service registers the read expectations and builds the Service under test.
func (f *fixture) service() Service {
	f.workshop.EXPECT().DealershipConfig(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, id string) (scheduling.DealershipConfig, error) {
			if d, ok := f.dealerships[id]; ok {
				return d, nil
			}
			return scheduling.DealershipConfig{}, notFound("dealership", id)
		})
	f.workshop.EXPECT().Requirement(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, id string) (scheduling.ServiceRequirement, error) {
			if r, ok := f.requirements[id]; ok {
				return r, nil
			}
			return scheduling.ServiceRequirement{}, notFound("service type", id)
		})
	f.workshop.EXPECT().Resources(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, _ string, localDate string) ([]scheduling.ServiceBay, []scheduling.Technician, error) {
			f.resourcesAsked = append(f.resourcesAsked, localDate)
			return f.bays, f.techs, nil
		})
	f.workshop.EXPECT().Bay(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, id string) (scheduling.ServiceBay, error) {
			for _, b := range []scheduling.ServiceBay{bay1, bay2, bay3} {
				if b.ID == id {
					return b, nil
				}
			}
			return scheduling.ServiceBay{}, notFound("bay", id)
		})
	f.workshop.EXPECT().Technician(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, id string) (scheduling.Technician, error) {
			for _, t := range []scheduling.Technician{sam, priya, lee} {
				if t.ID == id {
					return t, nil
				}
			}
			return scheduling.Technician{}, notFound("technician", id)
		})
	f.customers.EXPECT().Customer(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, id string) (scheduling.Customer, error) {
			if c, ok := f.people[id]; ok {
				return c, nil
			}
			return scheduling.Customer{}, notFound("customer", id)
		})
	f.customers.EXPECT().Vehicle(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, id string) (scheduling.Vehicle, error) {
			if v, ok := f.vehicles[id]; ok {
				return v, nil
			}
			return scheduling.Vehicle{}, notFound("vehicle", id)
		})

	f.store.EXPECT().DayLedger(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, _ string, day scheduling.DayWindow) ([]scheduling.Island, scheduling.Loads, error) {
			f.ledgerCalls[day.LocalDate]++
			return f.islands(day, f.ledgerCalls[day.LocalDate]), f.loads, nil
		})
	f.store.EXPECT().WorkingCells(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, _ string, from, to time.Time) (int, error) {
			return workingCells(from, to), nil
		})
	f.store.EXPECT().AppointmentByBookingKey(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, key scheduling.BookingKey) (scheduling.Appointment, bool, error) {
			f.lookups++
			if f.existingFrom > 0 && f.lookups >= f.existingFrom && sameKey(key, f.existing.Key()) {
				return f.existing, true, nil
			}
			return scheduling.Appointment{}, false, nil
		})
	f.store.EXPECT().Appointment(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, id string) (scheduling.Appointment, error) {
			if a, ok := f.stored[id]; ok {
				return a, nil
			}
			if f.existingFrom > 0 && f.existing.ID == id {
				return f.existing, nil
			}
			return scheduling.Appointment{}, notFound("appointment", id)
		})
	f.store.EXPECT().OwnedCells(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, id string) (scheduling.OwnedCells, error) { return f.owned[id], nil })

	return NewService(f.store, f.workshop, f.customers, f.clock, fixedID(apptID))
}

// expectTx expects n transactions, each running its function against the mock Tx.
func (f *fixture) expectTx(n int) {
	f.store.EXPECT().InTx(gomock.Any(), gomock.Any()).Times(n).DoAndReturn(
		func(_ context.Context, fn func(scheduling.Tx) error) error { return fn(f.tx) })
}

// attempt expects one booking attempt on a pair, in order: insert, claim; and, when the claim
// succeeds, the CONFIRMED event and the outbox row.
func (f *fixture) attempt(bayID, techID string, bayRun, techRun scheduling.CellRun, claimErr error) []any {
	calls := []any{
		f.tx.EXPECT().InsertAppointment(gomock.Any()).Return(nil),
		f.tx.EXPECT().ClaimPair("man01", bayID, runEq{bayRun}, techID, runEq{techRun}, apptID).Return(claimErr),
	}
	if claimErr == nil {
		calls = append(calls,
			f.tx.EXPECT().AppendEvent(apptID, "CONFIRMED", "anonymous", gomock.Any(), gomock.Any()).Return(nil),
			f.tx.EXPECT().AppendOutbox("Appointment", apptID, "AppointmentConfirmed", gomock.Any()).Return(nil),
		)
	}
	return calls
}

// runEq matches a CellRun by instant, count and slot, whatever the time's location.
type runEq struct{ want scheduling.CellRun }

func (m runEq) Matches(x any) bool {
	r, ok := x.(scheduling.CellRun)
	return ok && r.First.Equal(m.want.First) && r.Cells == m.want.Cells && r.Slot == m.want.Slot
}

func (m runEq) String() string {
	return fmt.Sprintf("run of %d cells from %s", m.want.Cells, m.want.First)
}

// stored makes an appointment that exists already, for replay and Get tests.
func storedAppointment(serviceTypeID string, status scheduling.AppointmentStatus) scheduling.Appointment {
	return scheduling.Appointment{
		ID: "existing-1", TenantID: tenant, DealershipID: "man01", CustomerID: "alice", VehicleID: "yk21",
		ServiceTypeID: serviceTypeID, Kind: scheduling.KindDropOff, StartAt: on("08:00"), EndAt: on("08:45"),
		ServiceBayID: "bay-1", TechnicianID: "lee", Status: status,
		CustomerRequests: scheduling.CustomerRequests{Notes: "Rattle from rear left"},
		Channel:          scheduling.ChannelAdvisorConsole, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
}

func sameKey(a, b scheduling.BookingKey) bool {
	return a.VehicleID == b.VehicleID && a.CustomerID == b.CustomerID && a.Kind == b.Kind && a.StartAt.Equal(b.StartAt)
}
