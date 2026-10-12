package service

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
)

// One test per branch of the booking flow (research R4) and per guarantee only a unit test can
// show (research R16). Locking, isolation and unique indexes are the acceptance suite's job.

// distinctLoads makes the INTERIM pairs at 08:00Z try in one fixed order:
// bay-1/sam (0), bay-1/priya (1), bay-1/lee (2), bay-2/sam (5), bay-2/priya (6), bay-2/lee (7).
var distinctLoads = scheduling.Loads{"bay-2": 5, "priya": 1, "lee": 2}

func requireField(t *testing.T, err error, code, field string) {
	t.Helper()
	var fe *platform.FieldError
	if !errors.As(err, &fe) || fe.Code != code || fe.Field != field {
		t.Fatalf("error = %v, want %s naming %s", err, code, field)
	}
}

func TestBook_RejectsReferencesThatDoNotResolve(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *fixture, cmd *BookCommand)
		field string
	}{
		{"unknown dealership", func(_ *fixture, c *BookCommand) { c.DealershipID = "nowhere" }, "dealershipId"},
		{"unknown service type", func(_ *fixture, c *BookCommand) { c.ServiceTypeID = "nothing" }, "serviceTypeId"},
		{"inactive service type", func(f *fixture, _ *BookCommand) {
			r := f.requirements["interim"]
			r.Active = false
			f.requirements["interim"] = r
		}, "serviceTypeId"},
		{"service type of another tenant", func(f *fixture, c *BookCommand) {
			r := interim
			r.ServiceTypeID, r.TenantID = "foreign", otherTenant
			f.requirements["foreign"] = r
			c.ServiceTypeID = "foreign"
		}, "serviceTypeId"},
		{"unknown customer", func(_ *fixture, c *BookCommand) { c.CustomerID = "nobody" }, "customerId"},
		{"customer of another tenant", func(f *fixture, c *BookCommand) {
			f.people["carol"] = scheduling.Customer{ID: "carol", TenantID: otherTenant}
			f.vehicles["carols"] = scheduling.Vehicle{ID: "carols", TenantID: otherTenant, CustomerID: "carol"}
			c.CustomerID, c.VehicleID = "carol", "carols"
		}, "customerId"},
		{"unknown vehicle", func(_ *fixture, c *BookCommand) { c.VehicleID = "nothing" }, "vehicleId"},
		{"vehicle of another customer", func(_ *fixture, c *BookCommand) { c.VehicleID = "bd70" }, "vehicleId"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			cmd := booking()
			tt.setup(f, &cmd)

			_, err := f.service().Book(ctx, cmd)

			requireField(t, err, "INVALID_VALUE", tt.field)
		})
	}
}

func TestBook_ReplayOfTheSameRequestReturnsTheExistingAppointment(t *testing.T) {
	f := newFixture(t)
	f.existing, f.existingFrom = storedAppointment("interim", scheduling.StatusConfirmed), 1
	owned := scheduling.OwnedCells{
		Bay:        &scheduling.CellRange{From: on("08:00"), To: on("08:45"), Cells: 3},
		Technician: &scheduling.CellRange{From: on("08:00"), To: on("08:45"), Cells: 3},
	}
	f.owned["existing-1"] = owned
	// The ledger is full: a replay must not depend on free capacity.
	f.islands = func(scheduling.DayWindow, int) []scheduling.Island { return nil }
	cmd := booking()
	cmd.CustomerRequests = scheduling.CustomerRequests{Notes: "Also check the wipers"}

	rec, err := f.service().Book(ctx, cmd)

	if err != nil {
		t.Fatalf("Book: %v", err)
	}
	if rec.Appointment.ID != "existing-1" || rec.Appointment.CustomerRequests.Notes != "Rattle from rear left" {
		t.Errorf("appointment = %+v, want the existing one unchanged", rec.Appointment)
	}
	if rec.Bay != bay1 || rec.Technician.ID != "lee" || rec.Cells != owned || rec.Dealership.Code != "MAN01" ||
		rec.ServiceType.Code != "INTERIM" || rec.Customer != alice || rec.Vehicle != aliceCar {
		t.Errorf("record = %+v, want the existing appointment's names and cells", rec)
	}
}

func TestBook_SameKeyWithADifferentRequestIsADuplicateBooking(t *testing.T) {
	tests := []struct {
		name     string
		existing scheduling.Appointment
		cmd      func() BookCommand
		wantCode string
	}{
		{"different service type", storedAppointment("interim", scheduling.StatusConfirmed),
			func() BookCommand { c := booking(); c.ServiceTypeID = "major"; return c }, "INTERIM"},
		{"different dealership", storedAppointment("interim", scheduling.StatusConfirmed),
			func() BookCommand { c := booking(); c.DealershipID = "lds01"; return c }, "INTERIM"},
		{"existing is cancelled", storedAppointment("interim", scheduling.StatusCancelled), booking, "INTERIM"},
		{"existing is a no-show", storedAppointment("interim", scheduling.StatusNoShow), booking, "INTERIM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.existing, f.existingFrom = tt.existing, 1

			_, err := f.service().Book(ctx, tt.cmd())

			var dup *scheduling.DuplicateBookingError
			if !errors.As(err, &dup) {
				t.Fatalf("error = %v, want DuplicateBookingError", err)
			}
			if dup.Existing.ID != "existing-1" || dup.ServiceTypeCode != tt.wantCode || dup.Location != london {
				t.Errorf("duplicate = %+v, want existing-1 with %s in Europe/London", dup, tt.wantCode)
			}
		})
	}
}

func TestBook_StaffOnlyServiceIsRefusedOnCustomerChannels(t *testing.T) {
	for _, ch := range []scheduling.Channel{scheduling.ChannelCustomerWeb, scheduling.ChannelCustomerApp} {
		t.Run(string(ch), func(t *testing.T) {
			f := newFixture(t)
			cmd := booking()
			cmd.ServiceTypeID, cmd.Channel = "diagnostic", ch

			_, err := f.service().Book(ctx, cmd)

			if !errors.Is(err, scheduling.ErrNotBookableOnline) {
				t.Errorf("error = %v, want ErrNotBookableOnline", err)
			}
		})
	}
}

func TestBook_StartOffTheGridIsRefused(t *testing.T) {
	f := newFixture(t)
	cmd := booking()
	cmd.StartAt = on("08:05")

	_, err := f.service().Book(ctx, cmd)

	if !errors.Is(err, scheduling.ErrOffGrid) {
		t.Errorf("error = %v, want ErrOffGrid", err)
	}
}

func TestBook_StartMustBeAfterNowAndWithinTheHorizon(t *testing.T) {
	horizon := now.Add(28 * 24 * time.Hour) // 2026-11-16T08:00Z
	for _, tt := range []struct {
		name  string
		start time.Time
	}{
		{"start is now", now},
		{"start is in the past", now.Add(-15 * time.Minute)},
		{"start is one cell beyond the horizon", horizon.Add(15 * time.Minute)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			cmd := booking()
			cmd.StartAt = tt.start

			_, err := f.service().Book(ctx, cmd)

			requireField(t, err, "INVALID_VALUE", "startAt")
		})
	}

	t.Run("start exactly at the horizon is accepted", func(t *testing.T) {
		f := newFixture(t)
		f.loads = distinctLoads
		f.expectTx(1)
		gomock.InOrder(f.attempt("bay-1", "sam", cellRun(horizon, 3), cellRun(horizon, 3), nil)...)
		cmd := booking()
		cmd.StartAt = horizon

		if _, err := f.service().Book(ctx, cmd); err != nil {
			t.Errorf("Book at the horizon: %v", err)
		}
	})
}

func TestBook_RunsMustLieInsideWorkingTime(t *testing.T) {
	for _, tt := range []struct {
		name, service, start string
	}{
		{"before the first cell", "interim", "06:45"},
		{"job runs past the last cell", "interim", "16:30"},
		{"before-buffer falls before the first cell", "major", "07:00"},
		{"after-buffer runs past the last cell", "major", "15:30"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			cmd := booking()
			cmd.ServiceTypeID, cmd.StartAt = tt.service, on(tt.start)

			_, err := f.service().Book(ctx, cmd)

			if !errors.Is(err, scheduling.ErrOutsideOpeningHours) {
				t.Errorf("error = %v, want ErrOutsideOpeningHours", err)
			}
		})
	}

	t.Run("a job ending on the last cell is inside", func(t *testing.T) {
		f := newFixture(t)
		f.loads = distinctLoads
		f.expectTx(1)
		gomock.InOrder(f.attempt("bay-1", "sam", cellRun(on("16:15"), 3), cellRun(on("16:15"), 3), nil)...)
		cmd := booking()
		cmd.StartAt = on("16:15")

		if _, err := f.service().Book(ctx, cmd); err != nil {
			t.Errorf("Book 16:15-17:00: %v", err)
		}
	})
}

func TestBook_NoQualifiedTechnician(t *testing.T) {
	t.Run("nobody holds the skill", func(t *testing.T) {
		f := newFixture(t)
		cmd := booking()
		cmd.ServiceTypeID = "aircon"

		_, err := f.service().Book(ctx, cmd)

		if !errors.Is(err, scheduling.ErrNoQualifiedTechnician) {
			t.Errorf("error = %v, want ErrNoQualifiedTechnician", err)
		}
	})
	t.Run("the only holder is not active on the day", func(t *testing.T) {
		f := newFixture(t)
		f.techs = []scheduling.Technician{priya, lee}
		cmd := booking()
		cmd.ServiceTypeID = "ev-major"

		_, err := f.service().Book(ctx, cmd)

		if !errors.Is(err, scheduling.ErrNoQualifiedTechnician) {
			t.Errorf("error = %v, want ErrNoQualifiedTechnician", err)
		}
	})
}

// Priya, the only MOT tester, is busy 08:00Z to 08:45Z on 2026-10-20.
func priyaBusyAt8(f *fixture) func(scheduling.DayWindow, int) []scheduling.Island {
	return func(day scheduling.DayWindow, _ int) []scheduling.Island {
		var out []scheduling.Island
		for _, i := range f.wholeDay(day) {
			if i.ResourceID == "priya" && day.LocalDate == "2026-10-20" {
				out = append(out,
					scheduling.Island{Kind: i.Kind, ResourceID: "priya", From: on("07:00"), To: on("08:00")},
					scheduling.Island{Kind: i.Kind, ResourceID: "priya", From: on("08:45"), To: on("17:00")})
				continue
			}
			out = append(out, i)
		}
		return out
	}
}

func TestBook_NoCandidatePairIsSlotTakenWithTheNearestAlternatives(t *testing.T) {
	f := newFixture(t)
	f.islands = priyaBusyAt8(f)
	cmd := booking()
	cmd.ServiceTypeID = "mot"

	_, err := f.service().Book(ctx, cmd)

	var taken *scheduling.SlotTakenError
	if !errors.As(err, &taken) {
		t.Fatalf("error = %v, want SlotTakenError", err)
	}
	wantStarts := []time.Time{on("07:15"), on("08:45"), on("07:00"), on("09:00"), on("09:15")}
	var gotStarts []time.Time
	for _, s := range taken.Alternatives {
		gotStarts = append(gotStarts, s.StartAt)
		if !s.EndAt.Equal(s.StartAt.Add(45 * time.Minute)) {
			t.Errorf("alternative %s ends %s, want start + 45 minutes", s.StartAt, s.EndAt)
		}
	}
	if !slices.EqualFunc(gotStarts, wantStarts, time.Time.Equal) {
		t.Errorf("alternatives = %v, want %v", gotStarts, wantStarts)
	}
	if taken.Location != london {
		t.Errorf("alternatives location = %v, want Europe/London", taken.Location)
	}
	if f.lookups != 2 {
		t.Errorf("booking key looked up %d times, want 2 (before capacity, then before answering SLOT_TAKEN)", f.lookups)
	}
}

func TestBook_ConfirmsInOneTransactionInStatementOrder(t *testing.T) {
	f := newFixture(t)
	f.loads = distinctLoads
	f.expectTx(1)
	var inserted scheduling.Appointment
	bayRun := cellRun(on("08:00"), 3)
	gomock.InOrder(
		f.tx.EXPECT().InsertAppointment(gomock.Any()).DoAndReturn(func(a scheduling.Appointment) error { inserted = a; return nil }),
		f.tx.EXPECT().ClaimPair("man01", "bay-1", runEq{bayRun}, "sam", runEq{bayRun}, apptID).Return(nil),
		f.tx.EXPECT().AppendEvent(apptID, "CONFIRMED", "anonymous", scheduling.ChannelAdvisorConsole, gomock.Any()).Return(nil),
		f.tx.EXPECT().AppendOutbox("Appointment", apptID, "AppointmentConfirmed", gomock.Any()).Return(nil),
	)
	cmd := booking()
	cmd.CustomerRequests = scheduling.CustomerRequests{Notes: "Rattle from rear left"}
	cmd.Notes = "Call when ready"

	rec, err := f.service().Book(ctx, cmd)

	if err != nil {
		t.Fatalf("Book: %v", err)
	}
	want := scheduling.Appointment{ID: apptID, TenantID: tenant, DealershipID: "man01", CustomerID: "alice", VehicleID: "yk21",
		ServiceTypeID: "interim", Kind: scheduling.KindDropOff, ServiceBayID: "bay-1", TechnicianID: "sam",
		Status: scheduling.StatusConfirmed, CustomerRequests: cmd.CustomerRequests, Notes: "Call when ready",
		Channel: scheduling.ChannelAdvisorConsole, Version: 1}
	got := inserted
	if !got.StartAt.Equal(on("08:00")) || !got.EndAt.Equal(on("08:45")) {
		t.Errorf("inserted times %s to %s, want 08:00Z to 08:45Z (start + duration, no buffers)", got.StartAt, got.EndAt)
	}
	got.StartAt, got.EndAt, got.CreatedAt, got.UpdatedAt = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	if got != want {
		t.Errorf("inserted %+v\nwant     %+v", got, want)
	}
	if rec.Appointment.ID != apptID || rec.Bay != bay1 || rec.Technician.ID != "sam" || rec.Dealership.Code != "MAN01" ||
		rec.ServiceType.Code != "INTERIM" || rec.Customer != alice || rec.Vehicle != aliceCar {
		t.Errorf("record = %+v", rec)
	}
	wantCells := scheduling.CellRange{From: on("08:00"), To: on("08:45"), Cells: 3}
	if rec.Cells.Bay == nil || rec.Cells.Technician == nil || !sameRange(*rec.Cells.Bay, wantCells) || !sameRange(*rec.Cells.Technician, wantCells) {
		t.Errorf("record cells = %+v, want bay and technician %+v", rec.Cells, wantCells)
	}
	if !slices.Contains(f.resourcesAsked, "2026-10-20") {
		t.Errorf("resources read for %v, want the appointment's local date 2026-10-20", f.resourcesAsked)
	}
}

func sameRange(a, b scheduling.CellRange) bool {
	return a.From.Equal(b.From) && a.To.Equal(b.To) && a.Cells == b.Cells
}

func TestBook_CustomerChannelNotesAreNotStored(t *testing.T) {
	f := newFixture(t)
	f.loads = distinctLoads
	f.expectTx(1)
	var inserted scheduling.Appointment
	bayRun := cellRun(on("08:00"), 3)
	gomock.InOrder(
		f.tx.EXPECT().InsertAppointment(gomock.Any()).DoAndReturn(func(a scheduling.Appointment) error { inserted = a; return nil }),
		f.tx.EXPECT().ClaimPair("man01", "bay-1", runEq{bayRun}, "sam", runEq{bayRun}, apptID).Return(nil),
		f.tx.EXPECT().AppendEvent(apptID, "CONFIRMED", "anonymous", scheduling.ChannelCustomerWeb, gomock.Any()).Return(nil),
		f.tx.EXPECT().AppendOutbox("Appointment", apptID, "AppointmentConfirmed", gomock.Any()).Return(nil),
	)
	cmd := booking()
	cmd.Channel, cmd.Notes = scheduling.ChannelCustomerWeb, "Internal: offer a valet"

	if _, err := f.service().Book(ctx, cmd); err != nil {
		t.Fatalf("Book: %v", err)
	}
	if inserted.Notes != "" || inserted.Channel != scheduling.ChannelCustomerWeb {
		t.Errorf("inserted notes %q channel %s, want no notes through CUSTOMER_WEB", inserted.Notes, inserted.Channel)
	}
}

// The scenario "The least-loaded qualified bay and technician get the job".
func TestBook_TriesTheLeastLoadedPairFirst(t *testing.T) {
	f := newFixture(t)
	f.loads = scheduling.Loads{"bay-3": 10, "sam": 10, "bay-2": 3, "priya": 3}
	f.expectTx(1)
	run := cellRun(on("12:00"), 3)
	gomock.InOrder(f.attempt("bay-1", "lee", run, run, nil)...)
	cmd := booking()
	cmd.StartAt = on("12:00")

	rec, err := f.service().Book(ctx, cmd)

	if err != nil || rec.Bay != bay1 || rec.Technician.ID != "lee" {
		t.Errorf("Book = %+v, %v; want Bay 1 with Lee", rec.Appointment, err)
	}
}

func TestBook_SlotTakenMovesToTheNextPair(t *testing.T) {
	f := newFixture(t)
	f.loads = distinctLoads
	f.expectTx(2)
	run := cellRun(on("08:00"), 3)
	gomock.InOrder(slices.Concat(
		f.attempt("bay-1", "sam", run, run, fmt.Errorf("claim bay: %w", scheduling.ErrSlotTaken)),
		f.attempt("bay-1", "priya", run, run, nil),
	)...)

	rec, err := f.service().Book(ctx, booking())

	if err != nil || rec.Technician.ID != "priya" {
		t.Errorf("Book = %+v, %v; want the second pair, Bay 1 with Priya", rec.Appointment, err)
	}
}

func TestBook_DeadlockVictimRetriesTheSamePairOnce(t *testing.T) {
	f := newFixture(t)
	f.loads = distinctLoads
	f.expectTx(2)
	run := cellRun(on("08:00"), 3)
	gomock.InOrder(slices.Concat(
		f.attempt("bay-1", "sam", run, run, fmt.Errorf("claim: %w", scheduling.ErrDeadlockVictim)),
		f.attempt("bay-1", "sam", run, run, nil),
	)...)

	rec, err := f.service().Book(ctx, booking())

	if err != nil || rec.Technician.ID != "sam" {
		t.Errorf("Book = %+v, %v; want the same pair after one retry", rec.Appointment, err)
	}
}

func TestBook_SecondDeadlockOnAPairMovesOn(t *testing.T) {
	f := newFixture(t)
	f.loads = distinctLoads
	f.expectTx(3)
	run := cellRun(on("08:00"), 3)
	gomock.InOrder(slices.Concat(
		f.attempt("bay-1", "sam", run, run, scheduling.ErrDeadlockVictim),
		f.attempt("bay-1", "sam", run, run, scheduling.ErrDeadlockVictim),
		f.attempt("bay-1", "priya", run, run, nil),
	)...)

	rec, err := f.service().Book(ctx, booking())

	if err != nil || rec.Technician.ID != "priya" {
		t.Errorf("Book = %+v, %v; want the next pair after two deadlocks", rec.Appointment, err)
	}
}

func TestBook_MissingCellsRecomputeCandidatesOnce(t *testing.T) {
	f := newFixture(t)
	f.loads = distinctLoads
	// After the first claim fails, the recomputed ledger no longer has Sam that day.
	f.islands = func(day scheduling.DayWindow, call int) []scheduling.Island {
		is := f.wholeDay(day)
		if call < 2 {
			return is
		}
		return slices.DeleteFunc(is, func(i scheduling.Island) bool { return i.ResourceID == "sam" })
	}
	f.expectTx(2)
	run := cellRun(on("08:00"), 3)
	gomock.InOrder(slices.Concat(
		f.attempt("bay-1", "sam", run, run, fmt.Errorf("claim: %w", scheduling.ErrTechnicianNotWorking)),
		f.attempt("bay-1", "priya", run, run, nil),
	)...)

	rec, err := f.service().Book(ctx, booking())

	if err != nil || rec.Technician.ID != "priya" {
		t.Errorf("Book = %+v, %v; want the first pair of the recomputed list", rec.Appointment, err)
	}
}

func TestBook_MissingCellsTwiceIsAvailabilityChanged(t *testing.T) {
	f := newFixture(t)
	f.loads = distinctLoads
	f.expectTx(2)
	run := cellRun(on("08:00"), 3)
	gomock.InOrder(slices.Concat(
		f.attempt("bay-1", "sam", run, run, scheduling.ErrTechnicianNotWorking),
		f.attempt("bay-1", "sam", run, run, scheduling.ErrBayNotOpen),
	)...)

	_, err := f.service().Book(ctx, booking())

	var changed *scheduling.AvailabilityChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("error = %v, want AvailabilityChangedError", err)
	}
	if n := len(changed.Alternatives); n < 1 || n > 5 {
		t.Errorf("%d alternatives, want 1 to 5", n)
	}
	if f.lookups != 2 {
		t.Errorf("booking key looked up %d times, want 2 (before capacity, then before answering)", f.lookups)
	}
}

func TestBook_DuplicateKeyOnInsertReturnsTheExistingAppointment(t *testing.T) {
	f := newFixture(t)
	f.existing, f.existingFrom = storedAppointment("interim", scheduling.StatusConfirmed), 2 // committed while we computed
	f.loads = distinctLoads
	f.expectTx(1)
	f.tx.EXPECT().InsertAppointment(gomock.Any()).Return(fmt.Errorf("insert: %w", scheduling.ErrDuplicateBooking))

	rec, err := f.service().Book(ctx, booking())

	if err != nil || rec.Appointment.ID != "existing-1" {
		t.Errorf("Book = %+v, %v; want the existing appointment", rec.Appointment, err)
	}
}

func TestBook_DuplicateKeyOnInsertWithADifferentRequestIsADuplicateBooking(t *testing.T) {
	f := newFixture(t)
	f.existing, f.existingFrom = storedAppointment("major", scheduling.StatusConfirmed), 2
	f.loads = distinctLoads
	f.expectTx(1)
	f.tx.EXPECT().InsertAppointment(gomock.Any()).Return(scheduling.ErrDuplicateBooking)

	_, err := f.service().Book(ctx, booking())

	var dup *scheduling.DuplicateBookingError
	if !errors.As(err, &dup) || dup.Existing.ID != "existing-1" || dup.ServiceTypeCode != "MAJOR" {
		t.Errorf("error = %v, want DuplicateBookingError for existing-1 (MAJOR)", err)
	}
}

func TestBook_ExhaustedPairsRecheckTheKeyBeforeSlotTaken(t *testing.T) {
	f := newFixture(t)
	mine := storedAppointment("mot", scheduling.StatusConfirmed)
	mine.ServiceBayID, mine.TechnicianID = "bay-2", "priya"
	f.existing, f.existingFrom = mine, 2 // an identical concurrent request won the only pair
	f.expectTx(1)
	run := cellRun(on("08:00"), 3)
	gomock.InOrder(f.attempt("bay-2", "priya", run, run, scheduling.ErrSlotTaken)...)
	cmd := booking()
	cmd.ServiceTypeID = "mot"

	rec, err := f.service().Book(ctx, cmd)

	if err != nil || rec.Appointment.ID != "existing-1" {
		t.Errorf("Book = %+v, %v; want the concurrent identical booking, not SLOT_TAKEN", rec.Appointment, err)
	}
}

func TestBook_ExhaustedPairsIsSlotTaken(t *testing.T) {
	f := newFixture(t)
	f.expectTx(1)
	run := cellRun(on("08:00"), 3)
	gomock.InOrder(f.attempt("bay-2", "priya", run, run, scheduling.ErrSlotTaken)...)
	cmd := booking()
	cmd.ServiceTypeID = "mot"

	_, err := f.service().Book(ctx, cmd)

	var taken *scheduling.SlotTakenError
	if !errors.As(err, &taken) {
		t.Fatalf("error = %v, want SlotTakenError", err)
	}
	for _, s := range taken.Alternatives {
		if s.StartAt.Equal(on("08:00")) {
			t.Errorf("alternatives include the requested start: %v", taken.Alternatives)
		}
	}
	if n := len(taken.Alternatives); n < 1 || n > 5 {
		t.Errorf("%d alternatives, want 1 to 5", n)
	}
}

func TestBook_UnexpectedStoreErrorsAreReturned(t *testing.T) {
	f := newFixture(t)
	f.loads = distinctLoads
	f.expectTx(1)
	boom := errors.New("connection reset")
	f.tx.EXPECT().InsertAppointment(gomock.Any()).Return(boom)

	_, err := f.service().Book(ctx, booking())

	if !errors.Is(err, boom) || errors.Is(err, scheduling.ErrSlotTaken) {
		t.Errorf("error = %v, want the store error, not a business outcome", err)
	}
}
