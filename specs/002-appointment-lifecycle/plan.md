# Implementation Plan: Appointment lifecycle

**Branch**: `002-appointment-lifecycle` | **Date**: 2026-10-13 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/002-appointment-lifecycle/spec.md`

## Summary

Deliver the six transitions after booking, `rescheduleAppointment` (also reassign),
`cancelAppointment`, `checkInAppointment` (with the `NO_SHOW` late arrival), `startAppointment`,
`completeAppointment` and `markNoShow`, and the no-show sweeper, on top of feature 001's Scheduling
service. Every transition is one transaction in one order: the version-guarded update of the
appointment row, then the ledger change the state machine prescribes (`ClaimPair` with the
appointment's own cells claimable, then the new `ReleaseCells`), then the audit event and the
outbox row (design 4.3, 5.4, 11.4; notes 4.4, 4.5). Reschedule and the late arrival reuse
booking's plan, checks, candidate search and claim loop, with the appointment as the owner whose
cells count as free and optional pinned resources. The sweeper is a ticker worker that marks
overdue `CONFIRMED` appointments through the service's own `MarkNoShow`. The transition table
lives in `scheduling/enums.go` (notes 5.4). The appointment table gains `actual_start_at` and
`actual_end_at` from the design schema. Time-dependent scenarios run against the real clock: a
`@clock` step backdates an appointment and its cells, and the suite polls the public record.
Technical decisions are in [research.md](./research.md).

## Technical Context

**Language/Version**: Go 1.26 (`go.mod`)

**Primary Dependencies**: unchanged from feature 001: `database/sql` + `github.com/go-sql-driver/mysql` (hand-written SQL, no ORM), `github.com/go-kit/kit` endpoint and HTTP transport, `github.com/go-kit/log`, `github.com/google/uuid`, OpenTelemetry, `golang.org/x/sync/errgroup`. The acceptance suite starts importing the MySQL driver already in `go.mod`, for the `@clock` step only. No new module

**Storage**: MySQL 8.4 in docker compose; `local/mysql/schema.sql` gains two columns copied from `docs/design/schema.sql`; `local/mysql/data.sql` gains `LDS01` resources and cells. No migrations, no stored routines, no triggers, VARCHAR enumerations as Go constants

**Testing**: unit tests with `go.uber.org/mock` and `platform.FakeClock` (`make test`); Gherkin acceptance with godog against the containerised service (`make test-integration TAGS=@spec-002`), black-box over HTTP except the `@clock` backdating step

**Target Platform**: Linux container (Alpine, static binary), one process

**Project Type**: web service (HTTP JSON API) with one in-process background worker

**Performance Goals**: a transition < 200 ms p95 at 50 req/s on one node, booking's target (design 7)

**Constraints**: no false confirmation and no half-applied transition under any concurrency; correct at MySQL's default REPEATABLE READ; only `internal/scheduling/mysql/ledger.go` writes `time_slot` in non-test code; no test-only endpoint, fake clock or worker trigger in the running service (decision 2); every time stored in UTC

**Scale/Scope**: 6 operations, 1 worker, 2 configuration variables; 29 named scenarios (50 executed with outline examples) in 3 feature files; the fixture grows by 2 resources and 2 688 cells

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Checked against `.specify/memory/constitution.md` v1.0.0.

| Principle / gate | Pre-research | Post-design | Notes |
|---|---|---|---|
| **I. Integration-first** | PASS (pending approval) | PASS (pending approval) | The three feature files exist before any code and fail on first run (no step is defined; strict mode). Scenarios go through the HTTP API, with one sanctioned exception: `@clock` scenarios write to the database to backdate an appointment and its cells (decision 2; `docs/design/README.md`), never to assert. That contradicts today's `docs/development.md` ("It never touches the database"), which the docs task rewrites; it is recorded in Complexity Tracking. **The Behavior gate is open: the user has not approved the scenarios.** Implementation must not start until they do. Proven by unit tests only, because no black-box scenario reaches them deterministically: the grace boundary, the split of cells on complete inside a job, the deadlock retry and `AVAILABILITY_CHANGED` on a reschedule, the statement order in each transaction, the outbox rows, text-length limits, the `completedAt` rules. That is a gap in what the acceptance suite proves, not a violation |
| **II. Test-first (non-negotiable)** | PASS | PASS | Order of work below: scenarios (written), then unit tests per package written, approved and seen failing, then code |
| **III. Simplicity** | PASS with justified complexity | PASS with justified complexity | One new package (`scheduling/sweeper`), one new store type (`Keep`), one new parameter (`DayLedger` owner), the 403 rule, the version pre-check, the `@clock` step and the `LDS01` fixture rows are recorded in Complexity Tracking. Reschedule reuses booking's plan instead of a second flow; the claim keeps its signature (research R2); the sweeper reuses `MarkNoShow` (R7). What is not built is listed below |
| **IV. Specs are ephemeral** | PASS | PASS | `specs/001-book-appointment/` was read for context only; it is cited nowhere and unchanged. Nothing in code or `docs/` will reference `specs/` |
| **V. Living documentation** | PASS | PASS | The task list must update `docs/architecture.md`, `docs/features.md`, `docs/development.md` and the README status (below) |
| Testing Standards: deterministic; controllable clocks or triggers, never wall-clock sleeps | PASS with note | PASS with note | Honest reading: the acceptance path has no controllable clock. What is controlled is the appointment's time (backdating) and the sweeper's interval (configuration, 1 s under compose); outcomes are read by polling with a 10 s deadline, never by sleeping a fixed time. Two residual dependencies on the wall clock remain: the sweeper must tick within 10 s (ten intervals of margin), and the late arrival's "current grid cell" is checked with one cell of tolerance for clock difference between suite and container. Exact time rules are unit tests with `FakeClock`. A justified deviation, recorded in Complexity Tracking |
| Development workflow gates | Behavior gate open | Behavior gate open | Red, Green, Refactor, Docs and Review gates apply in the implementation phase |

No unjustified violation. The gate passes for planning; it does not pass for implementation until
the Behavior gate closes.

## Project Structure

### Documentation (this feature)

```text
specs/002-appointment-lifecycle/
├── spec.md                       # specification, with Clarifications
├── checklists/requirements.md    # spec quality checklist
├── plan.md                       # this file
├── research.md                   # Phase 0 decisions R1 to R13
├── data-model.md                 # tables used, columns copied, Go types, fixture rows
├── quickstart.md                 # how to validate end to end
└── contracts/
    ├── README.md                 # the six operations of api/openapi.yaml, codes, stand-ins
    └── acceptance-steps.md       # step vocabulary: reused, extended, new
```

### Source Code (repository root)

```text
features/
├── lifecycle.feature             # written in this step (13 scenarios, 31 executed)
├── reschedule.feature            # written in this step (12 scenarios, 15 executed)
└── noshow.feature                # written in this step (4 scenarios, all @clock)
local/mysql/
├── schema.sql                    # appointment: actual_start_at, actual_end_at copied from docs/design/schema.sql
└── data.sql                      # LDS01: Bay L1, Jo, Jo's skill, 2 688 cells around the clock
docker-compose.yml                # app: NO_SHOW_SWEEP_INTERVAL=1s
scripts/integration.sh            # exports SCHEDULER_TEST_DSN for the suite
cmd/scheduler/main.go             # builds the sweeper over the wrapped Service, adds its Worker to the errgroup
internal/platform/config.go       # NoShowGrace (NO_SHOW_GRACE, 30m), NoShowSweepInterval (NO_SHOW_SWEEP_INTERVAL, 1m)
internal/scheduling/              # domain root, standard library only
├── appointment.go                # Appointment.ActualStartAt/ActualEndAt; Keep
├── enums.go                      # transition table, CanTransitionTo (notes 5.4)
├── errors.go                     # ErrStaleVersion, VersionConflictError, ErrInvalidTransition, ErrForbidden
├── grid.go                       # Floor, Ceil
├── repository.go                 # Store.DayLedger(owner), Store.DueNoShows; Tx.UpdateAppointment, Tx.ReleaseCells
├── mysql/
│   ├── ledger.go                 # claim: cells of the claimer are claimable; ReleaseCells; DayLedger owner
│   └── appointments.go           # actual_* columns; UpdateAppointment (1062, zero rows); DueNoShows
├── service/
│   ├── service.go                # Service gains Reschedule, Cancel, CheckIn, Start, Complete, MarkNoShow; commands
│   ├── booking.go                # plan gains owner and pins; current pair first; alternatives pass the owner
│   ├── reschedule.go             # Reschedule and the late-arrival placement (reuse booking's steps)
│   ├── lifecycle.go              # Cancel, CheckIn, Start, Complete, MarkNoShow over one transition helper
│   ├── endpoint.go transport.go  # six endpoints, decoders, routes
│   ├── middleware.go             # logging, instrumenting, tracing for the six methods
│   └── view.go                   # actualStartAt, actualEndAt
└── sweeper/
    └── sweeper.go                # Service (RunOnce, Report), Worker (ticker loop)
test/acceptance/
├── fixture.go                    # Bay L1, Jo
├── world.go                      # appointments by customer; last seen version
├── steps_lifecycle.go            # transitions, prepared transition requests, record assertions
├── steps_booking.go              # "has booked ... at dealership"; numeric "set to"
├── steps_clock.go              # @clock backdating and polling steps; the suite's only database access
└── suite_test.go                 # registers the new steps
```

**Structure Decision**: the existing single module and go-kit per-domain layout (notes 2). The
only new package is `internal/scheduling/sweeper`, where notes 2 already place it. The backdating
step is in a `_test.go` file so the ledger guard, which checks non-test code, keeps passing
unchanged; it is registered from `suite_test.go`, because non-test files cannot reference symbols
of `_test.go` files.

## What this feature does not build

- No availability endpoint, no `listAppointments`, no schedule view.
- No outbox relay, no publisher, no notification, no DMS adapter or repair order; outbox rows are written and stay unpublished.
- No setup CRUD, materialiser, rotas, holds, absences, blocks, channel reservations or caps.
- No authentication or ownership checks; only the channel-based 403 of FR-008.
- No overrun detection, at-risk flags, automatic reallocation, reinstating, or undoing a completion.
- No harness, no test-only endpoint, no fake clock or `RunOnce` trigger reachable in the running service, no routes beyond the six.
- No sweeper batching, leader election or multi-instance locking: one process runs in this release.
- No `NOWAIT` claim, no retry beyond feature 001's single deadlock retry, no cache.
- No column beyond the two copied from the design schema; `mileage` and `additionalConcerns` live in the event payload.

## Approach and order of work

Each step follows Red → approval → Green → Refactor (Principle II). Tests in a step are written,
approved and seen failing before the code in that step.

1. **Behavior gate.** The user approves `features/lifecycle.feature`, `reschedule.feature` and
   `noshow.feature`. Run `make test-integration TAGS=@spec-002` and record that they fail
   (undefined steps).
2. **Schema and fixture.** Copy `actual_start_at` and `actual_end_at` into `local/mysql/schema.sql`
   verbatim; add the `LDS01` rows to `data.sql` (data-model.md). `NO_SHOW_SWEEP_INTERVAL=1s` in
   `docker-compose.yml`; `SCHEDULER_TEST_DSN` in `scripts/integration.sh`. Check with `make db-up`
   (6 048 cells) and run the full feature 001 suite: it must stay green.
3. **Step definitions** for contracts/acceptance-steps.md. Scenarios now fail on the missing routes
   (404), not on undefined steps.
4. **Scheduling root**: transition table and its 36-pair table test; `Grid.Floor`, `Grid.Ceil`;
   `Keep`; the new errors and their entries in the platform error-walk test; `Appointment` actual
   times.
5. **Scheduling `mysql`**: `UpdateAppointment`, `ReleaseCells`, the claim's own-cell rule,
   `DayLedger` owner, `DueNoShows`, the actual-time columns. `ledger_guard_test.go` unchanged and
   green. Exercised by the acceptance suite (locking is not unit-testable, notes 6.0).
6. **Scheduling service** with mocks: FR-003's order for each method; the transition table
   consulted before the store; statement order inside `InTx` (update, claim, release, event,
   outbox); reschedule's pins, current pair first, own cells free, deadlock retry, recompute then
   `AVAILABILITY_CHANGED`, `SLOT_TAKEN` alternatives, `DUPLICATE_BOOKING` from 1062,
   `VERSION_CONFLICT` from zero rows; late arrival at `Floor(now)` and every failure as
   `SLOT_TAKEN`; complete releases from `Ceil(now)` (09:40 on a 09:00 to 10:00 job keeps three cells
   per resource); `startedAt`/`completedAt` rules; idempotent cancel; 403 for customer channels.
7. **Sweeper** with mocks and `FakeClock`: the cutoff is `now - grace` and `start + grace == now`
   is not yet due; conflicts are skipped; one failure does not stop the run; the worker stops with
   its context.
8. **Endpoints, transport, middleware, wiring** (`main.go`, config); transport table tests for every
   decoder rejection and status.
9. **Green**: `make test`, `make test-integration TAGS=@spec-002`, then the whole suite including
   the 41 existing scenarios (feature 001 and health); `make lint`, `make check-generate`.
10. **Refactor** with both suites green.
11. **Documentation** (Principle V), below.

## Documentation (Principle V, required tasks)

- `docs/architecture.md`: the six routes in the HTTP table, and the sentence saying they are not
  served removed; a "Lifecycle" section (the transaction order, the claim's own-cell rule,
  `ReleaseCells` beside `ClaimPair` as the only writers of `time_slot`, reschedule's reuse of
  booking, the late arrival, complete's `Ceil(now)`); the sweeper in "Process" and in the packages
  table; the 403 rule in "Channel", replacing "401 and 403 are never returned"; `VERSION_CONFLICT`,
  `INVALID_TRANSITION` and `FORBIDDEN` moved from "not produced yet" to produced; `actualStartAt`
  and `actualEndAt` no longer always `null`; the new configuration; the sweeper's observability.
- `docs/features.md`: an "Appointment lifecycle" row linking the three feature files.
- `docs/development.md`: the `@clock` rule (the only database write the suite makes, backdating by
  14 days and why 14, backdated windows never reused, polling with a deadline, never sleeping); the
  `SCHEDULER_TEST_DSN` variable; the `@lifecycle`, `@reschedule`, `@no-show` and `@clock` tags; the
  scenario calendar for days 9 to 12 and the `LDS01` rows; the fixture table rows for `Bay L1` and
  `Jo` and the cell count (6 048). The "Tests" bullet "It never touches the database" becomes the
  `@clock` exception.
- `README.md`: status (lifecycle delivered; listing, availability, authentication and the relay
  still not), the configuration table (`NO_SHOW_GRACE`, `NO_SHOW_SWEEP_INTERVAL`,
  `SCHEDULER_TEST_DSN`), and the `test/acceptance/` line of the layout ("no database access").

## Conflicts found between the sources

| # | Between | Conflict | Resolution |
|---|---|---|---|
| C1 | notes 4.7, 6.1, 6.4 vs decision 2, `docs/design/README.md`, the implemented suite | The notes run the suite in-process with a fake clock, `RunOnce` steps, a harness and a database reset per scenario; the release runs it black-box against a container with the real clock | Decision 2: `@clock` backdating plus polling; unit tests for the exact rules |
| C2 | design 11.4 vs notes 4.4 | Reschedule order: the design claims, releases, then updates with the version check; the notes give every transition booking's shape (appointment row first) | Version-guarded update first (research R1). Same guarantee, earlier and cleaner conflict |
| C3 | notes 4.5 vs the implemented `ClaimPair` | The notes add an `excludeApptID` parameter; the implemented claim has none, and design 11.4 always passes the claimer itself | No parameter: cells owned by the claimer are claimable (R2) |
| C4 | contract `TransitionConflict` vs the operations | The shared 409 response lists "(late arrival) no cells are free" for cancel, start, complete and no-show too | Only check-in produces `SLOT_TAKEN` (R8) |
| C5 | design 4.3 vs contract `checkInAppointment` | Design: "if nothing is free the advisor books a fresh appointment" (no code); contract: `409 SLOT_TAKEN` | `SLOT_TAKEN` with alternatives for every way of finding no pair, including missing cells and no qualified technician, where booking would answer `AVAILABILITY_CHANGED` or `NO_QUALIFIED_TECHNICIAN` (spec Clarifications) |
| C6 | contract (`VERSION_CONFLICT`, `INVALID_TRANSITION`) vs notes 4.6 | The contract gives no precedence when both apply; notes 4.6 detect staleness only as zero rows on the guarded update, which cannot produce the contract's detail ("at version 3; you sent 2") | Compare `expectedVersion` first (detail with both numbers), then the transition table; the guarded update still catches races (spec Clarifications, FR-003) |
| C7 | contract `cancelAppointment` vs the state machine and `expectedVersion` | "Cancelling an already cancelled appointment returns the record unchanged", but `CANCELLED → CANCELLED` is not a transition and a retried cancel carries a stale version | Answer it before the version check (FR-021) |
| C8 | feature 001's release statement vs contract "Staff only" | `docs/architecture.md`: "401 and 403 are never returned"; the contract marks check-in and the reschedule pins as staff only, design 6 also start, complete and no-show | 403 `FORBIDDEN` for customer channels on staff actions (spec Clarifications). Docs updated |
| C9 | decision 2 vs the fixture and the implemented working-time rule | Decision 2 lists the late arrival among behaviours tested by backdating, but the late arrival claims cells at the current time, which exist on `MAN01` only from 07:00Z to 17:00Z; and `MAN01` cannot get cells around the clock, because working time is any cell of the dealership (feature 001's `OUTSIDE_OPENING_HOURS` at 06:00 and 19:00 would break) | `LDS01` gets `Bay L1` and `Jo` with cells around the clock (R10, R13). Its late arrival uses the current day at `LDS01`, outside the "days 9 to 12" calendar, which governs `MAN01` |
| C10 | `ledger_guard_test.go` vs the `@clock` step | The guard fails on `UPDATE time_slot` in any non-test `.go` file; `test/acceptance` keeps its steps in non-test files | The backdating step goes in `steps_clock.go`; the guard is unchanged (R10) |
| C11 | design 4.3, notes 4.7 vs decision 2 | The design's job "runs every minute"; the notes hard-code `time.Minute` and build the sweeper over the store with its own transaction | Interval and grace are configuration (defaults 1 minute and 30 minutes); the sweeper calls the service's `MarkNoShow` (R7) |
| C12 | contract `RescheduleRequest` | `minProperties: 2` admits `{expectedVersion, reason}`, which changes nothing | 400 `VALIDATION_ERROR` naming `startAt` (FR-002) |
| C13 | contract `CheckInRequest` vs the schema | `mileage` and `additionalConcerns` have no column and no field on `Appointment` | Event payload only (FR-007) |
| C14 | contract "safe to repeat" vs reschedule | The booking key includes the start; after a move, a replay of the original `POST` books a new appointment at the old time | Accepted and stated as an edge case; not changed |

## Open items for the reviewer

- **Behavior gate**: the scenarios need the user's approval before implementation (Principle I).
- **403 for customer channels** (C8): a policy choice the contract implies but that authentication
  has not arrived to back; overturning it removes FR-008, two scenarios and one error.
- **`LDS01` fixture rows** (C9): the only fixture change, needed only by the late-arrival scenario.
- **Reschedule onto a cancelled appointment's key is `DUPLICATE_BOOKING`** (FR-015): consistent with
  booking (design 7), but a product owner may prefer to let a move reuse a cancelled minute, which
  needs a schema change (the key covers every status).
- **Late arrival prefers the previous pair and starts at the current cell, not the next**: the
  contract's wording; the next cell would give the advisor a few minutes but contradicts it.

## Review record

**Behavior gate: approved 2026-10-13** by the reviewing session on the user's behalf. The three feature files and the spec's acceptance block were read in full against the design extract, the implementation notes, the contract and the feature 001 code. Accepted as written: the 403 rule for customer channels on staff actions (the contract marks them staff only), the backdating step as the only database write in the suite, the LDS01 bay, technician and round-the-clock cells (the only deterministic way to claim cells at the current time), late arrival starting at the current grid cell, and a reschedule onto a cancelled appointment's key answering DUPLICATE_BOOKING (consistent with booking). One instruction for the Red phase: the ledger guard should exclude the `test/` tree explicitly rather than rely on the backdate step living in a `_test.go` file.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|---|---|---|
| A new package `internal/scheduling/sweeper` (service, report, worker, a one-method `marker` interface) | FR-051 needs a job on an interval; notes 2 and 4.7 place it there; the `marker` interface lets it use the service's own `MarkNoShow` with its middleware | A goroutine in `main` with its own SQL: a second copy of the no-show transaction and no unit-test seam for the grace boundary |
| `scheduling.Keep` and `Tx.ReleaseCells` with three keep rules | Four transitions release cells differently (all; all outside the new runs; all from `Ceil(now)`); notes 4.5 specify this one method | Three release methods: three locking reads and three tests for one filter |
| `Store.DayLedger` gains an `owner` parameter (booking passes `""`; mocks regenerate) | Reschedule's candidates and alternatives must see the appointment's own cells as free (design 5.4) | Merging owned cells into islands in Go: more code, and loads would still count them |
| Booking's `plan` gains `owner`, `pinBay`, `pinTech`; the current pair is tried first | Decision 1 and design 5.4: a move re-validates exactly what booking does; one implementation of the rules | A separate reschedule flow: a second copy of the grid, horizon, working-time and candidate rules |
| 403 `FORBIDDEN` for customer channels on staff actions | Contract "Staff only"; the channel claim's stand-in is the only signal there is | Allowing every channel: customers could check in, complete or pin technicians, contradicting the contract |
| `expectedVersion` compared before the transaction and again by the guarded update | The pre-check gives the contract's detail and the right precedence; the guarded update closes the race (notes 4.6) | Guarded update only: no current version for the detail, and staleness would be found after the transition table, giving `INVALID_TRANSITION` to stale callers |
| `@clock` backdating step with a database connection in the acceptance suite, and `SCHEDULER_TEST_DSN` | Decision 2: time-dependent behaviour cannot be reached black-box with the real clock otherwise | A clock or trigger endpoint: forbidden by decision 2. Waiting 30 real minutes: not a test |
| Polling with a 10-second deadline | The sweeper acts asynchronously; the public record is the only observation allowed | A fixed sleep: forbidden by the Testing Standards, and slower |
| `LDS01` resources with cells around the clock (2 688 rows) | The late arrival claims cells at the current time; without them its outcome depends on the hour the suite runs | Cells around the clock on `MAN01`: changes working time for feature 001's `OUTSIDE_OPENING_HOURS` scenarios |
| Two configuration variables (`NO_SHOW_GRACE`, `NO_SHOW_SWEEP_INTERVAL`) | Decision 2; design 4.3 leaves the grace period as an open question with a default | Constants: the suite could not run the sweeper every second |
| `VersionConflictError` carrying both versions | Contract detail "Appointment is at version 3; you sent 2" | A plain sentinel: loses the numbers the client needs |
| `plan` gains `channel`, `requested`, `prefer`; `claim` takes an attempt closure and returns `errNoPair` / `errCellsGone` for `Book`, `Reschedule` and the late arrival to map | Three callers share one candidate loop with different transactions and different final answers (implementer decision, accepted at review) | Three copies of the loop, or a loop that knows every caller's answer |
| `change` + `apply` as the single transition helper; `eventPayload` and `placement` types | Six transitions share one statement order (guarded update, claim, release, event, outbox) and one payload shape | Six hand-written transactions that could drift in order |
| Generic `commandRequest[C]`, `makeCommandEndpoint[C]`, `decodeTransition[C]`; `staffOnly`; `checkPins`, `restrict` on `plan` | Six operations with the same envelope (`expectedVersion`, channel, path id) and the same 403 rule | Six copies of decoder and endpoint |
