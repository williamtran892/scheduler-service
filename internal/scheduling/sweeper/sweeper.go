// Package sweeper is the no-show sweeper (design 4.3; notes 4.7; research R7): a worker that,
// on an interval, marks every CONFIRMED appointment whose start plus the grace period has passed
// as NO_SHOW, through the Scheduling service's own MarkNoShow, one appointment per transaction.
package sweeper

import (
	"context"
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
// clock and the grace period. Not implemented yet.
func NewService(appointments Appointments, marker Marker, clock platform.Clock, grace time.Duration, logger log.Logger) Service {
	return stub{}
}

type stub struct{}

func (stub) RunOnce(context.Context) (Report, error) { panic("not implemented") }

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
	return func() error { panic("not implemented") }
}
