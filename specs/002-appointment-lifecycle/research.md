# Research: Appointment lifecycle

Phase 0 decisions for [plan.md](./plan.md). Each one resolves a point the spec, the design
(`docs/design/SCHEDULING_DESIGN.md`, "design N.N"), the implementation notes
(`docs/design/IMPLEMENTATION_NOTES.md`, "notes N.N"), the contract (`api/openapi.yaml`) and the
implemented feature 001 (`docs/architecture.md`) leave open or disagree on. The Technical Context
had no NEEDS CLARIFICATION left after the spec's Clarifications; these are the design choices
behind it.

## R1. One transaction per transition, version-guarded update first

**Decision**: every transition runs in one `Store.InTx` with the statements in this order:

1. `Tx.UpdateAppointment(next, expectedVersion)`: `UPDATE appointment SET ... , version = version + 1,
   updated_at = ? WHERE id = ? AND version = ?`. Zero rows is `scheduling.ErrStaleVersion`
   (409 `VERSION_CONFLICT`); MySQL 1062 on `uq_appointment_booking` (a reschedule or late arrival
   whose new start is another appointment's booking key) is `scheduling.ErrDuplicateBooking`.
2. The ledger change, if the state machine has one: `Tx.ClaimPair` (reschedule, late arrival),
   then `Tx.ReleaseCells` (reschedule, cancel, no-show, complete).
3. `Tx.AppendEvent`, then `Tx.AppendOutbox`.

**Rationale**: this is booking's shape (the appointment row is written first, notes 4.4) and the
notes' shape for the other transitions ("`UpdateAppointment` with the expected `version`, then
`ReleaseCells` ..., then event and outbox, all in one `InTx`", notes 4.4). Writing the appointment
row first locks it, so two transitions of one appointment (including the sweeper racing staff)
serialise on that row and the loser fails the version check before it touches a single cell. It
also makes every transaction take locks in the same order: appointment row, bay cells, technician
cells, cells by appointment.

**Alternatives considered**: design 11.4's order for reschedule (claim, release, then the
version-guarded update). Rejected: a stale reschedule would lock and rewrite cells before
discovering it is stale, and two concurrent reschedules of one appointment could deadlock on the
cells instead of failing cleanly on the row. The guarantee (nothing changes unless all of it does)
is the same either way, because it is one transaction. Recorded as a conflict between design 11.4
and notes 4.4 in plan.md.

## R2. The claim's exclusion is the claiming appointment

**Decision**: `Tx.ClaimPair` keeps its signature. A cell already `BOOKED` by the appointment the
claim is for counts as claimable; any other `BOOKED` cell is `SLOT_TAKEN`. The `UPDATE` then sets
every cell of the run to `BOOKED` for that appointment (re-setting its own cells is harmless:
`version = version + 1` already forces a change, design 11.3's gotcha does not arise because the
claim reads rows rather than counting affected rows, notes 4.5).

**Rationale**: design 11.4 calls the procedure with `:appt_id, :appt_id`: the excluded appointment
is always the claimer. For a booking no cell can be owned by an id generated a moment ago, so the
rule changes nothing for feature 001. One `if` in `claimRun` instead of a parameter threaded
through the interface, the mocks and every call site.

**Alternatives considered**: notes 4.5's `excludeApptID` parameter. Rejected as a parameter that
would always equal another parameter (Principle III).

## R3. `ReleaseCells`: what to keep, matched on resource and time

**Decision**: one new `Tx` method in `internal/scheduling/mysql/ledger.go`:
`ReleaseCells(appointmentID string, keep scheduling.Keep) (int, error)` where `Keep` holds the bay
run and technician run to keep, each with its resource id (reschedule, late arrival: none), and
`Before time.Time` (complete: `grid.Ceil(now)`; zero: none). It locks the appointment's cells with
`SELECT ... FROM time_slot WHERE appointment_id = ? ORDER BY resource_kind, resource_id,
slot_start FOR UPDATE`, filters in Go (a cell is kept if it lies inside a kept run on the same
resource kind and id, or starts before `Before`), and frees the rest with one
`UPDATE time_slot SET status = 'FREE', appointment_id = NULL, version = version + 1 WHERE
appointment_id = ? AND resource_kind = ? AND resource_id = ? AND slot_start IN (...)` per resource.
It returns the number of cells freed (for the log line and the sweeper report).

**Rationale**: notes 4.5 describe exactly this shape; matching on resource id as well as time is
what makes a reassign free the old technician (design 5.4, 11.3 row "Reassign to another
technician, same window"). `ClaimPair` and `ReleaseCells` in `ledger.go` stay the only writers of
`time_slot`, so `ledger_guard_test.go` keeps passing unchanged.

**Alternatives considered**: a separate `ReleaseAll` for cancel and no-show. Rejected: the zero
`Keep` is the same statement.

## R4. Reschedule reuses booking's plan, with an owner and pins

**Decision**: the booking flow's `plan` (the dealership, the requirement, the start, the runs)
gains three fields: `owner` (the appointment being moved, whose cells count as free), `pinBay` and
`pinTech`. Reschedule builds the plan from the stored appointment and the request, then runs
booking's own steps: `validate` (online flag for customer channels, grid, after now, horizon),
`checkWorkingTime`, `candidates`, and the claim loop. Differences:

- `Store.DayLedger(ctx, dealershipID, day, owner string)` treats cells booked by `owner` as free in
  the islands and leaves them out of the loads; booking passes `""`.
- `candidates` keeps only pairs on the pinned bay and technician. Pins are validated first against
  `WorkshopReader.Resources` for the new local date: a pinned bay must be among the dealership's
  ACTIVE bays and of an allowed type (`INVALID_VALUE` on `serviceBayId`); a pinned technician must
  be among its ACTIVE technicians (`INVALID_VALUE` on `technicianId`) and hold every required skill
  (`NO_QUALIFIED_TECHNICIAN`). No change to `WorkshopReader`.
- The current pair, if it is a candidate, is moved to the front; the rest keep booking's order
  (least loaded, seeded shuffle with `SeedFrom(appointment id)`).
- The attempt is R1's transaction (update, claim, release outside the new runs, event, outbox)
  instead of booking's insert. A taken pair moves on, a deadlock victim is retried once, missing
  cells recompute once then `AVAILABILITY_CHANGED`, `ErrDuplicateBooking` loads the key's owner and
  answers `DUPLICATE_BOOKING` with it, `ErrStaleVersion` answers `VERSION_CONFLICT`.
- Alternatives on `SLOT_TAKEN` and `AVAILABILITY_CHANGED` use the same `alternatives` function,
  which passes the owner to `DayLedger` and the pins to `FeasibleStarts` through the filtered
  resource lists.

**Rationale**: decision 1 ("validates the chosen bay type and technician skills against the
service type like booking does") and design 5.4 ("the server re-validates skills, bay type and
overlap on every move"). Reusing the plan keeps one implementation of every booking rule.

**Alternatives considered**: a separate `reschedule.go` flow with its own checks. Rejected: a
second copy of the grid, horizon, working-time and candidate rules would drift.

## R5. Late arrival is a reschedule to the current grid cell, inside the check-in

**Decision**: check-in of a `NO_SHOW` appointment builds the same plan as R4 with
`start = grid.Floor(now)`, no pins, the previous pair moved to the front of the candidates, and
skips `validate` (no horizon, no "after now"; the online flag does not apply because check-in is a
staff action). The transaction is R1's with status `CHECKED_IN` and no release (a no-show owns no
cells). Every way of ending without a pair (no candidate, cells missing twice, no qualified
technician) is answered with `SlotTakenError` and alternatives from the same `alternatives`
function, computed for the current grid cell. Event `CHECKED_IN` with `lateArrival: true` and the
previous and new times and resources; outbox `AppointmentCheckedIn`.

**Rationale**: design 4.3 ("a reschedule into whatever run is free now, same transaction as the
check-in"), contract `checkInAppointment` ("starting at the current grid cell ... If nothing is
free the response is `409 SLOT_TAKEN`"). The grid gains `Floor` and `Ceil` (notes 4.5 already
names `grid.Ceil`).

**Alternatives considered**: start at the next grid cell (`Ceil`). Rejected: the contract says
"current". Answering `NO_QUALIFIED_TECHNICIAN` or `AVAILABILITY_CHANGED` for a late arrival:
rejected, the contract gives check-in only `409` and names `SLOT_TAKEN`.

## R6. Start and complete: actual times, and what complete releases

**Decision**: start sets `actual_start_at` to `startedAt` or `clock.Now()`, truncated to the
microsecond; complete sets `actual_end_at` likewise and calls `ReleaseCells` with
`Keep{Before: grid.Ceil(now)}`. `startedAt`/`completedAt` after now, or `completedAt` before the
actual start, is `INVALID_VALUE` on the field. `actualStartAt` and `actualEndAt` render in the
dealership's time zone. `Appointment` gains `ActualStartAt, ActualEndAt *time.Time`; the view
stops hard-coding `null`.

**Rationale**: design 4.3 and 11.4 "Complete" (`keep_before` set to now rounded up to the grid);
design 11.3 row "Complete at 09:40 on a 09:00 to 10:00 job: 6 past cells kept, 2 future cells
FREE" is exactly `Ceil(09:40) = 09:45`. The contract's `completedAt` "allows clocking data from
the DMS to be back-filled", which is about the record, not the ledger.

**Alternatives considered**: release from `Ceil(completedAt)`. Rejected: a back-filled
`completedAt` in the past would leave cells that are free in reality booked, or free cells already
used.

## R7. The no-show sweeper: a worker over the service's own `MarkNoShow`

**Decision**: a new package `internal/scheduling/sweeper` (notes 2 and 4.7 name it):

- `Service.RunOnce(ctx) (Report, error)`: `cutoff = clock.Now() - grace`; `Store.DueNoShows(ctx,
  cutoff)` returns the `CONFIRMED` appointments with `start_at < cutoff`, oldest first; for each it
  calls `marker.MarkNoShow(ctx, TransitionCommand{AppointmentID, ExpectedVersion: a.Version,
  Actor: "no-show-sweeper"})`. `VERSION_CONFLICT` and `INVALID_TRANSITION` count as skipped
  (someone acted first); any other error is logged and the run goes on. `Report` counts marked,
  skipped and failed.
- `Worker{svc, every}.Run(ctx) func() error`: a ticker loop that ends with the context, as notes
  4.7 sketch; `main` adds it to the errgroup.
- `marker` is a one-method interface (`MarkNoShow`) satisfied by `service.Service`, so the sweeper
  goes through the same middleware (logs, metrics, traces) and the same transaction as staff.
- Configuration in `platform.Config`: `NO_SHOW_GRACE` (default `30m`) and `NO_SHOW_SWEEP_INTERVAL`
  (default `1m`), Go durations, both must be positive. `docker-compose.yml` sets
  `NO_SHOW_SWEEP_INTERVAL: 1s` for the app container, which `make up` and the acceptance suite
  share.

**Rationale**: one implementation of the no-show transition (FR-051: "exactly the transition of
FR-050"). The strict `<` is the spec's "start plus grace strictly before now"; the boundary is a
unit test with `FakeClock`. One transaction per appointment, so one failure or conflict does not
hold back the others (FR-052).

**Alternatives considered**: the sweeper with its own transaction code over `Store`/`Tx` (notes
4.7's `sweeper.NewService(schedStore, clock, grace)`). Rejected: a second copy of the no-show
transaction. A single `UPDATE ... WHERE status = 'CONFIRMED' AND start_at < ?` over all rows:
rejected, it cannot release cells, append events and outbox rows per appointment in the
transaction the spec requires.

## R8. Problem codes and the order of checks

| Situation | Code | Status | Source |
|---|---|---|---|
| malformed body, missing `expectedVersion`, non-UUID id, date-time without offset, missing `X-Channel`, a reschedule naming nothing | `VALIDATION_ERROR` | 400 | contract `BadRequest`; feature 001 decoders |
| `expectedVersion < 1`, negative `mileage`, `reason` > 500, `additionalConcerns` > 4000, unknown channel, `startedAt`/`completedAt` after now or before the actual start, unknown or inactive pinned resource, pinned bay of the wrong type | `INVALID_VALUE` | 422 | contract schemas (`minimum`, `maxLength`) |
| customer channel on check-in, start, complete, no-show, or a reschedule with pins | `FORBIDDEN` | 403 | contract "Staff only"; spec Clarifications |
| unknown appointment | `NOT_FOUND` | 404 | contract `NotFound` |
| `expectedVersion` differs, or the guarded update matched no row | `VERSION_CONFLICT` | 409 | contract; notes 4.6 `ErrStaleVersion` |
| transition not in the table | `INVALID_TRANSITION` | 409 | contract `TransitionConflict` |
| reschedule found no pair; late arrival found no pair for any reason | `SLOT_TAKEN` + `alternatives` | 409 | contract `rescheduleAppointment`, `checkInAppointment` |
| reschedule's cells disappeared twice | `AVAILABILITY_CHANGED` + `alternatives` | 409 | contract `ProblemCode`; feature 001 |
| new start is another appointment's booking key | `DUPLICATE_BOOKING` + `existing` | 409 | design 7, 11.1 |
| reschedule: off grid, past, beyond horizon, outside working time, staff-only service through a customer channel, nobody qualified | `OFF_GRID_START`, `INVALID_VALUE`, `OUTSIDE_OPENING_HOURS`, `SERVICE_NOT_BOOKABLE_ONLINE`, `NO_QUALIFIED_TECHNICIAN` | 422 | booking's rules (FR-011, FR-012) |

The order is FR-003's. Decoding happens in the transport (as for booking); the 403 rule is checked
in the service, before the appointment is loaded, so the transport stays a parser. Every code is
already in `platform`'s table; `ErrStaleVersion`, `ErrInvalidTransition` and `ErrForbidden` are
new `scheduling.Error` values with the codes `VERSION_CONFLICT`, `INVALID_TRANSITION` and
`FORBIDDEN`, so the platform test that walks every exported error keeps covering them.
`VersionConflictError` carries the stored and the sent version for the detail text ("Appointment
is at version 3; you sent 2"), and is `ErrStaleVersion`.

**Conflict noted**: the contract's `TransitionConflict` response describes three 409 causes for
every Lifecycle operation, but only `checkInAppointment` can produce the third (`SLOT_TAKEN`).
Cancel, start, complete and no-show produce only `VERSION_CONFLICT` and `INVALID_TRANSITION`.

## R9. Transition table in `scheduling/enums.go`

**Decision**: notes 5.4's `appointmentTransitions` map and `CanTransitionTo` next to
`AppointmentStatus`, exactly as listed there (`CONFIRMED` → `CONFIRMED`, `CHECKED_IN`, `CANCELLED`,
`NO_SHOW`; `NO_SHOW` → `CHECKED_IN`; `CHECKED_IN` → `IN_PROGRESS`, `CANCELLED`; `IN_PROGRESS` →
`COMPLETED`), with a table test over all 36 pairs. Each service method states the target status;
the service asks `current.CanTransitionTo(target)` and returns `ErrInvalidTransition` before the
store is called. The idempotent cancel is checked before it (FR-003 step 4), so `CANCELLED →
CANCELLED` stays out of the table.

## R10. Acceptance testing of time, without a fake clock

**Decision**:

- **`@clock` tag**: scenarios whose outcome depends on the real time. Only they may use the
  backdating step.
- **Backdating step** ("the appointment is backdated by 14 days"): one transaction over a
  connection opened from `SCHEDULER_TEST_DSN`:
  `UPDATE appointment SET start_at = start_at - INTERVAL ? DAY, end_at = end_at - INTERVAL ? DAY
  WHERE id = ?` and `UPDATE time_slot SET slot_start = slot_start - INTERVAL ? DAY WHERE
  appointment_id = ?`, asserting one appointment row changed. It never reads anything to assert,
  never touches `version`, `status` or any other row. It lives in `test/acceptance/backdate_test.go`
  (a `_test.go` file, see below). `scripts/integration.sh` exports `SCHEDULER_TEST_DSN` pointing at
  the compose MySQL on `127.0.0.1:${MYSQL_PORT}`; without it a `@clock` step fails with a message
  naming `make test-integration`.
- **14 days**: the London "today" is at most one day ahead of the database's `CURRENT_DATE`, so day
  12 is at most `CURRENT_DATE + 13`, and 14 days back is before `CURRENT_DATE`, where the fixture
  has no cells. Primary keys therefore cannot collide, and the same offset everywhere keeps
  disjoint windows disjoint.
- **Polling step** ("the appointment becomes "NO_SHOW" within 10 seconds"): `GET` the record every
  200 ms until the status matches or the deadline passes; the last fetch becomes the current
  record. No fixed sleep anywhere.
- **Sweeper interval** `1s` in `docker-compose.yml`, so 10 s is ten sweeps.
- **Late arrival**: backdating cannot help (the claim is at the current time), so `LDS01` gets
  `Bay L1` and `Jo` with cells for all 96 cells of each of the 14 days. The step "the appointment
  starts at the current grid cell" records the instants before sending and after receiving the
  check-in and accepts a start between the floor of the first minus one cell and the floor of the
  second, allowing a few seconds of clock difference between the suite and the container.

**The ledger guard**: `ledger_guard_test.go` walks every non-test `.go` file of the module and fails
on an `UPDATE time_slot` outside `ledger.go`. The backdating statement is test code, so it goes in a
`_test.go` file of `package acceptance` (`backdate_test.go`), which the guard already skips. The
guard is not changed. Recorded as a conflict in plan.md.

**Rationale**: decision 2. The constitution's Testing Standards ask for controllable clocks or
triggers, never wall-clock sleeps: the backdating step controls the appointment's time instead of
the clock, the interval is configuration, and the poll has a deadline but no sleep that an outcome
depends on.

**Alternatives considered**: a `harness` clock endpoint (notes 4.7, 6.1). Rejected by decision 2
(no test-only endpoints, no fake clock in the acceptance path). Backdating by "start minus 31
minutes": rejected, it would move cells onto today's cells and collide with the primary key.
`MAN01` cells around the clock for the late arrival: rejected, it changes working time for every
feature 001 scenario (`OUTSIDE_OPENING_HOURS` at 06:00 and 19:00 would stop being refused, because
working time is any cell of the dealership).

## R11. Events, outbound rows and actors

| Transition | `appointment_event.event_type` | Event payload | `outbox.event_type` |
|---|---|---|---|
| reschedule | `RESCHEDULED` | `reason`, `previous` and `new` `{startAt, endAt, serviceBayId, technicianId}` | `AppointmentRescheduled` |
| cancel | `CANCELLED` | `reason`, `previousStatus` | `AppointmentCancelled` |
| check-in | `CHECKED_IN` | `reason`, `mileage`, `additionalConcerns`; on a late arrival also `lateArrival: true`, `previous`, `new` | `AppointmentCheckedIn` |
| start | `STARTED` | `reason`, `actualStartAt` | `AppointmentStarted` |
| complete | `COMPLETED` | `reason`, `actualEndAt`, `cellsReleased` | `AppointmentCompleted` |
| no-show | `NO_SHOW` | `reason`, `cellsReleased` | `AppointmentNoShow` |

The outbox payload is the staff view of the record after the transition, as booking writes for
`AppointmentConfirmed`. The actor is `anonymous` with the request's channel, or `no-show-sweeper`
with a `NULL` channel (the column is nullable; `Events` already reads `NULL` as no channel). Event
payloads are shown only to staff channels, as today.

## R12. Customer channels

Check-in, start, complete, no-show and a reschedule with pins are refused to `CUSTOMER_WEB` and
`CUSTOMER_APP` with 403 `FORBIDDEN` (spec Clarifications). Cancel and a time-only reschedule are
allowed; the response uses the customer view (no cells, no notes, technician id only), as `Get`
does. A reschedule through a customer channel of a staff-only service type is 422
`SERVICE_NOT_BOOKABLE_ONLINE`, booking's rule.

## R13. Fixture rows for `LDS01`

`local/mysql/data.sql` gains, after the existing rows: `Bay L1`
(`b0000000-0000-4000-8000-000000000011`, GENERAL, ACTIVE) and `Jo`
(`e0000000-0000-4000-8000-000000000011`, ACTIVE) at `LDS01`; `Jo`'s GENERAL_SERVICE skill valid from
a year ago; and a second ledger insert for those two resources, `CURRENT_DATE` to
`CURRENT_DATE + 13`, every 15-minute cell from 00:00Z (96 per resource-day, 2 688 in all). The
`LDS01` comment changes to say why. `test/acceptance/fixture.go` gains both names.
