package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"scheduler/internal/scheduling"
)

// Reschedule moves a CONFIRMED appointment, reusing booking's plan with the appointment's own
// cells counting as free (design 5.4). The new start is startAt, or the
// current start for a pure reassign; a named bay or technician restricts the candidates. The
// appointment's own pair is tried first, with the named resources put in its place.
func (s *service) Reschedule(ctx context.Context, cmd RescheduleCommand) (Record, error) {
	if cmd.TechnicianID != "" || cmd.ServiceBayID != "" {
		if err := staffOnly(cmd.Channel, "naming the bay or the technician"); err != nil {
			return Record{}, err
		}
	}
	a, err := s.loadForTransition(ctx, cmd.TransitionCommand, scheduling.StatusConfirmed)
	if err != nil {
		return Record{}, err
	}

	p, err := s.movePlan(ctx, a, cmd.Channel)
	if err != nil {
		return Record{}, err
	}
	p.requested = a.StartAt
	if cmd.StartAt != nil {
		p.requested = *cmd.StartAt
	}
	p.requiredBayID, p.requiredTechnicianID = cmd.ServiceBayID, cmd.TechnicianID
	if p.requiredBayID != "" {
		p.preferredPair.BayID = p.requiredBayID
	}
	if p.requiredTechnicianID != "" {
		p.preferredPair.TechID = p.requiredTechnicianID
	}

	if p, err = s.validate(p); err != nil {
		return Record{}, err
	}
	if err := s.checkWorkingTime(ctx, p); err != nil {
		return Record{}, err
	}
	pairs, err := s.candidates(ctx, &p)
	if err != nil {
		return Record{}, err
	}
	if err := s.loadPeople(ctx, &p, a); err != nil {
		return Record{}, err
	}

	rec, err := s.claim(ctx, &p, pairs, func(pair scheduling.Candidate) (Record, error) {
		next := s.next(a, scheduling.StatusConfirmed)
		next.StartAt, next.EndAt = p.start, p.start.Add(p.req.Duration)
		next.ServiceBayID, next.TechnicianID = pair.BayID, pair.TechID
		cells := scheduling.OwnedCells{Bay: cellRange(p.bay), Technician: cellRange(p.tech)}
		return s.commit(ctx, cmd.TransitionCommand, a, next, change{
			event:   eventRescheduled,
			outbox:  outboxRescheduled,
			payload: eventPayload{Reason: cmd.Reason, Previous: placementOf(a), New: placementOf(next)},
			claim:   &newRuns{bayID: pair.BayID, techID: pair.TechID, bay: p.bay, tech: p.tech},
			releaseExcept: &scheduling.Keep{
				Bay:        &scheduling.ResourceRun{ResourceID: pair.BayID, Run: p.bay},
				Technician: &scheduling.ResourceRun{ResourceID: pair.TechID, Run: p.tech},
			},
			resultingCells: cells,
		}, p.record(next, cells))
	})
	switch {
	case errors.Is(err, errNoPair):
		return Record{}, &scheduling.SlotTakenError{}
	case errors.Is(err, errCellsGone):
		return Record{}, &scheduling.AvailabilityChangedError{}
	case errors.Is(err, scheduling.ErrDuplicateBooking):
		return Record{}, s.keyOwner(ctx, a, p.start)
	}
	return rec, err
}

// lateArrival checks a NO_SHOW appointment in on the cells that are free now (design 4.3): the
// new start is the grid cell that contains now, the runs follow from the service type as in
// booking, and the previous pair is tried first, then the least-loaded. The booking horizon and
// "after now" do not apply. Every way of finding no pair, whether the cells are taken or missing
// or nobody is qualified, is SLOT_TAKEN, and the appointment stays NO_SHOW.
func (s *service) lateArrival(ctx context.Context, cmd TransitionCommand, a scheduling.Appointment, payload eventPayload) (Record, error) {
	p, err := s.movePlan(ctx, a, cmd.Channel)
	if err != nil {
		return Record{}, err
	}
	p.start = p.cfg.Grid.Floor(s.clock.Now())
	p.bay, p.tech = p.req.Runs(p.start, p.cfg.Grid)

	pairs, err := s.candidates(ctx, &p)
	if errors.Is(err, scheduling.ErrNoQualifiedTechnician) {
		return Record{}, &scheduling.SlotTakenError{}
	}
	if err != nil {
		return Record{}, err
	}
	if err := s.loadPeople(ctx, &p, a); err != nil {
		return Record{}, err
	}

	rec, err := s.claim(ctx, &p, pairs, func(pair scheduling.Candidate) (Record, error) {
		next := s.next(a, scheduling.StatusCheckedIn)
		next.StartAt, next.EndAt = p.start, p.start.Add(p.req.Duration)
		next.ServiceBayID, next.TechnicianID = pair.BayID, pair.TechID
		payload := payload
		payload.LateArrival, payload.Previous, payload.New = true, placementOf(a), placementOf(next)
		cells := scheduling.OwnedCells{Bay: cellRange(p.bay), Technician: cellRange(p.tech)}
		return s.commit(ctx, cmd, a, next, change{
			event:          eventCheckedIn,
			outbox:         outboxCheckedIn,
			payload:        payload,
			claim:          &newRuns{bayID: pair.BayID, techID: pair.TechID, bay: p.bay, tech: p.tech},
			resultingCells: cells,
		}, p.record(next, cells))
	})
	switch {
	case errors.Is(err, errNoPair), errors.Is(err, errCellsGone), errors.Is(err, scheduling.ErrNoQualifiedTechnician):
		return Record{}, &scheduling.SlotTakenError{}
	case errors.Is(err, scheduling.ErrDuplicateBooking):
		return Record{}, s.keyOwner(ctx, a, p.start)
	}
	return rec, err
}

// movePlan is the plan for moving an existing appointment: its dealership and service type, its id
// as the existing appointment whose cells count as free and as the shuffle seed, and its current
// pair as the preferred one.
func (s *service) movePlan(ctx context.Context, a scheduling.Appointment, channel scheduling.Channel) (plan, error) {
	cfg, err := s.workshop.DealershipConfig(ctx, a.DealershipID)
	if err != nil {
		return plan{}, fmt.Errorf("move appointment %s: %w", a.ID, err)
	}
	req, err := s.workshop.Requirement(ctx, a.ServiceTypeID)
	if err != nil {
		return plan{}, fmt.Errorf("move appointment %s: %w", a.ID, err)
	}
	return plan{
		cfg: cfg, req: req, channel: channel,
		appointmentID:         a.ID,
		existingAppointmentID: a.ID,
		preferredPair:         scheduling.Candidate{BayID: a.ServiceBayID, TechID: a.TechnicianID},
	}, nil
}

// loadPeople reads the customer and the vehicle of the appointment being moved into the plan, once
// the candidates are known and before the first attempt, so that no attempt reads anything to
// build its record. movePlan does not read them: a move refused by a check never needs them.
func (s *service) loadPeople(ctx context.Context, p *plan, a scheduling.Appointment) error {
	var err error
	if p.customer, err = s.customers.Customer(ctx, a.CustomerID); err != nil {
		return fmt.Errorf("record of appointment %s: %w", a.ID, err)
	}
	if p.vehicle, err = s.customers.Vehicle(ctx, a.VehicleID); err != nil {
		return fmt.Errorf("record of appointment %s: %w", a.ID, err)
	}
	return nil
}

// keyOwner answers a 1062 on the booking key of a move: DUPLICATE_BOOKING carrying the
// appointment that owns the key the appointment would get at start. The key covers every status.
func (s *service) keyOwner(ctx context.Context, a scheduling.Appointment, start time.Time) error {
	key := scheduling.BookingKey{VehicleID: a.VehicleID, CustomerID: a.CustomerID, StartAt: start, Kind: a.Kind}
	owner, found, err := s.store.AppointmentByBookingKey(ctx, key)
	if err != nil {
		return fmt.Errorf("look up booking key of vehicle %s: %w", a.VehicleID, err)
	}
	if !found {
		return fmt.Errorf("booking key of appointment %s reported as duplicate but no appointment owns it", a.ID)
	}
	return s.duplicate(ctx, owner)
}
