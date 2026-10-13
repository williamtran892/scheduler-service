package sweeper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"go.uber.org/mock/gomock"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
	"scheduler/internal/scheduling/service"
)

// The no-show sweeper (spec FR-051, FR-052; research R7), with a fake clock: now is
// 2026-10-20T09:31Z and the grace period 30 minutes, so the cutoff is 09:01Z.

var (
	ctx    = context.Background()
	now    = time.Date(2026, 10, 20, 9, 31, 0, 0, time.UTC)
	grace  = 30 * time.Minute
	cutoff = now.Add(-grace)
)

func confirmed(id string, start time.Time, version int) scheduling.Appointment {
	return scheduling.Appointment{ID: id, Status: scheduling.StatusConfirmed, StartAt: start, EndAt: start.Add(45 * time.Minute), Version: version}
}

type fixture struct {
	due    *MockAppointments
	marker *MockMarker
	logs   bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
	ctrl := gomock.NewController(t)
	return &fixture{due: NewMockAppointments(ctrl), marker: NewMockMarker(ctrl)}
}

func (f *fixture) service() Service {
	return NewService(f.due, f.marker, platform.NewFakeClock(now), grace, log.NewLogfmtLogger(&f.logs))
}

// sweeperCommand is the command the sweeper must send for a.
func sweeperCommand(a scheduling.Appointment) service.TransitionCommand {
	return service.TransitionCommand{AppointmentID: a.ID, ExpectedVersion: a.Version, Actor: "no-show-sweeper"}
}

func TestRunOnce_AsksForTheAppointmentsDueAtNowMinusTheGracePeriod(t *testing.T) {
	f := newFixture(t)
	f.due.EXPECT().DueNoShows(gomock.Any(), cutoff).Return(nil, nil)

	report, err := f.service().RunOnce(ctx)

	if err != nil || report != (Report{}) {
		t.Errorf("RunOnce = %+v, %v; want an empty report", report, err)
	}
}

func TestRunOnce_MarksEachDueAppointmentInItsOwnTransitionOldestFirst(t *testing.T) {
	f := newFixture(t)
	a1, a2 := confirmed("a1", cutoff.Add(-time.Hour), 1), confirmed("a2", cutoff.Add(-time.Minute), 3)
	f.due.EXPECT().DueNoShows(gomock.Any(), cutoff).Return([]scheduling.Appointment{a1, a2}, nil)
	gomock.InOrder(
		f.marker.EXPECT().MarkNoShow(gomock.Any(), sweeperCommand(a1)).Return(service.Record{}, nil),
		f.marker.EXPECT().MarkNoShow(gomock.Any(), sweeperCommand(a2)).Return(service.Record{}, nil),
	)

	report, err := f.service().RunOnce(ctx)

	if err != nil || report != (Report{Marked: 2}) {
		t.Errorf("RunOnce = %+v, %v; want 2 marked", report, err)
	}
}

func TestRunOnce_TheGraceBoundaryIsStrict(t *testing.T) {
	f := newFixture(t)
	onTheBoundary := confirmed("boundary", cutoff, 1)               // start + grace == now: not yet
	justPast := confirmed("past", cutoff.Add(-time.Microsecond), 1) // start + grace a microsecond before now
	f.due.EXPECT().DueNoShows(gomock.Any(), cutoff).Return([]scheduling.Appointment{justPast, onTheBoundary}, nil)
	f.marker.EXPECT().MarkNoShow(gomock.Any(), sweeperCommand(justPast)).Return(service.Record{}, nil)

	report, err := f.service().RunOnce(ctx)

	if err != nil || report.Marked != 1 {
		t.Errorf("RunOnce = %+v, %v; want only the appointment past the boundary marked", report, err)
	}
}

func TestRunOnce_SkipsAnAppointmentThatIsNoLongerConfirmed(t *testing.T) {
	f := newFixture(t)
	checkedIn := confirmed("checked-in", cutoff.Add(-time.Hour), 2)
	checkedIn.Status = scheduling.StatusCheckedIn
	due := confirmed("due", cutoff.Add(-time.Hour), 1)
	f.due.EXPECT().DueNoShows(gomock.Any(), cutoff).Return([]scheduling.Appointment{checkedIn, due}, nil)
	f.marker.EXPECT().MarkNoShow(gomock.Any(), sweeperCommand(due)).Return(service.Record{}, nil)

	report, err := f.service().RunOnce(ctx)

	if err != nil || report != (Report{Marked: 1, Skipped: 1}) {
		t.Errorf("RunOnce = %+v, %v; want 1 marked, 1 skipped", report, err)
	}
}

func TestRunOnce_GoesOnPastAnAppointmentAnotherActorChangedFirst(t *testing.T) {
	f := newFixture(t)
	a1, a2, a3 := confirmed("a1", cutoff.Add(-3*time.Hour), 1), confirmed("a2", cutoff.Add(-2*time.Hour), 1),
		confirmed("a3", cutoff.Add(-time.Hour), 1)
	f.due.EXPECT().DueNoShows(gomock.Any(), cutoff).Return([]scheduling.Appointment{a1, a2, a3}, nil)
	gomock.InOrder(
		f.marker.EXPECT().MarkNoShow(gomock.Any(), sweeperCommand(a1)).
			Return(service.Record{}, &scheduling.VersionConflictError{Current: 2, Sent: 1}),
		f.marker.EXPECT().MarkNoShow(gomock.Any(), sweeperCommand(a2)).
			Return(service.Record{}, fmt.Errorf("mark a2: %w", scheduling.ErrInvalidTransition)),
		f.marker.EXPECT().MarkNoShow(gomock.Any(), sweeperCommand(a3)).Return(service.Record{}, nil),
	)

	report, err := f.service().RunOnce(ctx)

	if err != nil || report != (Report{Marked: 1, Skipped: 2}) {
		t.Errorf("RunOnce = %+v, %v; want 1 marked, 2 skipped", report, err)
	}
}

func TestRunOnce_GoesOnPastAFailureAndCountsIt(t *testing.T) {
	f := newFixture(t)
	a1, a2 := confirmed("a1", cutoff.Add(-2*time.Hour), 1), confirmed("a2", cutoff.Add(-time.Hour), 1)
	f.due.EXPECT().DueNoShows(gomock.Any(), cutoff).Return([]scheduling.Appointment{a1, a2}, nil)
	gomock.InOrder(
		f.marker.EXPECT().MarkNoShow(gomock.Any(), sweeperCommand(a1)).Return(service.Record{}, errors.New("connection reset")),
		f.marker.EXPECT().MarkNoShow(gomock.Any(), sweeperCommand(a2)).Return(service.Record{}, nil),
	)

	report, err := f.service().RunOnce(ctx)

	if err != nil || report != (Report{Marked: 1, Failed: 1}) {
		t.Errorf("RunOnce = %+v, %v; want 1 marked, 1 failed", report, err)
	}
	if !bytes.Contains(f.logs.Bytes(), []byte("a1")) || !bytes.Contains(f.logs.Bytes(), []byte("connection reset")) {
		t.Errorf("log %q does not name the failed appointment and its error", f.logs.String())
	}
}

func TestRunOnce_AFailedQueryIsReturned(t *testing.T) {
	f := newFixture(t)
	f.due.EXPECT().DueNoShows(gomock.Any(), cutoff).Return(nil, errors.New("database unavailable"))

	_, err := f.service().RunOnce(ctx)

	if err == nil {
		t.Error("RunOnce = nil error, want the query's error")
	}
}

func TestWorker_RunsOnItsIntervalUntilItsContextEnds(t *testing.T) {
	svc := NewMockService(gomock.NewController(t))
	ran := make(chan struct{}, 10)
	svc.EXPECT().RunOnce(gomock.Any()).MinTimes(2).DoAndReturn(func(context.Context) (Report, error) {
		ran <- struct{}{}
		return Report{}, nil
	})
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- NewWorker(svc, time.Millisecond).Run(runCtx)() }()

	for i := 0; i < 2; i++ {
		select {
		case <-ran:
		case <-time.After(5 * time.Second):
			t.Fatal("RunOnce was not called on the interval")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return when its context ended")
	}
}
