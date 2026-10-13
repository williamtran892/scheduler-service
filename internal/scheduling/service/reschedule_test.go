package service

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"scheduler/internal/scheduling"
)

// Reschedule and reassign (spec FR-010 to FR-015; design 5.4; research R4). The fixture
// appointment is a CONFIRMED INTERIM at 2026-10-20 08:00Z on Bay 1 with Lee, version 1; the
// clock reads 2026-10-19T08:00Z.

func moveTo(start string) RescheduleCommand {
	at := on(start)
	return RescheduleCommand{TransitionCommand: command(1), StartAt: &at}
}

// moved expects one reschedule attempt on a pair, in order: the version-guarded update, the
// claim with the appointment's own id, the release of everything outside the new runs, the
// event and the outbox row. claimErr stops it after the claim.
func (f *fixture) moved(written *scheduling.Appointment, bayID, techID string, run scheduling.CellRun, claimErr error) []any {
	calls := []any{
		f.update(1, written, nil),
		f.tx.EXPECT().ClaimPair("man01", bayID, runEq{run}, techID, runEq{run}, lifeID).Return(claimErr),
	}
	if claimErr == nil {
		calls = append(calls,
			f.tx.EXPECT().ReleaseCells(lifeID, keepEq{scheduling.Keep{
				Bay:        &scheduling.ResourceRun{ResourceID: bayID, Run: run},
				Technician: &scheduling.ResourceRun{ResourceID: techID, Run: run},
			}}).Return(3, nil),
			f.tx.EXPECT().AppendEvent(lifeID, "RESCHEDULED", "anonymous", scheduling.ChannelAdvisorConsole,
				payloadHas{"previous": map[string]any{"startAt": "2026-10-20T08:00:00Z", "endAt": "2026-10-20T08:45:00Z",
					"serviceBayId": "bay-1", "technicianId": "lee"}}).Return(nil),
			f.tx.EXPECT().AppendOutbox("Appointment", lifeID, "AppointmentRescheduled", gomock.Any()).Return(nil),
		)
	}
	return calls
}

func TestReschedule_MovesInOneTransactionKeepingTheCurrentPairWhenItIsFree(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusConfirmed, 1)
	f.loads = distinctLoads // bay-1/sam is least loaded; the current pair bay-1/lee still goes first
	f.expectTx(1)
	run := cellRun(on("09:00"), 3)
	var written scheduling.Appointment
	gomock.InOrder(f.moved(&written, "bay-1", "lee", run, nil)...)

	rec, err := f.service().Reschedule(ctx, moveTo("09:00"))

	if err != nil {
		t.Fatalf("Reschedule: %v", err)
	}
	if written.Status != scheduling.StatusConfirmed || written.Version != 2 || !written.StartAt.Equal(on("09:00")) ||
		!written.EndAt.Equal(on("09:45")) || written.ServiceBayID != "bay-1" || written.TechnicianID != "lee" {
		t.Errorf("written %+v, want CONFIRMED v2 from 09:00Z to 09:45Z on bay-1 with lee", written)
	}
	if rec.Appointment.Version != 2 || !rec.Appointment.StartAt.Equal(on("09:00")) {
		t.Errorf("record v%d at %s, want v2 at 09:00Z", rec.Appointment.Version, rec.Appointment.StartAt)
	}
	if len(f.ledgerOwners) == 0 || slices.ContainsFunc(f.ledgerOwners, func(o string) bool { return o != lifeID }) {
		t.Errorf("ledger read with owners %v, want every read with %q so its own cells count as free", f.ledgerOwners, lifeID)
	}
}

func TestReschedule_APureReassignKeepsTheStartAndTheCurrentBay(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusConfirmed, 1)
	f.expectTx(1)
	run := cellRun(on("08:00"), 3)
	var written scheduling.Appointment
	gomock.InOrder(f.moved(&written, "bay-1", "priya", run, nil)...)

	_, err := f.service().Reschedule(ctx, RescheduleCommand{TransitionCommand: command(1), TechnicianID: "priya"})

	if err != nil {
		t.Fatalf("Reschedule: %v", err)
	}
	if !written.StartAt.Equal(on("08:00")) || written.ServiceBayID != "bay-1" || written.TechnicianID != "priya" {
		t.Errorf("written at %s on %s/%s, want 08:00Z on bay-1 with priya", written.StartAt, written.ServiceBayID, written.TechnicianID)
	}
}

func TestReschedule_PinningBayAndTechnicianUsesExactlyThatPair(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusConfirmed, 1)
	f.expectTx(1)
	run := cellRun(on("09:00"), 3)
	var written scheduling.Appointment
	gomock.InOrder(f.moved(&written, "bay-2", "sam", run, nil)...)
	cmd := moveTo("09:00")
	cmd.ServiceBayID, cmd.TechnicianID = "bay-2", "sam"

	if _, err := f.service().Reschedule(ctx, cmd); err != nil {
		t.Fatalf("Reschedule: %v", err)
	}
}

func TestReschedule_TheNewStartFollowsTheBookingRules(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *fixture, cmd *RescheduleCommand)
		check func(t *testing.T, err error)
	}{
		{"off the grid", func(_ *fixture, c *RescheduleCommand) { *c = moveTo("09:05") },
			func(t *testing.T, err error) { mustBe(t, err, scheduling.ErrOffGrid) }},
		{"in the past", func(_ *fixture, c *RescheduleCommand) { at := now.Add(-15 * time.Minute); c.StartAt = &at },
			func(t *testing.T, err error) { requireField(t, err, "INVALID_VALUE", "startAt") }},
		{"beyond the horizon", func(_ *fixture, c *RescheduleCommand) { at := onDay(20, "08:00").AddDate(0, 1, 0); c.StartAt = &at },
			func(t *testing.T, err error) { requireField(t, err, "INVALID_VALUE", "startAt") }},
		{"a pure reassign after the booked start has passed", func(f *fixture, c *RescheduleCommand) {
			f.clock.Set(on("08:10"))
			*c = RescheduleCommand{TransitionCommand: command(1), TechnicianID: "priya"}
		}, func(t *testing.T, err error) { requireField(t, err, "INVALID_VALUE", "startAt") }},
		{"outside working time", func(_ *fixture, c *RescheduleCommand) { *c = moveTo("05:00") },
			func(t *testing.T, err error) { mustBe(t, err, scheduling.ErrOutsideOpeningHours) }},
		{"a staff-only service through a customer channel", func(f *fixture, c *RescheduleCommand) {
			a := f.stored[lifeID]
			a.ServiceTypeID = "diagnostic"
			f.stored[lifeID] = a
			c.Channel = scheduling.ChannelCustomerWeb
		}, func(t *testing.T, err error) { mustBe(t, err, scheduling.ErrNotBookableOnline) }},
		{"nobody qualified on the day", func(f *fixture, _ *RescheduleCommand) {
			f.techs = []scheduling.Technician{{ID: "trainee", Name: "Trainee"}}
		}, func(t *testing.T, err error) { mustBe(t, err, scheduling.ErrNoQualifiedTechnician) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.storedAt(scheduling.StatusConfirmed, 1)
			cmd := moveTo("09:00")
			tt.setup(f, &cmd)

			_, err := f.service().Reschedule(ctx, cmd)

			tt.check(t, err)
		})
	}
}

func TestReschedule_PinnedResourcesAreValidatedLikeBooking(t *testing.T) {
	tests := []struct {
		name    string
		service string
		bay     string
		tech    string
		check   func(t *testing.T, err error)
	}{
		{"an unknown bay", "interim", "bay-9", "", func(t *testing.T, err error) { requireField(t, err, "INVALID_VALUE", "serviceBayId") }},
		{"a bay of a type the service cannot use", "mot", "bay-1", "", func(t *testing.T, err error) {
			requireField(t, err, "INVALID_VALUE", "serviceBayId")
		}},
		{"an unknown technician", "interim", "", "nobody", func(t *testing.T, err error) {
			requireField(t, err, "INVALID_VALUE", "technicianId")
		}},
		{"a technician without the required skill", "mot", "", "lee", func(t *testing.T, err error) {
			mustBe(t, err, scheduling.ErrNoQualifiedTechnician)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			a := f.storedAt(scheduling.StatusConfirmed, 1)
			a.ServiceTypeID = tt.service
			if tt.service == "mot" {
				a.ServiceBayID, a.TechnicianID = "bay-2", "priya"
			}
			f.stored[lifeID] = a
			cmd := RescheduleCommand{TransitionCommand: command(1), ServiceBayID: tt.bay, TechnicianID: tt.tech}

			_, err := f.service().Reschedule(ctx, cmd)

			tt.check(t, err)
		})
	}
}

func TestReschedule_ATakenPairMovesToTheNextOne(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusConfirmed, 1)
	f.loads = distinctLoads
	f.expectTx(2)
	run := cellRun(on("09:00"), 3)
	var first, second scheduling.Appointment
	gomock.InOrder(slices.Concat(
		f.moved(&first, "bay-1", "lee", run, fmt.Errorf("claim: %w", scheduling.ErrSlotTaken)),
		f.moved(&second, "bay-1", "sam", run, nil),
	)...)

	if _, err := f.service().Reschedule(ctx, moveTo("09:00")); err != nil {
		t.Fatalf("Reschedule: %v", err)
	}
	if second.TechnicianID != "sam" {
		t.Errorf("second attempt with %s, want sam", second.TechnicianID)
	}
}

func TestReschedule_ADeadlockVictimRetriesTheSamePairOnce(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusConfirmed, 1)
	f.expectTx(2)
	run := cellRun(on("09:00"), 3)
	var first, second scheduling.Appointment
	gomock.InOrder(slices.Concat(
		f.moved(&first, "bay-1", "lee", run, scheduling.ErrDeadlockVictim),
		f.moved(&second, "bay-1", "lee", run, nil),
	)...)

	if _, err := f.service().Reschedule(ctx, moveTo("09:00")); err != nil {
		t.Fatalf("Reschedule: %v", err)
	}
}

func TestReschedule_MissingCellsTwiceIsAvailabilityChanged(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusConfirmed, 1)
	f.expectTx(2)
	run := cellRun(on("09:00"), 3)
	var first, second scheduling.Appointment
	gomock.InOrder(slices.Concat(
		f.moved(&first, "bay-1", "lee", run, scheduling.ErrTechnicianNotWorking),
		f.moved(&second, "bay-1", "lee", run, scheduling.ErrBayNotOpen),
	)...)

	_, err := f.service().Reschedule(ctx, moveTo("09:00"))

	var changed *scheduling.AvailabilityChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("error = %v, want AvailabilityChangedError", err)
	}
}

func TestReschedule_NoFreePairIsSlotTakenAndChangesNothing(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusConfirmed, 1)
	f.free = func(int) scheduling.Free { return nil } // every resource is booked for 09:00Z to 09:45Z

	_, err := f.service().Reschedule(ctx, moveTo("09:00"))

	var taken *scheduling.SlotTakenError
	if !errors.As(err, &taken) {
		t.Fatalf("error = %v, want SlotTakenError", err)
	}
	if a := f.stored[lifeID]; a.Version != 1 || !a.StartAt.Equal(on("08:00")) {
		t.Errorf("appointment v%d at %s, want it unchanged", a.Version, a.StartAt)
	}
}

func TestReschedule_OntoAnotherAppointmentsBookingKeyIsADuplicateBooking(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusConfirmed, 1)
	cancelled := storedAppointment("interim", scheduling.StatusCancelled)
	cancelled.ID, cancelled.StartAt, cancelled.EndAt = "cancelled-1", on("09:00"), on("09:45")
	f.existing, f.existingFrom = cancelled, 1
	f.expectTx(1)
	var written scheduling.Appointment
	f.update(1, &written, fmt.Errorf("update appointment: %w", scheduling.ErrDuplicateBooking))

	_, err := f.service().Reschedule(ctx, moveTo("09:00"))

	var dup *scheduling.DuplicateBookingError
	if !errors.As(err, &dup) || dup.Existing.ID != "cancelled-1" {
		t.Fatalf("error = %v, want DuplicateBookingError carrying cancelled-1", err)
	}
}

func TestReschedule_AStaleVersionAtTheWriteIsAVersionConflict(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusConfirmed, 1)
	f.expectTx(1)
	var written scheduling.Appointment
	f.update(1, &written, scheduling.ErrStaleVersion)

	_, err := f.service().Reschedule(ctx, moveTo("09:00"))

	if !errors.Is(err, scheduling.ErrStaleVersion) {
		t.Errorf("error = %v, want VERSION_CONFLICT", err)
	}
}

func TestReschedule_CustomersMayMoveInTimeOnly(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusConfirmed, 1)
	f.expectTx(1)
	run := cellRun(on("09:00"), 3)
	var written scheduling.Appointment
	gomock.InOrder(
		f.update(1, &written, nil),
		f.tx.EXPECT().ClaimPair("man01", "bay-1", runEq{run}, "lee", runEq{run}, lifeID).Return(nil),
		f.tx.EXPECT().ReleaseCells(lifeID, gomock.Any()).Return(3, nil),
		f.tx.EXPECT().AppendEvent(lifeID, "RESCHEDULED", "anonymous", scheduling.ChannelCustomerApp, gomock.Any()).Return(nil),
		f.tx.EXPECT().AppendOutbox("Appointment", lifeID, "AppointmentRescheduled", gomock.Any()).Return(nil),
	)
	cmd := moveTo("09:00")
	cmd.Channel = scheduling.ChannelCustomerApp

	if _, err := f.service().Reschedule(ctx, cmd); err != nil {
		t.Fatalf("Reschedule: %v", err)
	}
}

func mustBe(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Errorf("error = %v, want %v", err, want)
	}
}
