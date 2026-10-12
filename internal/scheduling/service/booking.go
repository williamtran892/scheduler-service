package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
)

// The booking flow (research R4) is implemented here: checks in order, the booking-key lookups,
// candidate selection and the one-transaction-per-pair loop (implementation notes 4.4).

const (
	maxAlternatives = 5 // contract Problem.alternatives: up to five nearest feasible starts

	actorAnonymous  = "anonymous" // the audit actor until authentication exists
	eventConfirmed  = "CONFIRMED"
	aggregateType   = "Appointment"
	outboxConfirmed = "AppointmentConfirmed"
)

// plan is a booking request on its way through the checks: the dealership and service type it
// resolved to, then the aligned start and the cell runs it needs, then the one appointment id the
// whole request uses (it also seeds the tie shuffle).
type plan struct {
	cmd   BookCommand
	cfg   scheduling.DealershipConfig
	req   scheduling.ServiceRequirement
	start time.Time
	bay   scheduling.CellRun
	tech  scheduling.CellRun
	id    string
}

// Book confirms an appointment, or returns the one that already owns the booking key.
func (s *service) Book(ctx context.Context, cmd BookCommand) (Record, error) {
	p, err := s.resolve(ctx, cmd)
	if err != nil {
		return Record{}, err
	}
	if rec, answered, err := s.existing(ctx, cmd); answered {
		return rec, err
	}

	if p, err = s.validate(p); err != nil {
		return Record{}, err
	}
	if err := s.checkWorkingTime(ctx, p); err != nil {
		return Record{}, err
	}
	p.id = s.ids.New()

	pairs, err := s.candidates(ctx, p)
	if err != nil {
		return Record{}, err
	}
	return s.claim(ctx, p, pairs)
}

// resolve looks up the dealership, service type, customer and vehicle the request names. Each
// must exist, share the dealership's tenant, and be consistent: the service type active, the
// vehicle the customer's. A failure is 422 INVALID_VALUE naming the field.
func (s *service) resolve(ctx context.Context, cmd BookCommand) (plan, error) {
	cfg, err := s.workshop.DealershipConfig(ctx, cmd.DealershipID)
	if err != nil {
		return plan{}, referenceError("dealershipId", err)
	}
	req, err := s.workshop.Requirement(ctx, cmd.ServiceTypeID)
	if err != nil {
		return plan{}, referenceError("serviceTypeId", err)
	}
	if !req.Active || req.TenantID != cfg.TenantID {
		return plan{}, platform.InvalidValue("serviceTypeId", "is not an active service type of this dealership's tenant")
	}
	customer, err := s.customers.Customer(ctx, cmd.CustomerID)
	if err != nil {
		return plan{}, referenceError("customerId", err)
	}
	if customer.TenantID != cfg.TenantID {
		return plan{}, platform.InvalidValue("customerId", "is not a customer of this dealership's tenant")
	}
	vehicle, err := s.customers.Vehicle(ctx, cmd.VehicleID)
	if err != nil {
		return plan{}, referenceError("vehicleId", err)
	}
	if vehicle.TenantID != cfg.TenantID || vehicle.CustomerID != customer.ID {
		return plan{}, platform.InvalidValue("vehicleId", "is not a vehicle of this customer")
	}
	return plan{cmd: cmd, cfg: cfg, req: req}, nil
}

// referenceError answers a failed lookup of a request field: no such row is INVALID_VALUE naming
// the field, anything else is an internal error.
func referenceError(field string, err error) error {
	if errors.Is(err, scheduling.ErrNotFound) {
		return platform.InvalidValue(field, "does not exist")
	}
	return fmt.Errorf("resolve %s: %w", field, err)
}

// existing answers a request whose booking key already has an appointment: that appointment,
// unchanged, when it is the same request (the same dealership and service type, and neither
// cancelled nor a no-show), otherwise DUPLICATE_BOOKING carrying it. answered is false when the
// key is free, or true when rec and err are the outcome of the request (research R2).
func (s *service) existing(ctx context.Context, cmd BookCommand) (rec Record, answered bool, err error) {
	key := scheduling.BookingKey{VehicleID: cmd.VehicleID, CustomerID: cmd.CustomerID, StartAt: cmd.StartAt.UTC(), Kind: cmd.Kind}
	a, found, err := s.store.AppointmentByBookingKey(ctx, key)
	if err != nil {
		return Record{}, true, fmt.Errorf("look up booking key of vehicle %s: %w", cmd.VehicleID, err)
	}
	if !found {
		return Record{}, false, nil
	}

	live := a.Status != scheduling.StatusCancelled && a.Status != scheduling.StatusNoShow
	if live && a.DealershipID == cmd.DealershipID && a.ServiceTypeID == cmd.ServiceTypeID {
		rec, err := s.load(ctx, a)
		return rec, true, err
	}
	return Record{}, true, s.duplicate(ctx, a)
}

// duplicate builds the DUPLICATE_BOOKING error for the appointment that owns the key.
func (s *service) duplicate(ctx context.Context, a scheduling.Appointment) error {
	cfg, err := s.workshop.DealershipConfig(ctx, a.DealershipID)
	if err != nil {
		return fmt.Errorf("duplicate booking of appointment %s: %w", a.ID, err)
	}
	serviceType, err := s.workshop.Requirement(ctx, a.ServiceTypeID)
	if err != nil {
		return fmt.Errorf("duplicate booking of appointment %s: %w", a.ID, err)
	}
	return &scheduling.DuplicateBookingError{Existing: a, ServiceTypeCode: serviceType.Code, Location: cfg.Location}
}

// validate applies the rules that need no capacity read, in order: customer channels may not
// book staff-only services, the start is on the grid, after now and inside the booking horizon.
// It returns the plan with its start and the runs the service type needs.
func (s *service) validate(p plan) (plan, error) {
	if p.cmd.Channel.IsCustomer() && !p.req.OnlineBookable {
		return plan{}, fmt.Errorf("%s: %w", p.req.Code, scheduling.ErrNotBookableOnline)
	}
	start, err := p.cfg.Grid.Align(p.cmd.StartAt.UTC())
	if err != nil {
		return plan{}, fmt.Errorf("%s: %w", p.cfg.Code, err)
	}
	now := s.clock.Now()
	if !start.After(now) {
		return plan{}, platform.InvalidValue("startAt", "must be in the future")
	}
	if start.After(now.Add(time.Duration(p.cfg.BookingHorizonDays) * 24 * time.Hour)) {
		return plan{}, platform.InvalidValue("startAt", fmt.Sprintf("must be within the %d-day booking horizon", p.cfg.BookingHorizonDays))
	}
	p.start = start
	p.bay, p.tech = p.req.Runs(start, p.cfg.Grid)
	return p, nil
}

// checkWorkingTime refuses a booking whose bay run or technician run, buffers included, covers a
// cell the dealership has no ledger cell for: in this release the ledger is working time
// (research R6).
func (s *service) checkWorkingTime(ctx context.Context, p plan) error {
	for _, run := range []scheduling.CellRun{p.bay, p.tech} {
		n, err := s.store.WorkingCells(ctx, p.cfg.DealershipID, run.First, run.End())
		if err != nil {
			return err
		}
		if n < run.Cells {
			return fmt.Errorf("%s is not open for the whole job, %s to %s: %w", p.cfg.Code,
				run.First.In(p.cfg.Location).Format(time.RFC3339), run.End().In(p.cfg.Location).Format(time.RFC3339),
				scheduling.ErrOutsideOpeningHours)
		}
	}
	return nil
}

// candidates returns the (bay, technician) pairs that can take the request's runs, least-loaded
// first. It answers NO_QUALIFIED_TECHNICIAN when nobody active holds the required skills on the
// local date.
func (s *service) candidates(ctx context.Context, p plan) ([]scheduling.Candidate, error) {
	day := p.cfg.Day(p.start)
	bays, techs, err := s.workshop.Resources(ctx, p.cfg.DealershipID, day.LocalDate)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(techs, func(t scheduling.Technician) bool { return t.HasAll(p.req.RequiredSkills) }) {
		return nil, fmt.Errorf("no active technician at %s holds %s on %s: %w", p.cfg.Code,
			strings.Join(p.req.RequiredSkills, ", "), day.LocalDate, scheduling.ErrNoQualifiedTechnician)
	}
	islands, loads, err := s.store.DayLedger(ctx, p.cfg.DealershipID, day)
	if err != nil {
		return nil, err
	}
	return scheduling.CandidatePairs(p.start, p.cfg.Grid, p.req, bays, techs, islands, loads, scheduling.SeedFrom(p.id)), nil
}

// claim tries the candidate pairs in order, one transaction each (notes 4.4). A pair that was
// taken moves on to the next; a deadlock victim is retried once on the same pair; missing cells
// recompute the candidates once and start again; a booking key that already has an appointment
// answers with it.
func (s *service) claim(ctx context.Context, p plan, pairs []scheduling.Candidate) (Record, error) {
	recomputed := false
	for i := 0; i < len(pairs); i++ {
		rec, err := s.attempt(ctx, p, pairs[i])
		if errors.Is(err, scheduling.ErrDeadlockVictim) {
			rec, err = s.attempt(ctx, p, pairs[i])
		}

		switch {
		case err == nil:
			return rec, nil
		case errors.Is(err, scheduling.ErrSlotTaken), errors.Is(err, scheduling.ErrDeadlockVictim):
			// the pair is gone; try the next one
		case errors.Is(err, scheduling.ErrCellsMissing):
			if recomputed {
				return s.availabilityChanged(ctx, p)
			}
			recomputed = true
			if pairs, err = s.candidates(ctx, p); err != nil {
				return Record{}, err
			}
			i = -1 // start again with the recomputed pairs
		case errors.Is(err, scheduling.ErrDuplicateBooking):
			return s.afterDuplicate(ctx, p, err)
		default:
			return Record{}, err
		}
	}
	return s.slotTaken(ctx, p)
}

// attempt is one booking transaction on one pair, in the statement order that is the
// correctness argument: insert the appointment (its booking key is the idempotency guard), claim
// the cells, append the audit event, append the outbox row.
func (s *service) attempt(ctx context.Context, p plan, pair scheduling.Candidate) (Record, error) {
	a := s.newAppointment(p, pair)
	cells := scheduling.OwnedCells{Bay: cellRange(p.bay), Technician: cellRange(p.tech)}
	rec, err := s.record(ctx, a, cells) // everything the snapshots need, read before the transaction
	if err != nil {
		return Record{}, err
	}
	snapshot := newView(rec, true)

	err = s.store.InTx(ctx, func(tx scheduling.Tx) error {
		if err := tx.InsertAppointment(a); err != nil {
			return err
		}
		if err := tx.ClaimPair(p.cfg.DealershipID, pair.BayID, p.bay, pair.TechID, p.tech, a.ID); err != nil {
			return err
		}
		if err := tx.AppendEvent(a.ID, eventConfirmed, actorAnonymous, a.Channel, snapshot); err != nil {
			return err
		}
		return tx.AppendOutbox(aggregateType, a.ID, outboxConfirmed, snapshot)
	})
	if err != nil {
		return Record{}, err
	}
	return rec, nil
}

// newAppointment is the CONFIRMED appointment a booking on pair would create. The exact times
// are the customer's (start plus duration, no buffers); notes sent through a customer channel are
// ignored; the timestamps are whole microseconds, as the database stores them, so the booking
// response equals a later fetch.
func (s *service) newAppointment(p plan, pair scheduling.Candidate) scheduling.Appointment {
	notes := p.cmd.Notes
	if p.cmd.Channel.IsCustomer() {
		notes = ""
	}
	now := s.clock.Now().Truncate(time.Microsecond)
	return scheduling.Appointment{
		ID:               p.id,
		TenantID:         p.cfg.TenantID,
		DealershipID:     p.cfg.DealershipID,
		CustomerID:       p.cmd.CustomerID,
		VehicleID:        p.cmd.VehicleID,
		ServiceTypeID:    p.req.ServiceTypeID,
		Kind:             p.cmd.Kind,
		StartAt:          p.start,
		EndAt:            p.start.Add(p.req.Duration),
		ServiceBayID:     pair.BayID,
		TechnicianID:     pair.TechID,
		Status:           scheduling.StatusConfirmed,
		CustomerRequests: p.cmd.CustomerRequests,
		Notes:            notes,
		Channel:          p.cmd.Channel,
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

// cellRange is the half-open range of cells a run claims.
func cellRange(r scheduling.CellRun) *scheduling.CellRange {
	return &scheduling.CellRange{From: r.First, To: r.End(), Cells: r.Cells}
}

// afterDuplicate answers a 1062 on the booking key: the appointment that owns it, which a
// concurrent identical request committed first.
func (s *service) afterDuplicate(ctx context.Context, p plan, dup error) (Record, error) {
	if rec, answered, err := s.existing(ctx, p.cmd); answered {
		return rec, err
	}
	// Appointments are never deleted, so the owner must exist. %v, not %w: this is an internal
	// error, not a DUPLICATE_BOOKING answer without the appointment it should carry.
	return Record{}, fmt.Errorf("booking key reported as duplicate but no appointment owns it: %v", dup)
}

// slotTaken ends a booking that found no free pair with 409 SLOT_TAKEN and alternatives, unless
// an identical concurrent request took the pair for itself, in which case that appointment is
// the answer.
func (s *service) slotTaken(ctx context.Context, p plan) (Record, error) {
	if rec, answered, err := s.existing(ctx, p.cmd); answered {
		return rec, err
	}
	alternatives, err := s.alternatives(ctx, p)
	if err != nil {
		return Record{}, err
	}
	return Record{}, &scheduling.SlotTakenError{Alternatives: alternatives, Location: p.cfg.Location}
}

// availabilityChanged ends a booking whose cells kept disappearing with 409
// AVAILABILITY_CHANGED, with the same re-check of the key as slotTaken.
func (s *service) availabilityChanged(ctx context.Context, p plan) (Record, error) {
	if rec, answered, err := s.existing(ctx, p.cmd); answered {
		return rec, err
	}
	alternatives, err := s.alternatives(ctx, p)
	if err != nil {
		return Record{}, err
	}
	return Record{}, &scheduling.AvailabilityChangedError{Alternatives: alternatives, Location: p.cfg.Location}
}

// alternatives returns the nearest feasible starts for the same service type, freshly computed
// over the requested local date and the next one (research R7).
func (s *service) alternatives(ctx context.Context, p plan) ([]scheduling.Slot, error) {
	first := p.cfg.Day(p.start)
	var starts []time.Time
	for _, day := range []scheduling.DayWindow{first, p.cfg.Day(first.To)} {
		bays, techs, err := s.workshop.Resources(ctx, p.cfg.DealershipID, day.LocalDate)
		if err != nil {
			return nil, err
		}
		islands, _, err := s.store.DayLedger(ctx, p.cfg.DealershipID, day)
		if err != nil {
			return nil, err
		}
		starts = append(starts, scheduling.FeasibleStarts(day, p.cfg.Grid, p.req, bays, techs, islands)...)
	}

	nearest := scheduling.NearestAlternatives(starts, p.start, s.clock.Now(), maxAlternatives)
	slots := make([]scheduling.Slot, len(nearest))
	for i, start := range nearest {
		slots[i] = scheduling.Slot{StartAt: start, EndAt: start.Add(p.req.Duration)}
	}
	return slots, nil
}
