package service

import (
	"context"
	"fmt"
	"time"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
)

// The lifecycle transitions after booking (design 4.3; research R1, R5, R6, R8): one transaction
// each, the version-guarded update of the appointment first, then the ledger change, then the
// audit event and the outbox row. The checks run in spec FR-003's order: the channel, the
// appointment, the version, the state machine, then the operation's own rules.

// Audit event types and outbox event types of each transition (research R11).
const (
	eventRescheduled = "RESCHEDULED"
	eventCancelled   = "CANCELLED"
	eventCheckedIn   = "CHECKED_IN"
	eventStarted     = "STARTED"
	eventCompleted   = "COMPLETED"
	eventNoShow      = "NO_SHOW"

	outboxRescheduled = "AppointmentRescheduled"
	outboxCancelled   = "AppointmentCancelled"
	outboxCheckedIn   = "AppointmentCheckedIn"
	outboxStarted     = "AppointmentStarted"
	outboxCompleted   = "AppointmentCompleted"
	outboxNoShow      = "AppointmentNoShow"
)

// eventPayload is the audit payload of every transition. Each transition fills the members
// research R11 lists for it; the others stay out of the JSON.
type eventPayload struct {
	Reason             string     `json:"reason,omitempty"`
	PreviousStatus     string     `json:"previousStatus,omitempty"`
	Mileage            *int       `json:"mileage,omitempty"`
	AdditionalConcerns string     `json:"additionalConcerns,omitempty"`
	LateArrival        bool       `json:"lateArrival,omitempty"`
	Previous           *placement `json:"previous,omitempty"`
	New                *placement `json:"new,omitempty"`
	ActualStartAt      *time.Time `json:"actualStartAt,omitempty"`
	ActualEndAt        *time.Time `json:"actualEndAt,omitempty"`
	CellsReleased      *int       `json:"cellsReleased,omitempty"`
}

// placement is where and when an appointment is, as a reschedule or a late arrival records its
// previous and new state.
type placement struct {
	StartAt      time.Time `json:"startAt"`
	EndAt        time.Time `json:"endAt"`
	ServiceBayID string    `json:"serviceBayId"`
	TechnicianID string    `json:"technicianId"`
}

func placementOf(a scheduling.Appointment) *placement {
	return &placement{StartAt: a.StartAt.UTC(), EndAt: a.EndAt.UTC(), ServiceBayID: a.ServiceBayID, TechnicianID: a.TechnicianID}
}

// newRuns are the cells a reschedule or a late arrival claims.
type newRuns struct {
	bayID, techID string
	bay, tech     scheduling.CellRun
}

// change is what one transition does besides updating the appointment: the cells it claims, the
// cells it releases, the audit event and outbox row it appends, and the cells the appointment
// owns afterwards, which the outbox snapshot and the response show.
type change struct {
	event, outbox string
	payload       eventPayload
	claim         *newRuns
	release       *scheduling.Keep
	countReleased bool // the event reports how many cells the release freed
	cells         scheduling.OwnedCells
}

// Cancel releases every cell of a CONFIRMED or CHECKED_IN appointment; a CANCELLED one is
// returned unchanged, whatever expectedVersion says (spec FR-021).
func (s *service) Cancel(ctx context.Context, cmd TransitionCommand) (Record, error) {
	a, err := s.store.Appointment(ctx, cmd.AppointmentID)
	if err != nil {
		return Record{}, err
	}
	if a.Status == scheduling.StatusCancelled {
		return s.load(ctx, a)
	}
	if err := checkTransition(a, cmd, scheduling.StatusCancelled); err != nil {
		return Record{}, err
	}
	return s.apply(ctx, cmd, a, s.next(a, scheduling.StatusCancelled), change{
		event:   eventCancelled,
		outbox:  outboxCancelled,
		payload: eventPayload{Reason: cmd.Reason, PreviousStatus: string(a.Status)},
		release: &scheduling.Keep{},
	})
}

// MarkNoShow marks a CONFIRMED appointment NO_SHOW and releases every cell. Staff and the
// no-show sweeper use it alike.
func (s *service) MarkNoShow(ctx context.Context, cmd TransitionCommand) (Record, error) {
	if cmd.Channel.IsCustomer() {
		return Record{}, fmt.Errorf("no-show is a staff action: %w", scheduling.ErrForbidden)
	}
	a, err := s.begin(ctx, cmd, scheduling.StatusNoShow)
	if err != nil {
		return Record{}, err
	}
	return s.apply(ctx, cmd, a, s.next(a, scheduling.StatusNoShow), change{
		event:         eventNoShow,
		outbox:        outboxNoShow,
		payload:       eventPayload{Reason: cmd.Reason},
		release:       &scheduling.Keep{},
		countReleased: true,
	})
}

// CheckIn checks a CONFIRMED appointment in and leaves the ledger alone, or checks a NO_SHOW
// appointment in as a late arrival (spec FR-030, FR-031).
func (s *service) CheckIn(ctx context.Context, cmd CheckInCommand) (Record, error) {
	if cmd.Channel.IsCustomer() {
		return Record{}, fmt.Errorf("check-in is a staff action: %w", scheduling.ErrForbidden)
	}
	a, err := s.begin(ctx, cmd.TransitionCommand, scheduling.StatusCheckedIn)
	if err != nil {
		return Record{}, err
	}
	payload := eventPayload{Reason: cmd.Reason, Mileage: cmd.Mileage, AdditionalConcerns: cmd.AdditionalConcerns}
	if a.Status == scheduling.StatusNoShow {
		return s.lateArrival(ctx, cmd.TransitionCommand, a, payload)
	}

	cells, err := s.store.OwnedCells(ctx, a.ID)
	if err != nil {
		return Record{}, fmt.Errorf("read cells of appointment %s: %w", a.ID, err)
	}
	return s.apply(ctx, cmd.TransitionCommand, a, s.next(a, scheduling.StatusCheckedIn), change{
		event:   eventCheckedIn,
		outbox:  outboxCheckedIn,
		payload: payload,
		cells:   cells,
	})
}

// Start records the actual start of a CHECKED_IN appointment: startedAt when the dealer system
// sent one, otherwise now. It leaves the ledger alone (spec FR-040).
func (s *service) Start(ctx context.Context, cmd StartCommand) (Record, error) {
	if cmd.Channel.IsCustomer() {
		return Record{}, fmt.Errorf("start is a staff action: %w", scheduling.ErrForbidden)
	}
	a, err := s.begin(ctx, cmd.TransitionCommand, scheduling.StatusInProgress)
	if err != nil {
		return Record{}, err
	}
	now := s.clock.Now()
	startedAt := now
	if cmd.StartedAt != nil {
		if cmd.StartedAt.After(now) {
			return Record{}, platform.InvalidValue("startedAt", "must not be in the future")
		}
		startedAt = *cmd.StartedAt
	}
	startedAt = startedAt.UTC().Truncate(time.Microsecond)

	cells, err := s.store.OwnedCells(ctx, a.ID)
	if err != nil {
		return Record{}, fmt.Errorf("read cells of appointment %s: %w", a.ID, err)
	}
	next := s.next(a, scheduling.StatusInProgress)
	next.ActualStartAt = &startedAt
	return s.apply(ctx, cmd.TransitionCommand, a, next, change{
		event:   eventStarted,
		outbox:  outboxStarted,
		payload: eventPayload{Reason: cmd.Reason, ActualStartAt: &startedAt},
		cells:   cells,
	})
}

// Complete records the actual end of an IN_PROGRESS appointment, completedAt when the dealer
// system sent one, otherwise now, and releases the cells that start at or after the current grid
// cell rounded up. Which cells go depends on now, never on completedAt (spec FR-041).
func (s *service) Complete(ctx context.Context, cmd CompleteCommand) (Record, error) {
	if cmd.Channel.IsCustomer() {
		return Record{}, fmt.Errorf("complete is a staff action: %w", scheduling.ErrForbidden)
	}
	a, err := s.begin(ctx, cmd.TransitionCommand, scheduling.StatusCompleted)
	if err != nil {
		return Record{}, err
	}
	now := s.clock.Now()
	completedAt := now
	if cmd.CompletedAt != nil {
		if cmd.CompletedAt.After(now) {
			return Record{}, platform.InvalidValue("completedAt", "must not be in the future")
		}
		if a.ActualStartAt != nil && cmd.CompletedAt.Before(*a.ActualStartAt) {
			return Record{}, platform.InvalidValue("completedAt", "must not be before the actual start")
		}
		completedAt = *cmd.CompletedAt
	}
	completedAt = completedAt.UTC().Truncate(time.Microsecond)

	cfg, err := s.workshop.DealershipConfig(ctx, a.DealershipID)
	if err != nil {
		return Record{}, fmt.Errorf("complete appointment %s: %w", a.ID, err)
	}
	owned, err := s.store.OwnedCells(ctx, a.ID)
	if err != nil {
		return Record{}, fmt.Errorf("read cells of appointment %s: %w", a.ID, err)
	}
	before := cfg.Grid.Ceil(now)
	next := s.next(a, scheduling.StatusCompleted)
	next.ActualEndAt = &completedAt
	return s.apply(ctx, cmd.TransitionCommand, a, next, change{
		event:         eventCompleted,
		outbox:        outboxCompleted,
		payload:       eventPayload{Reason: cmd.Reason, ActualEndAt: &completedAt},
		release:       &scheduling.Keep{Before: before},
		countReleased: true,
		cells: scheduling.OwnedCells{
			Bay:        cellsBefore(owned.Bay, before, cfg.Grid.Slot()),
			Technician: cellsBefore(owned.Technician, before, cfg.Grid.Slot()),
		},
	})
}

// cellsBefore is the part of an owned range made of the cells that start before before.
func cellsBefore(r *scheduling.CellRange, before time.Time, slot time.Duration) *scheduling.CellRange {
	if r == nil || !r.From.Before(before) {
		return nil
	}
	to := r.To
	if before.Before(to) {
		to = before
	}
	return &scheduling.CellRange{From: r.From, To: to, Cells: int(to.Sub(r.From) / slot)}
}

// begin loads the appointment and runs the checks every transition shares, in FR-003's order:
// the appointment exists (scheduling.ErrNotFound), expectedVersion is its version, and the state
// machine allows moving to to.
func (s *service) begin(ctx context.Context, cmd TransitionCommand, to scheduling.AppointmentStatus) (scheduling.Appointment, error) {
	a, err := s.store.Appointment(ctx, cmd.AppointmentID)
	if err != nil {
		return scheduling.Appointment{}, err
	}
	return a, checkTransition(a, cmd, to)
}

// checkTransition refuses a stale expectedVersion, naming both versions, before a status the
// state machine does not allow: a stale caller's idea of the status is unreliable (spec FR-003).
func checkTransition(a scheduling.Appointment, cmd TransitionCommand, to scheduling.AppointmentStatus) error {
	if cmd.ExpectedVersion != a.Version {
		return &scheduling.VersionConflictError{Current: a.Version, Sent: cmd.ExpectedVersion}
	}
	if !a.Status.CanTransitionTo(to) {
		return fmt.Errorf("appointment %s is %s and cannot become %s: %w", a.ID, a.Status, to, scheduling.ErrInvalidTransition)
	}
	return nil
}

// next is the appointment after a transition to status: the next version, updated now (whole
// microseconds, as the database stores them).
func (s *service) next(a scheduling.Appointment, status scheduling.AppointmentStatus) scheduling.Appointment {
	a.Status = status
	a.Version++
	a.UpdatedAt = s.clock.Now().Truncate(time.Microsecond)
	return a
}

// apply runs one transition in one transaction, in the statement order that is the correctness
// argument (research R1): the version-guarded update of the appointment, the claim, the release,
// the audit event, the outbox row. It returns the record after the transition, which is also what
// the outbox row holds. The record is read before the transaction, like booking's.
func (s *service) apply(ctx context.Context, cmd TransitionCommand, a, next scheduling.Appointment, ch change) (Record, error) {
	rec, err := s.record(ctx, next, ch.cells)
	if err != nil {
		return Record{}, err
	}
	snapshot := newView(rec, true)
	actor := cmd.Actor
	if actor == "" {
		actor = actorAnonymous
	}

	err = s.store.InTx(ctx, func(tx scheduling.Tx) error {
		if err := tx.UpdateAppointment(next, a.Version); err != nil {
			return err
		}
		if c := ch.claim; c != nil {
			if err := tx.ClaimPair(a.DealershipID, c.bayID, c.bay, c.techID, c.tech, a.ID); err != nil {
				return err
			}
		}
		payload := ch.payload
		if ch.release != nil {
			released, err := tx.ReleaseCells(a.ID, *ch.release)
			if err != nil {
				return err
			}
			if ch.countReleased {
				payload.CellsReleased = &released
			}
		}
		if err := tx.AppendEvent(a.ID, ch.event, actor, cmd.Channel, payload); err != nil {
			return err
		}
		return tx.AppendOutbox(aggregateType, a.ID, ch.outbox, snapshot)
	})
	if err != nil {
		return Record{}, err
	}
	return rec, nil
}
