# Research: Book an appointment

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Date**: 2026-10-12

Every decision below is taken from the design extract (`docs/design/SCHEDULING_DESIGN.md`, "design N.N"), the implementation notes (`docs/design/IMPLEMENTATION_NOTES.md`, "notes N.N"), the contract (`api/openapi.yaml`), the schema (`docs/design/schema.sql`) and the release decisions given for this feature. Where those sources disagree, the decision says which one wins and why. No item in the Technical Context is left as NEEDS CLARIFICATION.

---

## R1. The claim: locking read, check in Go, update

- **Decision**: `Tx.ClaimPair` in `internal/scheduling/mysql/ledger.go` is the only code that marks cells BOOKED. Per resource, bay first then technician: `SELECT slot_start, status, appointment_id FROM time_slot WHERE resource_kind = ? AND resource_id = ? AND slot_start BETWEEN ? AND ? ORDER BY slot_start FOR UPDATE`; fewer rows than the run needs is `ErrBayNotOpen` / `ErrTechnicianNotWorking` (both `ErrCellsMissing`); any row not FREE is `*SlotTakenError` (is `ErrSlotTaken`); otherwise `UPDATE time_slot SET status = 'BOOKED', appointment_id = ?, version = version + 1 WHERE ...` over the same range. Default isolation (REPEATABLE READ). No `NOWAIT`.
- **Rationale**: deviation D2 (notes 0, 4.5) and the comment block after `time_slot` in `docs/design/schema.sql`. A locking read is a current read at any isolation level; two claimers serialise on the first shared row and the loser sees BOOKED before writing. Bay before technician in every caller keeps plain claims deadlock-free (design 11.3). `version = version + 1` keeps the schema's flip counter meaningful (notes 5.1).
- **Alternatives considered**: the design's `UPDATE ... WHERE status = 'FREE'` plus `ROW_COUNT()` inside `sp_claim_pair` (design 11.3). Rejected by D2 and by decision 5 (no stored procedures). `FOR UPDATE NOWAIT` (notes 4.5 "optional"): rejected until a measured need, per Principle III.
- **Guard**: a unit test in `internal/scheduling/mysql` greps the module for `UPDATE time_slot` and `INSERT INTO time_slot` outside `ledger.go` and fails (notes 4.5 rule 3). Code review of `ledger.go` checks `FOR UPDATE` first (rule 1); the twenty-client race scenario catches its removal.

## R2. Idempotency: natural key, insert first, looked up before capacity

- **Decision**: the unique index `uq_appointment_booking (vehicle_id, customer_id, start_at, kind)` is the guard; `InsertAppointment` is the first statement of every attempt. MySQL 1062 naming `uq_appointment_booking` maps to `ErrDuplicateBooking`. In addition, `Book` calls `Store.AppointmentByBookingKey` (a) after the request's references are validated and before any capacity rule, and (b) before answering `SLOT_TAKEN` or `AVAILABILITY_CHANGED`. A found appointment with the same dealership and service type and a status other than CANCELLED or NO_SHOW is returned with 201; otherwise `*DuplicateBookingError` carrying the existing appointment (409).
- **Rationale**: deviation D4 and design 5.2 give the semantics. The extra lookups close a gap in the notes 4.4 sketch: there, a replay of a booking whose only qualified pair it already holds (`MOT`: Bay 2 and Priya) finds no free candidate and returns `SLOT_TAKEN` before it ever reaches the insert that would have reported the duplicate. The contract requires 201 for that replay. The pre-capacity lookup also makes a replay at another dealership (`LDS01`, no cells) answer `DUPLICATE_BOOKING` instead of `OUTSIDE_OPENING_HOURS`. Spec clarification Q3.
- **Concurrency**: two identical submissions that both miss the lookup both reach the insert; the second blocks on the unique index until the first commits (1062, then the existing appointment) or rolls back (it proceeds). If the first rolls back while two others wait, InnoDB can pick one of them as a deadlock victim (1213); that is retried once on the same pair (R5), after which the lookup finds the winner.
- **Alternatives considered**: an `idempotency_key` table and header (design 5.2, 7): rejected by D4. Lookup only on 1062 (notes 4.4): rejected for the replay gap above.

## R3. Candidate pairs: islands query plus a pure function

- **Decision**: per request, for the requested local date in the dealership's time zone, `Store.DayLedger(dealershipID, day)` runs the design 11.4 "free runs per resource" query (islands of FREE cells) and a `COUNT(*) ... status = 'BOOKED' GROUP BY resource_kind, resource_id` for loads, both through `ix_slot_day`. `WorkshopReader.Resources(dealershipID, date)` returns ACTIVE bays and ACTIVE technicians with their skills valid on that date (design 11.4 "Qualified technicians"). `scheduling.CandidatePairs(start, grid, requirement, bays, techs, islands, loads, seed)` keeps bays of an allowed type whose own island contains the bay run, and technicians holding every required skill whose own island contains the technician run, forms the cross product, sorts by load (bay load + technician load), and shuffles each equal-load group with `math/rand/v2` seeded from an FNV-1a hash of the appointment id.
- **Rationale**: design 5.1 (per-resource containment, then AND across kinds; the split-across-bays case is a named unit test), design 5.2 (least-loaded first, ties shuffled per request), notes 4.3, contract `CandidatePair.load`. One appointment id per request (R4) makes the seed reproducible in unit tests through `platform.IDs` without a new dependency. Spec clarification Q4.
- **Alternatives considered**: a random seed from `crypto/rand` or the clock: rejected, unit tests could not assert order. A seed from the request: rejected, a replay would then always contend for the same pair.

## R4. Booking order of checks and the candidate loop

- **Decision**: `service.Book` runs, in order:
  1. Decode and validate the request in the transport: JSON shape, required fields, `startAt` RFC 3339 with offset, `kind` and `X-Channel` enumerations, text lengths (400 `VALIDATION_ERROR` or 422 `INVALID_VALUE`).
  2. Resolve references: dealership config, service type requirement (active), customer, vehicle; same tenant (the dealership's); vehicle owned by customer (422 `INVALID_VALUE` naming the field).
  3. Booking-key lookup (R2).
  4. Customer channel and staff-only service type: 422 `SERVICE_NOT_BOOKABLE_ONLINE`.
  5. `Grid.Align(startAt)`: 422 `OFF_GRID_START`.
  6. `startAt` not after now, or after now + booking horizon: 422 `INVALID_VALUE` on `startAt`.
  7. Bay and technician runs from the requirement (buffers widen them).
  8. Opening hours from the ledger (R6): 422 `OUTSIDE_OPENING_HOURS`.
  9. No ACTIVE technician holds every required skill on the local date: 422 `NO_QUALIFIED_TECHNICIAN`.
  10. Candidates (R3); empty: booking-key re-check, then 409 `SLOT_TAKEN` with alternatives.
  11. Generate one appointment id; for each pair, one `InTx`: insert appointment, `ClaimPair`, append event, append outbox. `ErrSlotTaken`: next pair. `ErrDeadlockVictim`: same pair once, then treat as taken. `ErrCellsMissing`: recompute candidates once and restart; second time: re-check, then 409 `AVAILABILITY_CHANGED`. `ErrDuplicateBooking`: load and apply R2.
  12. Pairs exhausted: re-check, then 409 `SLOT_TAKEN` with alternatives.
- **Rationale**: notes 4.4 is the loop; design 5.2 and 11.3 give the reactions per error; the order puts every check that needs no capacity read before any capacity read, so refusals are cheap and touch nothing (spec User Story 4). One id per request instead of one per attempt (notes 4.4 generates per attempt) because a rolled-back attempt leaves no row, and a stable id gives a stable seed.
- **Alternatives considered**: checking opening hours from candidates being empty (notes 4.4 sketch): rejected, it would answer `SLOT_TAKEN` for a request outside working time.

## R5. Error model: domain codes, one status table

- **Decision**: the scheduling root's errors carry their contract code through a `ProblemCode() string` method (standard library only); `*SlotTakenError`, `*AvailabilityChangedError` and `*DuplicateBookingError` also expose `ProblemExtensions() map[string]any` (alternatives, existing). `platform.EncodeError` stays the only place that knows HTTP statuses: it maps a code to a status from one table, writes the contract `Problem` (`type`, `title`, `status`, `code`, `detail`, plus `alternatives`, `existing` or `errors` when present). `platform.ErrBadRequest` becomes `VALIDATION_ERROR` (was `BAD_REQUEST`), and a `platform.FieldError` carries `errors[]` for `VALIDATION_ERROR` and `INVALID_VALUE`. `type` is `https://scheduler.example.com/problems/<CODE>` as in the contract examples. Unknown errors stay 500 `INTERNAL` with no detail.
- **Rationale**: notes 4.6 (one encoder, one mapping per layer) and `docs/architecture.md` ("`internal/platform` imports no domain package"). Notes 4.6 has `problemFor` switch on domain errors, which would make `platform` import `scheduling`; an interface keeps the status table in `platform` without the import. The contract's `ProblemCode` list has `VALIDATION_ERROR` for 400, not the bootstrap's `BAD_REQUEST`; the contract wins.
- **Test**: a table test in `internal/platform` covers every code in the contract's `ProblemCode` list; a test in `internal/scheduling/service` (which imports both) asserts that every exported scheduling error has a code with a non-500 status (notes 4.6).
- **Alternatives considered**: `platform` importing `scheduling` (notes 4.6 as written): rejected by the architecture rule. HTTP statuses on domain errors: rejected, a domain root must not know HTTP.

## R6. "Outside opening hours" from the ledger

- **Decision**: `Store.WorkingCells(dealershipID, first, last)` returns `COUNT(DISTINCT slot_start)` over `time_slot` for the dealership in the run's range (any resource, any status, through `ix_slot_day`). If it is less than the run's cell count for the bay run or the technician run, the answer is 422 `OUTSIDE_OPENING_HOURS`.
- **Rationale**: the opening-hours tables are not in this release (`docs/design/schema.sql` note; decision 1), and design 4.2 defines working time as cell existence. Spec clarification Q2.
- **Alternatives considered**: a hard-coded 08:00 to 18:00 window or a config value: rejected, it would duplicate what the fixture already says and drift from it. Treating it as `SLOT_TAKEN`: rejected, the contract has a distinct code.

## R7. Alternatives

- **Decision**: `scheduling.FeasibleStarts` (notes 4.3) over the requested local date and the next one, using the same islands query per day; keep starts later than now and not equal to the requested start; order by `|start - requested|`, earlier first on a tie; take five; each `Slot` is `{startAt, endAt = start + duration}`.
- **Rationale**: contract `Problem.alternatives` ("up to five nearest feasible starts, freshly computed"), design 5.2 and 5.3. The two-day window bounds the cost to two islands queries. Spec clarification Q5.
- **Alternatives considered**: the whole booking horizon: rejected, up to 28 queries for an error path.

## R8. Channel, tenant and actor without authentication

- **Decision**: an `X-Channel` header with a `Channel` value on every `/appointments` request; a missing header is 400 `VALIDATION_ERROR`, an unknown value is 422 `INVALID_VALUE`, both naming `X-Channel`. Staff channels are `ADVISOR_CONSOLE`, `CALL_CENTRE`, `INTEGRATION`; customer channels `CUSTOMER_WEB`, `CUSTOMER_APP`. The tenant is the dealership's; customer, vehicle and service type must share it. The audit actor is `anonymous`. No bearer token is validated, so 401 and 403 are never produced.
- **Rationale**: decision 1 puts authentication out of scope, but the contract derives channel from the token claim and the behaviour (`SERVICE_NOT_BOOKABLE_ONLINE`, the customer view) depends on it. Notes 4.0 anticipates "a header in v1" for the tenant. Spec clarification Q1.
- **Alternatives considered**: a `channel` field in the body (notes 4.0 `bookBody`): rejected, the contract's `BookAppointmentRequest` has no such field and adding one would change the contract. A default channel: rejected, a silent default would hand anonymous callers the staff view.

## R9. Routes and the `/v1` server prefix

- **Decision**: the routes are `POST /appointments` and `GET /appointments/{appointmentId}` on the root mux, next to `GET /healthz`. `GET /appointments` (`listAppointments`) is not registered; the mux answers 405 for it.
- **Rationale**: the feature description, notes 4.0 `RegisterRoutes` and the existing `/healthz` all use unprefixed paths. The contract's `servers` entries end in `/v1`; that is read as a gateway base path. This is a conflict reported to the reviewer.
- **Alternatives considered**: serving under `/v1`: rejected for this feature because it contradicts the feature description; a one-line change if the reviewer prefers it.

## R10. Times on the wire

- **Decision**: `startAt` must be RFC 3339 with an offset (400 otherwise). Seconds and fractions must be zero for the grid check (`OFF_GRID_START` otherwise). Responses render `startAt`, `endAt`, cell ranges and alternatives in the dealership's time zone offset, and `createdAt` and `updatedAt` in UTC (`Z`), as in the contract example and design Appendix B. Storage is UTC (`parseTime=true&loc=UTC`, `time_zone='+00:00'`). Cell ranges are half-open `[from, to)` with `cells` = count (contract `CellRange`).
- **Rationale**: design 6 and 7 ("ISO-8601 with offset; stored UTC"), design 11.1, notes 5.2. Acceptance steps compare instants, not strings.

## R11. The record view and its reads

- **Decision**: `Get` loads the appointment row and its owned cells (`time_slot WHERE appointment_id = ?`, grouped per resource kind into one `CellRange` each) from `scheduling/mysql`, and the names (dealership code, name and time zone; customer display name; vehicle registration, make, model; service type code, name, duration; bay name and type; technician name) through `WorkshopReader` and `CustomerReader`. `include=events` adds `appointment_event` rows. The view is built per caller channel: customer channels get no `cells`, no `notes`, and the technician without `displayName`. `actualStartAt` and `actualEndAt` are always `null` (no columns exist, see data-model.md).
- **Rationale**: notes 2 ("one `mysql` package per domain, touching only that domain's tables"; cross-domain reads only through reader interfaces and adapters). A replay answer is rendered for the current caller's channel.
- **Alternatives considered**: one SQL join across all tables in `scheduling/mysql`: fewer round trips, rejected because it breaks the domain rule the layout exists for.

## R12. Layout and dependencies

- **Decision**: domains `internal/scheduling` (root, `mysql`, `adapters`, `service`), `internal/setup` (root and `mysql`, read-only repository) and `internal/customers` (root and `mysql`, read-only repository). No `setup/service` or `customers/service`, no `engagement`, no workers: nothing in scope needs them. Add `github.com/go-kit/kit` (packages `endpoint`, `transport/http`) at a release published at least two weeks ago (v0.13.0 is the current one); everything else is already in `go.mod` (`go-kit/log`, `go-sql-driver/mysql`, `google/uuid`, OTel, `go.uber.org/mock`, `godog`).
- **Rationale**: decision 5 and notes 2, 4.0. Principle III: only the layers this feature uses.
- **Alternatives considered**: a single `internal/booking` package with SQL inline: simpler today, rejected by decision 5; it would also be split in feature 002.

## R13. Middleware and observability

- **Decision**: `LoggingMiddleware`, `InstrumentingMiddleware` (request counter and latency histogram per method with the problem code as an attribute, which gives the `SLOT_TAKEN` rate design 7 asks for) and `TracingMiddleware` around the scheduling service; `InTx` opens a span per transaction (notes 4.0).
- **Rationale**: notes 4.0, design 7 observability metrics.
- **Alternatives considered**: logging only: rejected, the `SLOT_TAKEN` rate at confirm is a design metric and costs one small file.

## R14. Schema and fixture delivery

- **Decision**: copy the 15 table definitions verbatim from `docs/design/schema.sql` into `local/mysql/schema.sql`, in dependency order, with the `time_slot` claim comment block. `local/mysql/data.sql` inserts the fixture with fixed UUIDs (data-model.md lists them) and generates FREE cells with a recursive CTE: days `CURRENT_DATE` to `CURRENT_DATE + 13`, cells `07:00Z + 15 min * k` for `k = 0..39`, for every bay and technician of `MAN01` (3 360 rows). Skill validity is relative: `valid_from = CURRENT_DATE - INTERVAL 1 YEAR`; Lee's AIRCON_FGAS has `valid_to = CURRENT_DATE - INTERVAL 1 DAY`.
- **Rationale**: decisions 2 and 5; notes 5.1 (init scripts, no migrations, no routines). Relative dates keep the fixture from going stale and need no fake clock. 07:00Z to 17:00Z is 08:00 to 17:00 local in both BST and GMT, so scenarios can name local times without caring which offset applies.
- **Fixture refinements over decision 2's suggestion**, each needed to reach an outcome black-box: `LDS01` (duplicate key at another dealership), `EV_CHECK` (a free bay with the only qualified technician busy), `DIAGNOSTIC` staff-only and 50 minutes (`SERVICE_NOT_BOOKABLE_ONLINE`, rounding to whole cells), `AIRCON` with an expired skill (`NO_QUALIFIED_TECHNICIAN`, skill validity), `INTERIM` on GENERAL or MOT bays (two bays for "two customers, one slot"), `EV_MAJOR` buffers 15/15 (design Appendix B), `Customer 01` to `Customer 20` (twenty distinct booking keys for the race).
- **Alternatives considered**: notes 5.1's fixed window 2026-10-19 to 2026-10-31 on Monday to Friday 08:00 to 18:00 local: rejected by decision 2 (goes stale; DST makes 08:00 local a different UTC hour across the window).

## R15. Acceptance suite shape

- **Decision**: the suite stays a black-box HTTP client (decision 3). Additions to `test/acceptance/`: `fixture.go` (the fixed ids from `data.sql` by name), `dates.go` ("today" in Europe/London fixed once at suite start; `tomorrow`, `in N days`, `yesterday`; local `HH:MM` to an instant with `time.LoadLocation`), `steps_booking.go`, `steps_record.go`, `steps_race.go` (N goroutines released together by closing a channel, outcomes collected and counted). The world keeps the last response, the first and last appointment, and race outcomes. Every request sends `X-Channel` (default `ADVISOR_CONSOLE`). The step vocabulary is in [contracts/acceptance-steps.md](./contracts/acceptance-steps.md).
- **Isolation without resets**: the database is not reset between scenarios, so each feature file books MAN01 only on its own days and windows (calendar below). Days are counted from the suite's "today"; the fixture's cells start at the container's UTC `CURRENT_DATE`, so even when the London date is one ahead of UTC, day 8 is within the 14 days of cells.

  | Day | Feature | Windows (local) |
  |---|---|---|
  | 1 | booking | 08:45 to 11:15, 11:30 to 12:15, 13:00 to 13:45, 15:00 to 15:45 (refusals touch nothing) |
  | 2 | booking | least-loaded scenario, whole day |
  | 3 | claim_ledger | 09:00 to 09:45, 10:45 to 12:45, 14:00 to 15:00 |
  | 4 | claim_ledger | 09:00 to 10:30, 12:00 to 12:45, 13:45 to 15:45 |
  | 5 | claim_ledger | 09:45 to 12:15, 14:00 to 14:45 |
  | 6 | idempotency | 09:00 to 16:30 in six disjoint windows |
  | 7 | idempotency, concurrency | 10:00 to 10:45; 13:00 to 13:45; 15:00 to 15:45 |
  | 8 | concurrency | 09:00 to 15:00 |
  | 9 to 12 | free for feature 002 | |

- **Rationale**: decisions 3 and 4; `docs/development.md` (scenarios create or pick their own data). Notes 6.1 (per-scenario truncate, direct `time_slot` reads, a harness to pin pairs, a fake clock, an in-process server) is superseded by decision 3; ledger state is read through `cells` on the staff view of the record.
- **Time-dependence**: no acceptance assertion depends on the exact wall-clock time: "yesterday" is always past, "in 30 days" always beyond the 28-day horizon, and every booked day is in the future. Boundaries at exactly now and exactly the horizon are unit tests with `platform.FakeClock` (constitution Testing Standards).

## R16. What only unit tests can prove

Black-box acceptance cannot provoke these in this release; each gets a unit test with mocks (notes 6.0):

- `AVAILABILITY_CHANGED`: cells never disappear (no materialiser, no setup CRUD).
- Deadlock victim retried once on the same pair, then treated as taken.
- `InsertAppointment` is the first call inside `InTx`, `ClaimPair` the second, then event and outbox, all in the same `InTx` (mock `Tx` records order).
- The outbox row and the `CONFIRMED` event payloads (no relay in this release; the event is visible through `include=events`, the outbox row is not visible at all).
- Split-across-bays is not a candidate (design 5.1 named test), least-loaded ordering and the seeded shuffle with a fixed id.
- Grid alignment, `CellsFor` rounding, runs with buffers, local-date handling on DST change days.
- Mapping of MySQL 1062 (only for `uq_appointment_booking`) and 1213 in `scheduling/mysql/errors.go`.
