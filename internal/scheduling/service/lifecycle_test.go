package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
)

// The lifecycle transitions (spec FR-001 to FR-052; research R1, R5, R6, R8). Each test drives
// one method against the mocks and the booking fixture: an INTERIM for Alice at MAN01 on
// 2026-10-20 09:00 London (08:00Z) on Bay 1 with Lee, id "existing-1". The clock reads
// 2026-10-19T08:00Z unless a test moves it.

const lifeID = "existing-1"

// stored puts the fixture appointment in the store with the given status and version and
// returns it.
func (f *fixture) storedAt(status scheduling.AppointmentStatus, version int) scheduling.Appointment {
	a := storedAppointment("interim", status)
	a.Version = version
	f.stored[a.ID] = a
	f.owned[a.ID] = scheduling.OwnedCells{
		Bay:        &scheduling.CellRange{From: on("08:00"), To: on("08:45"), Cells: 3},
		Technician: &scheduling.CellRange{From: on("08:00"), To: on("08:45"), Cells: 3},
	}
	return a
}

// update expects the version-guarded update of the appointment and captures what was written.
// It also stores it, so whatever the service reads back afterwards is the new state.
func (f *fixture) update(expectedVersion int, written *scheduling.Appointment, err error) *gomock.Call {
	return f.tx.EXPECT().UpdateAppointment(gomock.Any(), expectedVersion).DoAndReturn(
		func(a scheduling.Appointment, _ int) error {
			*written = a
			if err == nil {
				f.stored[a.ID] = a
			}
			return err
		})
}

// command is a transition command for the fixture appointment through the advisor console.
func command(version int) TransitionCommand {
	return TransitionCommand{AppointmentID: lifeID, ExpectedVersion: version, Channel: scheduling.ChannelAdvisorConsole}
}

// strictService is a Service whose dependencies expect no call at all: any read or write fails
// the test.
func strictService(t *testing.T) Service {
	ctrl := gomock.NewController(t)
	return NewService(NewMockStore(ctrl), NewMockWorkshopReader(ctrl), NewMockCustomerReader(ctrl),
		platform.NewFakeClock(now), fixedID(apptID))
}

// call runs the named lifecycle method with the base command tc.
func call(svc Service, method string, tc TransitionCommand) (Record, error) {
	switch method {
	case "Reschedule":
		start := on("09:00")
		return svc.Reschedule(ctx, RescheduleCommand{TransitionCommand: tc, StartAt: &start})
	case "Cancel":
		return svc.Cancel(ctx, tc)
	case "CheckIn":
		return svc.CheckIn(ctx, CheckInCommand{TransitionCommand: tc})
	case "Start":
		return svc.Start(ctx, StartCommand{TransitionCommand: tc})
	case "Complete":
		return svc.Complete(ctx, CompleteCommand{TransitionCommand: tc})
	case "MarkNoShow":
		return svc.MarkNoShow(ctx, tc)
	}
	panic("unknown method " + method)
}

var lifecycleMethods = []string{"Reschedule", "Cancel", "CheckIn", "Start", "Complete", "MarkNoShow"}

// keepEq matches a scheduling.Keep by resource ids, runs and instants.
type keepEq struct{ want scheduling.Keep }

func (m keepEq) Matches(x any) bool {
	k, ok := x.(scheduling.Keep)
	if !ok || !k.Before.Equal(m.want.Before) {
		return false
	}
	same := func(a, b *scheduling.ResourceRun) bool {
		if a == nil || b == nil {
			return a == b
		}
		return a.ResourceID == b.ResourceID && runEq{b.Run}.Matches(a.Run)
	}
	return same(k.Bay, m.want.Bay) && same(k.Technician, m.want.Technician)
}

func (m keepEq) String() string { return fmt.Sprintf("keep %+v", m.want) }

// payloadHas matches an event payload whose JSON form has every key with the given value
// (compared in JSON form).
type payloadHas map[string]any

func (m payloadHas) Matches(x any) bool {
	raw, err := json.Marshal(x)
	if err != nil {
		return false
	}
	var got map[string]any
	if json.Unmarshal(raw, &got) != nil {
		return false
	}
	for k, v := range m {
		want, _ := json.Marshal(v)
		have, ok := got[k]
		if !ok {
			return false
		}
		haveRaw, _ := json.Marshal(have)
		if string(haveRaw) != string(want) {
			return false
		}
	}
	return true
}

func (m payloadHas) String() string { return fmt.Sprintf("a payload with %v", map[string]any(m)) }

// ---- the order of checks (FR-003) ------------------------------------------------------------

func TestLifecycle_StaffActionsThroughACustomerChannelAreForbiddenBeforeAnyRead(t *testing.T) {
	customer := command(1)
	customer.Channel = scheduling.ChannelCustomerWeb
	for _, method := range []string{"CheckIn", "Start", "Complete", "MarkNoShow"} {
		t.Run(method, func(t *testing.T) {
			_, err := call(strictService(t), method, customer)
			if !errors.Is(err, scheduling.ErrForbidden) {
				t.Errorf("error = %v, want ErrForbidden", err)
			}
		})
	}
	for name, cmd := range map[string]RescheduleCommand{
		"reschedule naming a technician": {TransitionCommand: customer, TechnicianID: "lee"},
		"reschedule naming a bay":        {TransitionCommand: customer, ServiceBayID: "bay-1"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := strictService(t).Reschedule(ctx, cmd)
			if !errors.Is(err, scheduling.ErrForbidden) {
				t.Errorf("error = %v, want ErrForbidden", err)
			}
		})
	}
}

func TestLifecycle_AnUnknownAppointmentIsNotFound(t *testing.T) {
	for _, method := range lifecycleMethods {
		t.Run(method, func(t *testing.T) {
			f := newFixture(t)
			tc := command(1)
			tc.AppointmentID = "nothing"

			_, err := call(f.service(), method, tc)

			if !errors.Is(err, scheduling.ErrNotFound) {
				t.Errorf("error = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestLifecycle_AStaleVersionIsAVersionConflictBeforeAnyTransaction(t *testing.T) {
	statusFor := map[string]scheduling.AppointmentStatus{
		"Reschedule": scheduling.StatusConfirmed, "Cancel": scheduling.StatusConfirmed, "CheckIn": scheduling.StatusConfirmed,
		"Start": scheduling.StatusCheckedIn, "Complete": scheduling.StatusInProgress, "MarkNoShow": scheduling.StatusConfirmed,
	}
	for _, method := range lifecycleMethods {
		t.Run(method, func(t *testing.T) {
			f := newFixture(t)
			f.storedAt(statusFor[method], 3)

			_, err := call(f.service(), method, command(2))

			var conflict *scheduling.VersionConflictError
			if !errors.As(err, &conflict) || conflict.Current != 3 || conflict.Sent != 2 {
				t.Errorf("error = %v, want VersionConflictError{Current: 3, Sent: 2}", err)
			}
		})
	}
}

func TestLifecycle_AStaleVersionWinsOverAnIllegalTransition(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusCompleted, 4)

	_, err := f.service().Cancel(ctx, command(3))

	if !errors.Is(err, scheduling.ErrStaleVersion) || errors.Is(err, scheduling.ErrInvalidTransition) {
		t.Errorf("error = %v, want VERSION_CONFLICT, not INVALID_TRANSITION", err)
	}
}

func TestLifecycle_ATransitionTheStateMachineDoesNotAllowIsRefusedBeforeAnyTransaction(t *testing.T) {
	tests := []struct {
		method string
		from   scheduling.AppointmentStatus
	}{
		{"Start", scheduling.StatusConfirmed},
		{"Complete", scheduling.StatusConfirmed},
		{"CheckIn", scheduling.StatusCheckedIn},
		{"MarkNoShow", scheduling.StatusCheckedIn},
		{"Reschedule", scheduling.StatusCheckedIn},
		{"Cancel", scheduling.StatusInProgress},
		{"Cancel", scheduling.StatusCompleted},
		{"Cancel", scheduling.StatusNoShow},
		{"CheckIn", scheduling.StatusCancelled},
		{"Start", scheduling.StatusNoShow},
		{"Reschedule", scheduling.StatusNoShow},
		{"Complete", scheduling.StatusCompleted},
	}
	for _, tt := range tests {
		t.Run(tt.method+" from "+string(tt.from), func(t *testing.T) {
			f := newFixture(t)
			f.storedAt(tt.from, 2)

			_, err := call(f.service(), tt.method, command(2))

			if !errors.Is(err, scheduling.ErrInvalidTransition) {
				t.Errorf("error = %v, want ErrInvalidTransition", err)
			}
		})
	}
}

func TestLifecycle_AChangeThatLandsBetweenReadAndWriteIsAVersionConflict(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusConfirmed, 1)
	f.expectTx(1)
	var written scheduling.Appointment
	f.update(1, &written, fmt.Errorf("update appointment: %w", scheduling.ErrStaleVersion))

	_, err := f.service().Cancel(ctx, command(1))

	if !errors.Is(err, scheduling.ErrStaleVersion) {
		t.Errorf("error = %v, want VERSION_CONFLICT", err)
	}
}

// ---- cancel and no-show (FR-020, FR-021, FR-050) ---------------------------------------------

func TestCancel_ReleasesEveryCellInOneTransactionInStatementOrder(t *testing.T) {
	for _, from := range []scheduling.AppointmentStatus{scheduling.StatusConfirmed, scheduling.StatusCheckedIn} {
		t.Run(string(from), func(t *testing.T) {
			f := newFixture(t)
			f.storedAt(from, 2)
			f.clock.Set(on("07:10"))
			f.expectTx(1)
			var written scheduling.Appointment
			gomock.InOrder(
				f.update(2, &written, nil),
				f.tx.EXPECT().ReleaseCells(lifeID, keepEq{scheduling.Keep{}}).Return(6, nil),
				f.tx.EXPECT().AppendEvent(lifeID, "CANCELLED", "anonymous", scheduling.ChannelAdvisorConsole,
					payloadHas{"reason": "Customer no longer needs the service"}).Return(nil),
				f.tx.EXPECT().AppendOutbox("Appointment", lifeID, "AppointmentCancelled", gomock.Any()).Return(nil),
			)
			cmd := command(2)
			cmd.Reason = "Customer no longer needs the service"

			rec, err := f.service().Cancel(ctx, cmd)

			if err != nil {
				t.Fatalf("Cancel: %v", err)
			}
			if written.Status != scheduling.StatusCancelled || written.Version != 3 || !written.UpdatedAt.Equal(on("07:10")) {
				t.Errorf("written %s v%d updated %s, want CANCELLED v3 updated 07:10Z", written.Status, written.Version, written.UpdatedAt)
			}
			if rec.Appointment.Status != scheduling.StatusCancelled || rec.Appointment.Version != 3 {
				t.Errorf("record %s v%d, want CANCELLED v3", rec.Appointment.Status, rec.Appointment.Version)
			}
		})
	}
}

func TestCancel_OfACancelledAppointmentReturnsItUnchangedWhateverTheVersion(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusCancelled, 2)

	rec, err := f.service().Cancel(ctx, command(1)) // a retry of the original cancel

	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if rec.Appointment.Status != scheduling.StatusCancelled || rec.Appointment.Version != 2 || rec.Appointment.ID != lifeID {
		t.Errorf("record %s v%d, want the cancelled appointment unchanged at v2", rec.Appointment.Status, rec.Appointment.Version)
	}
}

func TestCancel_IsOpenToCustomerChannelsAndRecordsTheirChannel(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusConfirmed, 1)
	f.expectTx(1)
	var written scheduling.Appointment
	gomock.InOrder(
		f.update(1, &written, nil),
		f.tx.EXPECT().ReleaseCells(lifeID, keepEq{scheduling.Keep{}}).Return(6, nil),
		f.tx.EXPECT().AppendEvent(lifeID, "CANCELLED", "anonymous", scheduling.ChannelCustomerWeb, gomock.Any()).Return(nil),
		f.tx.EXPECT().AppendOutbox("Appointment", lifeID, "AppointmentCancelled", gomock.Any()).Return(nil),
	)
	cmd := command(1)
	cmd.Channel = scheduling.ChannelCustomerWeb

	if _, err := f.service().Cancel(ctx, cmd); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
}

func TestMarkNoShow_ReleasesEveryCellInOneTransactionInStatementOrder(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusConfirmed, 1)
	f.expectTx(1)
	var written scheduling.Appointment
	gomock.InOrder(
		f.update(1, &written, nil),
		f.tx.EXPECT().ReleaseCells(lifeID, keepEq{scheduling.Keep{}}).Return(6, nil),
		f.tx.EXPECT().AppendEvent(lifeID, "NO_SHOW", "anonymous", scheduling.ChannelAdvisorConsole, gomock.Any()).Return(nil),
		f.tx.EXPECT().AppendOutbox("Appointment", lifeID, "AppointmentNoShow", gomock.Any()).Return(nil),
	)

	rec, err := f.service().MarkNoShow(ctx, command(1))

	if err != nil {
		t.Fatalf("MarkNoShow: %v", err)
	}
	if written.Status != scheduling.StatusNoShow || written.Version != 2 || rec.Appointment.Status != scheduling.StatusNoShow {
		t.Errorf("written %s v%d, record %s; want NO_SHOW v2", written.Status, written.Version, rec.Appointment.Status)
	}
}

func TestMarkNoShow_ByTheSweeperRecordsItsActorAndNoChannel(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusConfirmed, 1)
	f.expectTx(1)
	var written scheduling.Appointment
	gomock.InOrder(
		f.update(1, &written, nil),
		f.tx.EXPECT().ReleaseCells(lifeID, gomock.Any()).Return(6, nil),
		f.tx.EXPECT().AppendEvent(lifeID, "NO_SHOW", "no-show-sweeper", scheduling.Channel(""), gomock.Any()).Return(nil),
		f.tx.EXPECT().AppendOutbox("Appointment", lifeID, "AppointmentNoShow", gomock.Any()).Return(nil),
	)

	_, err := f.service().MarkNoShow(ctx, TransitionCommand{AppointmentID: lifeID, ExpectedVersion: 1, Actor: "no-show-sweeper"})

	if err != nil {
		t.Fatalf("MarkNoShow: %v", err)
	}
}

// ---- check-in, start, complete (FR-030, FR-040, FR-041) --------------------------------------

func TestCheckIn_FromConfirmedChangesNoCellAndRecordsTheCheckInDetails(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusConfirmed, 1)
	f.expectTx(1)
	var written scheduling.Appointment
	gomock.InOrder(
		f.update(1, &written, nil),
		f.tx.EXPECT().AppendEvent(lifeID, "CHECKED_IN", "anonymous", scheduling.ChannelAdvisorConsole,
			payloadHas{"mileage": 41230, "additionalConcerns": "Intermittent warning light on cold start"}).Return(nil),
		f.tx.EXPECT().AppendOutbox("Appointment", lifeID, "AppointmentCheckedIn", gomock.Any()).Return(nil),
	)
	mileage := 41230

	rec, err := f.service().CheckIn(ctx, CheckInCommand{TransitionCommand: command(1), Mileage: &mileage,
		AdditionalConcerns: "Intermittent warning light on cold start"})

	if err != nil {
		t.Fatalf("CheckIn: %v", err)
	}
	if written.Status != scheduling.StatusCheckedIn || written.Version != 2 || !written.StartAt.Equal(on("08:00")) ||
		written.ServiceBayID != "bay-1" || written.TechnicianID != "lee" {
		t.Errorf("written %+v, want CHECKED_IN v2 with times and pair unchanged", written)
	}
	if rec.Appointment.Status != scheduling.StatusCheckedIn {
		t.Errorf("record status %s, want CHECKED_IN", rec.Appointment.Status)
	}
}

func TestStart_RecordsTheActualStartAndChangesNoCell(t *testing.T) {
	yesterday := on("07:00").Add(-24 * time.Hour)
	tests := []struct {
		name      string
		startedAt *time.Time
		want      time.Time
	}{
		{"now when no start time is sent", nil, now},
		{"the start time the dealer system sent", &yesterday, yesterday},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.storedAt(scheduling.StatusCheckedIn, 2)
			f.expectTx(1)
			var written scheduling.Appointment
			gomock.InOrder(
				f.update(2, &written, nil),
				f.tx.EXPECT().AppendEvent(lifeID, "STARTED", "anonymous", scheduling.ChannelAdvisorConsole, gomock.Any()).Return(nil),
				f.tx.EXPECT().AppendOutbox("Appointment", lifeID, "AppointmentStarted", gomock.Any()).Return(nil),
			)

			rec, err := f.service().Start(ctx, StartCommand{TransitionCommand: command(2), StartedAt: tt.startedAt})

			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			if written.Status != scheduling.StatusInProgress || written.Version != 3 || written.ActualStartAt == nil ||
				!written.ActualStartAt.Equal(tt.want) || written.ActualEndAt != nil {
				t.Errorf("written %s v%d actual %v to %v, want IN_PROGRESS v3 from %s", written.Status, written.Version,
					written.ActualStartAt, written.ActualEndAt, tt.want)
			}
			if rec.Appointment.ActualStartAt == nil {
				t.Error("record has no actual start")
			}
		})
	}
}

func TestStart_RefusesAStartTimeAfterNow(t *testing.T) {
	f := newFixture(t)
	f.storedAt(scheduling.StatusCheckedIn, 2)
	later := now.Add(time.Minute)

	_, err := f.service().Start(ctx, StartCommand{TransitionCommand: command(2), StartedAt: &later})

	requireField(t, err, "INVALID_VALUE", "startedAt")
}

func TestComplete_ReleasesFromTheCurrentGridCellAndKeepsThePastOnes(t *testing.T) {
	sent := on("08:30")
	tests := []struct {
		name        string
		completedAt *time.Time
		wantEnd     time.Time
	}{
		{"completed now", nil, on("08:40")},
		{"a completion time from the dealer system does not change what is released", &sent, on("08:30")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			a := f.storedAt(scheduling.StatusInProgress, 3)
			started := on("08:00")
			a.ActualStartAt = &started
			f.stored[lifeID] = a
			f.clock.Set(on("08:40")) // design 11.3: complete at 09:40 London on a job from 09:00
			f.expectTx(1)
			var written scheduling.Appointment
			gomock.InOrder(
				f.update(3, &written, nil),
				f.tx.EXPECT().ReleaseCells(lifeID, keepEq{scheduling.Keep{Before: on("08:45")}}).Return(1, nil),
				f.tx.EXPECT().AppendEvent(lifeID, "COMPLETED", "anonymous", scheduling.ChannelAdvisorConsole, gomock.Any()).Return(nil),
				f.tx.EXPECT().AppendOutbox("Appointment", lifeID, "AppointmentCompleted", gomock.Any()).Return(nil),
			)

			_, err := f.service().Complete(ctx, CompleteCommand{TransitionCommand: command(3), CompletedAt: tt.completedAt})

			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if written.Status != scheduling.StatusCompleted || written.Version != 4 || written.ActualEndAt == nil ||
				!written.ActualEndAt.Equal(tt.wantEnd) || written.ActualStartAt == nil || !written.ActualStartAt.Equal(started) {
				t.Errorf("written %s v%d actual %v to %v, want COMPLETED v4 from %s to %s", written.Status, written.Version,
					written.ActualStartAt, written.ActualEndAt, started, tt.wantEnd)
			}
		})
	}
}

func TestComplete_RefusesACompletionTimeAfterNowOrBeforeTheActualStart(t *testing.T) {
	later, early := now.Add(time.Minute), now.Add(-2*time.Hour)
	for name, completedAt := range map[string]time.Time{"after now": later, "before the actual start": early} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			a := f.storedAt(scheduling.StatusInProgress, 3)
			started := now.Add(-time.Hour)
			a.ActualStartAt = &started
			f.stored[lifeID] = a

			_, err := f.service().Complete(ctx, CompleteCommand{TransitionCommand: command(3), CompletedAt: &completedAt})

			requireField(t, err, "INVALID_VALUE", "completedAt")
		})
	}
}

// ---- late arrival (FR-031, FR-032) -----------------------------------------------------------

// lateArrival stores the fixture appointment as a NO_SHOW at version 2 on Bay 2 with Priya and
// sets the clock to 2026-10-20 10:07:30Z, inside the 10:00Z cell.
func (f *fixture) lateArrival() {
	a := f.storedAt(scheduling.StatusNoShow, 2)
	a.ServiceBayID, a.TechnicianID = "bay-2", "priya"
	f.stored[lifeID] = a
	f.owned[lifeID] = scheduling.OwnedCells{}
	f.loads = distinctLoads // bay-2/priya would be fifth by load
	f.clock.Set(on("10:07").Add(30 * time.Second))
}

func TestCheckIn_LateArrivalClaimsFromTheCurrentGridCellOnThePreviousPairFirst(t *testing.T) {
	f := newFixture(t)
	f.lateArrival()
	f.expectTx(1)
	run := cellRun(on("10:00"), 3)
	var written scheduling.Appointment
	gomock.InOrder(
		f.update(2, &written, nil),
		f.tx.EXPECT().ClaimPair("man01", "bay-2", runEq{run}, "priya", runEq{run}, lifeID).Return(nil),
		f.tx.EXPECT().AppendEvent(lifeID, "CHECKED_IN", "anonymous", scheduling.ChannelAdvisorConsole,
			payloadHas{"lateArrival": true}).Return(nil),
		f.tx.EXPECT().AppendOutbox("Appointment", lifeID, "AppointmentCheckedIn", gomock.Any()).Return(nil),
	)

	rec, err := f.service().CheckIn(ctx, CheckInCommand{TransitionCommand: command(2)})

	if err != nil {
		t.Fatalf("CheckIn: %v", err)
	}
	if written.Status != scheduling.StatusCheckedIn || written.Version != 3 || !written.StartAt.Equal(on("10:00")) ||
		!written.EndAt.Equal(on("10:45")) || written.ServiceBayID != "bay-2" || written.TechnicianID != "priya" {
		t.Errorf("written %+v, want CHECKED_IN v3 from 10:00Z to 10:45Z on bay-2 with priya", written)
	}
	if rec.Appointment.Status != scheduling.StatusCheckedIn {
		t.Errorf("record status %s, want CHECKED_IN", rec.Appointment.Status)
	}
	for _, owner := range f.ledgerOwners {
		if owner != lifeID {
			t.Errorf("ledger read with owner %q, want %q", owner, lifeID)
		}
	}
}

func TestCheckIn_LateArrivalMovesToTheLeastLoadedPairWhenThePreviousOneIsTaken(t *testing.T) {
	f := newFixture(t)
	f.lateArrival()
	f.expectTx(2)
	run := cellRun(on("10:00"), 3)
	var first, second scheduling.Appointment
	gomock.InOrder(
		f.update(2, &first, nil),
		f.tx.EXPECT().ClaimPair("man01", "bay-2", runEq{run}, "priya", runEq{run}, lifeID).Return(scheduling.ErrSlotTaken),
		f.update(2, &second, nil),
		f.tx.EXPECT().ClaimPair("man01", "bay-1", runEq{run}, "sam", runEq{run}, lifeID).Return(nil),
		f.tx.EXPECT().AppendEvent(lifeID, "CHECKED_IN", "anonymous", gomock.Any(), gomock.Any()).Return(nil),
		f.tx.EXPECT().AppendOutbox("Appointment", lifeID, "AppointmentCheckedIn", gomock.Any()).Return(nil),
	)

	if _, err := f.service().CheckIn(ctx, CheckInCommand{TransitionCommand: command(2)}); err != nil {
		t.Fatalf("CheckIn: %v", err)
	}
	if second.ServiceBayID != "bay-1" || second.TechnicianID != "sam" {
		t.Errorf("second attempt on %s/%s, want bay-1/sam", second.ServiceBayID, second.TechnicianID)
	}
}

func TestCheckIn_LateArrivalWithNothingFreeIsSlotTaken(t *testing.T) {
	tests := map[string]func(f *fixture){
		"no free cells": func(f *fixture) {
			f.free = func(int) scheduling.Free { return nil }
		},
		"nobody qualified on the day": func(f *fixture) {
			f.techs = []scheduling.Technician{{ID: "trainee", Name: "Trainee"}}
		},
		"no cells now, outside working time": func(f *fixture) { f.clock.Set(on("19:07")) },
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.lateArrival()
			setup(f)

			_, err := f.service().CheckIn(ctx, CheckInCommand{TransitionCommand: command(2)})

			var taken *scheduling.SlotTakenError
			if !errors.As(err, &taken) {
				t.Fatalf("error = %v, want SlotTakenError", err)
			}
			if f.stored[lifeID].Status != scheduling.StatusNoShow {
				t.Errorf("appointment is %s, want it still NO_SHOW", f.stored[lifeID].Status)
			}
		})
	}
}

func TestCheckIn_LateArrivalTriesEachPairInItsOwnTransaction(t *testing.T) {
	f := newFixture(t)
	f.lateArrival()
	f.expectTx(6)
	run := cellRun(on("10:00"), 3)
	order := [][2]string{{"bay-2", "priya"}, {"bay-1", "sam"}, {"bay-1", "priya"}, {"bay-1", "lee"}, {"bay-2", "sam"}, {"bay-2", "lee"}}
	var calls []any
	for _, p := range order {
		var w scheduling.Appointment
		calls = append(calls,
			f.update(2, &w, nil),
			f.tx.EXPECT().ClaimPair("man01", p[0], runEq{run}, p[1], runEq{run}, lifeID).Return(scheduling.ErrSlotTaken))
	}
	gomock.InOrder(calls...)

	_, err := f.service().CheckIn(ctx, CheckInCommand{TransitionCommand: command(2)})

	if !errors.Is(err, scheduling.ErrSlotTaken) || !slices.Contains(f.resourcesAsked, "2026-10-20") {
		t.Errorf("error = %v, resources asked %v; want SLOT_TAKEN after every pair, for 2026-10-20", err, f.resourcesAsked)
	}
}
