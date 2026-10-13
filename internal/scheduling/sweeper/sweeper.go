// Package sweeper is the no-show sweeper (design 4.3; notes 4.7; research R7): a worker that,
// on an interval, marks every CONFIRMED appointment whose start plus the grace period has passed
// as NO_SHOW, through the Scheduling service's own MarkNoShow, one appointment per transaction.
package sweeper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kit/log"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
	"scheduler/internal/scheduling/service"
)

//go:generate go tool mockgen -destination=mock_sweeper_test.go -package=sweeper -self_package=scheduler/internal/scheduling/sweeper scheduler/internal/scheduling/sweeper Appointments,Marker,Service

// Actor is the audit actor of a no-show the sweeper marks.
const Actor = "no-show-sweeper"

// Appointments lists the appointments that are due; scheduling.Store implements it.
type Appointments interface {
	DueNoShows(ctx context.Context, before time.Time) ([]scheduling.Appointment, error)
}

// Marker applies the no-show transition; service.Service implements it.
type Marker interface {
	MarkNoShow(ctx context.Context, cmd service.TransitionCommand) (service.Record, error)
}

// Report is what one run did: appointments marked, skipped because another actor changed them
// first, and failed.
type Report struct {
	Marked, Skipped, Failed int
}

// Service runs the sweep once.
type Service interface {
	RunOnce(ctx context.Context) (Report, error)
}

// NewService builds the sweeper over the due-appointment query, the no-show transition, the
// clock and the grace period. Failures are logged on logger: a run that cannot list the
// appointments, and each appointment it could not mark.
func NewService(appointments Appointments, marker Marker, clock platform.Clock, grace time.Duration, logger log.Logger) Service {
	return &sweeper{appointments: appointments, marker: marker, clock: clock, grace: grace, logger: logger}
}

type sweeper struct {
	appointments Appointments
	marker       Marker
	clock        platform.Clock
	grace        time.Duration
	logger       log.Logger
}

// RunOnce marks NO_SHOW every CONFIRMED appointment whose start plus the grace period is strictly
// before now, oldest first, each through the service's own MarkNoShow so it is one transaction
// per appointment (spec FR-051). An appointment that another actor changed first (a version
// conflict, or a status that no longer allows the transition) is skipped; any other failure is
// logged and counted, and the run goes on (spec FR-052).
func (w *sweeper) RunOnce(ctx context.Context) (Report, error) {
	cutoff := w.clock.Now().Add(-w.grace)
	due, err := w.appointments.DueNoShows(ctx, cutoff)
	if err != nil {
		_ = w.logger.Log("msg", "list due no-shows", "err", err)
		return Report{}, fmt.Errorf("list due no-shows: %w", err)
	}

	var report Report
	for _, a := range due {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if a.Status != scheduling.StatusConfirmed || !a.StartAt.Before(cutoff) {
			report.Skipped++
			continue
		}
		_, err := w.marker.MarkNoShow(ctx, service.TransitionCommand{AppointmentID: a.ID, ExpectedVersion: a.Version, Actor: Actor})
		switch {
		case err == nil:
			report.Marked++
		case errors.Is(err, scheduling.ErrStaleVersion), errors.Is(err, scheduling.ErrInvalidTransition):
			report.Skipped++
		default:
			report.Failed++
			_ = w.logger.Log("msg", "mark no-show", "appointment", a.ID, "err", err)
		}
	}
	return report, nil
}

// Worker runs a Service on a ticker until its context ends.
type Worker struct {
	svc   Service
	every time.Duration
}

// NewWorker returns a worker that runs svc every interval.
func NewWorker(svc Service, every time.Duration) Worker {
	return Worker{svc: svc, every: every}
}

// Run returns the function an errgroup runs: RunOnce on every tick, until ctx ends.
func (w Worker) Run(ctx context.Context) func() error {
	return func() error {
		ticker := time.NewTicker(w.every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				_, _ = w.svc.RunOnce(ctx) // RunOnce logs its own failures
			}
		}
	}
}
