# Implementation Plan: Book an appointment

**Branch**: `001-book-appointment` | **Date**: 2026-10-12 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-book-appointment/spec.md`

## Summary

Deliver `bookAppointment` (`POST /appointments`) and `getAppointment` (`GET /appointments/{appointmentId}`) end to end. A booking is validated, checked against its natural booking key, resolved into a service requirement, checked against the grid, the horizon, the ledger's working time and technician qualification, and then claimed: candidate (bay, technician) pairs come from the islands of FREE cells for the day, ordered least-loaded first with a seeded shuffle among ties, and each pair is tried in one transaction that inserts the appointment first, locks and checks the bay run then the technician run with `SELECT ... FOR UPDATE`, marks them BOOKED, and appends the audit event and the outbox row (design 5.2, 11.4; notes 0 D2, D4, 4.4, 4.5). The record carries the owned cell ranges, which is how the black-box acceptance suite sees the ledger. The schema's 15 tables are copied into `local/mysql/schema.sql`, and a relative-date fixture in `local/mysql/data.sql` supplies setup, customers and FREE cells. Technical decisions are in [research.md](./research.md).

## Technical Context

**Language/Version**: Go 1.26 (`go.mod`)

**Primary Dependencies**: `database/sql` + `github.com/go-sql-driver/mysql` (hand-written SQL, no ORM); `github.com/go-kit/kit` `endpoint` and `transport/http` (new; a release at least two weeks old, v0.13.0) with the existing `github.com/go-kit/log`; `github.com/google/uuid`; OpenTelemetry (existing); `golang.org/x/sync/errgroup` (existing)

**Storage**: MySQL 8.4 in docker compose; schema and fixture as init scripts (`local/mysql/schema.sql`, `local/mysql/data.sql`); no migrations, no stored routines, no triggers, VARCHAR enumerations validated as Go constants (notes 0 D2, D3; 5.1)

**Testing**: unit tests with `go.uber.org/mock` (mockgen through `go tool mockgen`), `make test`; Gherkin acceptance with `github.com/cucumber/godog`, black-box over HTTP against the containerised service, `make test-integration TAGS=@spec-001`

**Target Platform**: Linux container (Alpine, static binary), one process

**Project Type**: web service (HTTP JSON API)

**Performance Goals**: book < 200 ms p95 at 50 req/s on one node (design 7)

**Constraints**: no false confirmation under any concurrency (R2); correct at MySQL's default REPEATABLE READ; no distributed lock; no test-only endpoints or fake clock in the acceptance path (decision 3); every time stored in UTC

**Scale/Scope**: fixture of 1 tenant, 2 dealerships, 3 bays, 3 technicians, 7 service types, 22 customers, 3 360 ledger cells; 2 operations; 29 acceptance scenarios

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Checked against `.specify/memory/constitution.md` v1.0.0.

| Principle / gate | Pre-research | Post-design | Notes |
|---|---|---|---|
| **I. Integration-first** | PASS (pending approval) | PASS (pending approval) | The four feature files exist before any code, describe behaviour through the HTTP API only, and fail on first run because no step is defined (strict mode). **The Behavior gate is still open: the user has not yet approved the scenarios.** Implementation must not start until they do. Four guarantees cannot be reached black-box in this release and are proven by unit tests only (research.md R16): `AVAILABILITY_CHANGED`, the deadlock retry, statement order in the transaction, and the outbox row. That is a gap in what the acceptance suite proves, not a violation |
| **II. Test-first (non-negotiable)** | PASS | PASS | Order of work below: scenarios (written), then unit tests per package written, approved and seen failing, then code. The approval of unit tests before Green is a gate in the task list |
| **III. Simplicity** | PASS with justified complexity | PASS with justified complexity | Three domains with readers and adapters for one feature, a second booking-key lookup, the `X-Channel` stand-in and three middlewares are recorded in Complexity Tracking. Not built: `setup/service`, `customers/service`, `engagement`, workers, availability endpoint, `NOWAIT`, a cache |
| **IV. Specs are ephemeral** | PASS | PASS | Nothing in code or `docs/` will reference `specs/`. The scenario calendar and the fixture description go into `docs/development.md`; feature files carry only their own calendar line |
| **V. Living documentation** | PASS | PASS | Tasks must update `docs/architecture.md`, `docs/features.md`, `docs/development.md` (and the README status) in the same change; listed under Documentation below |
| Testing Standards: deterministic, controllable clocks | PASS with note | PASS with note | Acceptance scenarios run against the real clock with relative dates (decision 3). No assertion depends on the exact time: past and beyond-horizon cases use "yesterday" and "in 30 days", booked days are all in the future, "today" is fixed once per run. Exact boundaries (start equal to now, start at the horizon) are unit tests with `platform.FakeClock`. Race scenarios assert counts, not winners |
| Development workflow gates | Behavior gate open | Behavior gate open | Red, Green, Refactor, Docs and Review gates apply in the implementation phase |

No unjustified violation. The gate passes for planning; it does not pass for implementation until the Behavior gate closes.

## Project Structure

### Documentation (this feature)

```text
specs/001-book-appointment/
├── spec.md                    # specification, with Clarifications
├── checklists/requirements.md # spec quality checklist
├── plan.md                    # this file
├── research.md                # Phase 0 decisions R1 to R16
├── data-model.md              # tables used, Go types, fixture rows
├── quickstart.md              # how to validate end to end
├── contracts/
│   ├── README.md              # points at api/openapi.yaml; release stand-ins
│   └── acceptance-steps.md    # Gherkin step vocabulary
└── tasks.md                   # /speckit-tasks (not created here)

features/                      # written in this step, failing
├── booking.feature
├── claim_ledger.feature
├── idempotency.feature
└── concurrency.feature
```

### Source Code (repository root)

```text
cmd/scheduler/main.go                    wire setup/customers/scheduling repositories, adapters, service, middleware, routes
internal/
├── app/app.go                           Deps gains the scheduling routes registration; one mux
├── platform/
│   ├── problem.go                       Problem gains alternatives/existing/errors; code→status table; VALIDATION_ERROR,
│   │                                    INVALID_VALUE, NOT_FOUND; ProblemCode()/ProblemExtensions() interfaces; FieldError
│   └── problem_test.go                  every contract ProblemCode has a status
├── setup/                               WORKSHOP SETUP (read-only in this release)
│   ├── model.go enums.go errors.go      Dealership, ServiceBay, Technician, ServiceType; BayType, BayStatus, TechnicianStatus
│   ├── repository.go                    Repository: Dealership, ServiceType (+skills, bay types), Resources(dealership, date)
│   └── mysql/repository.go errors.go    over dealership, service_bay, technician, skill, technician_skill, service_type*
├── customers/                           CUSTOMER & VEHICLE (read-only in this release)
│   ├── model.go repository.go           Customer, Vehicle; Repository
│   └── mysql/repository.go              over customer, vehicle
└── scheduling/                          SCHEDULING
    ├── grid.go                          Grid, CellRun, Align, CellsFor (+ tests)
    ├── requirement.go                   ServiceRequirement.Runs, DealershipConfig, ServiceBay, Technician (+ tests)
    ├── availability.go                  Island, Candidate, FeasibleStarts, CandidatePairs (+ tests incl. split-across-bays)
    ├── appointment.go                   Appointment, BookingKey, CellRange
    ├── enums.go                         AppointmentKind, AppointmentStatus, Channel (+IsCustomer), ResourceKind, CellStatus (+ tests)
    ├── errors.go                        ErrSlotTaken/SlotTakenError, ErrCellsMissing (BayNotOpen, TechnicianNotWorking),
    │                                    ErrOffGrid, ErrDuplicateBooking/DuplicateBookingError, ErrDeadlockVictim,
    │                                    AvailabilityChangedError, ErrNoQualifiedTechnician, ErrOutsideOpeningHours,
    │                                    ErrNotBookableOnline, ErrNotFound, InvalidValueError — each with ProblemCode()
    ├── repository.go                    Store, Tx, WorkshopReader, CustomerReader
    ├── deps_test.go                     root imports only the standard library
    ├── mysql/
    │   ├── db.go                        Store, InTx (span per transaction)
    │   ├── ledger.go                    Tx.ClaimPair: FOR UPDATE, check, UPDATE; DayLedger; WorkingCells; OwnedCells
    │   ├── appointments.go              InsertAppointment, Appointment, AppointmentByBookingKey
    │   ├── events.go outbox.go          AppendEvent, Events, AppendOutbox
    │   ├── errors.go                    1062 on uq_appointment_booking → ErrDuplicateBooking; 1213 → ErrDeadlockVictim
    │   └── ledger_guard_test.go         no time_slot writes outside ledger.go
    ├── adapters/workshop.go customers.go  WorkshopReader over setup.Repository; CustomerReader over customers.Repository
    └── service/
        ├── service.go                   Service{Book, Get}, NewService(store, workshop, customers, clock, ids)
        ├── booking.go                   checks in order (research R4), candidate loop, alternatives
        ├── view.go                      Appointment view per channel (staff/customer)
        ├── endpoint.go transport.go     go-kit endpoints; RegisterRoutes; decoders; encodeCreated with Location
        ├── middleware.go                logging, instrumenting, tracing
        └── *_test.go mock_scheduling_test.go
local/mysql/schema.sql                   15 tables verbatim from docs/design/schema.sql
local/mysql/data.sql                     fixture (data-model.md)
test/acceptance/
├── world.go                             headers, prepared request, appointments, race outcomes
├── fixture.go dates.go                  names→ids; relative dates in Europe/London
├── steps_booking.go steps_record.go steps_race.go
└── suite_test.go                        registers the new steps
docs/architecture.md docs/features.md docs/development.md README.md
```

**Structure Decision**: the single Go module and the go-kit per-domain layout of implementation notes 2 and 4.0, limited to what this feature uses (research.md R12). `scheduling` never imports `setup` or `customers`; only `scheduling/adapters` does. `platform` imports no domain package.

## Approach and order of work

Each step follows Red → approval → Green → Refactor (Principle II). Tests in a step are written, approved and seen failing before the code in that step.

1. **Behavior gate.** User approves `features/booking.feature`, `claim_ledger.feature`, `idempotency.feature`, `concurrency.feature`. Run `make test-integration TAGS=@spec-001` and record that they fail (undefined steps).
2. **Schema and fixture.** Copy the 15 tables into `local/mysql/schema.sql`; write `local/mysql/data.sql` (data-model.md). Verify with `make db-up` and the cell count (3 360) and `make db-reset`.
3. **Step definitions** (`test/acceptance/`) for the vocabulary in contracts/acceptance-steps.md. Scenarios now fail on 404 from the missing routes, not on undefined steps.
4. **Platform problems**: contract `Problem` fields, code→status table, `VALIDATION_ERROR`, `INVALID_VALUE`, `NOT_FOUND` (research R5). Update `health` tests if affected.
5. **Scheduling root**: grid, requirement runs, enums, errors, `FeasibleStarts`, `CandidatePairs` (pure, table and property tests; the split-across-bays case; tie shuffle with fixed seed).
6. **Setup and customers roots and `mysql` repositories**; **scheduling adapters**.
7. **Scheduling `mysql`**: `InTx`, `ClaimPair`, `DayLedger`, `WorkingCells`, appointments, events, outbox, error mapping, the ledger grep guard. These are exercised by the acceptance suite (notes 6.0: locking is not unit-testable).
8. **Scheduling service** `Book` and `Get` with mocks: every branch of research R4, call order inside `InTx`, deadlock retry, recompute then `AVAILABILITY_CHANGED`, duplicate handling and both key lookups, view per channel.
9. **Endpoints, transport, middleware, wiring** in `main.go` and `app.go`; transport table tests with `httptest` for every status and decoder rejection.
10. **Green**: `make test`, `make test-integration TAGS=@spec-001`, then the full suite; `make lint`, `make check-generate`.
11. **Refactor** with both suites green.
12. **Documentation** (Principle V), below.

## Documentation (Principle V, required tasks)

- `docs/architecture.md`: packages `setup`, `customers`, `scheduling` (root, `mysql`, `adapters`, `service`) and the dependency rules; routes `POST /appointments`, `GET /appointments/{appointmentId}`; the booking transaction and the claim; the problem-code table replacing `BAD_REQUEST` with `VALIDATION_ERROR`; the `X-Channel` stand-in; data section listing the 15 tables and the fixture.
- `docs/features.md`: a "Book an appointment" row linking the four feature files; remove "No scheduling features have been delivered yet".
- `docs/development.md`: replace the test-only control endpoint and fake-clock text in "Tests" and "Time" with the black-box rules of decision 3 (relative dates, ledger through the record, races from the suite); add the scenario calendar (days per feature, days 9 to 12 for feature 002) and the fixture's names and ids; add the `@spec-001`, `@slow`, `@design-*`, `@R*` tags.
- `README.md`: status line and the `test/acceptance/` description ("database reset" is not what the suite does).

## Open items for the reviewer

- **Behavior gate**: the scenarios need the user's approval before implementation (Principle I).
- **`/v1` prefix**: the contract's `servers` end in `/v1`; routes are unprefixed here (research R9).
- **`actualStartAt` / `actualEndAt`**: in the contract, absent from the schema; null in this feature; feature 002 must decide, and decision 5 forbids proposing columns here.
- **Outbox relay**: not in this release's stated scope or in this feature; whether feature 002 publishes events is undecided.
- **Service type with no compatible ACTIVE bay**: no contract code; treated as `SLOT_TAKEN` with no alternatives (spec Assumptions).

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|---|---|---|
| Three domains (`setup`, `customers`, `scheduling`) with reader interfaces and an adapters package, for a feature whose only writes are in `scheduling` | Decision 5 and notes 2 (deviation D1): the context boundaries are in the code from the first feature, so later features and the eventual split are moves, not refactors | One `booking` package with inline SQL over all 15 tables: fewer files today, but it contradicts decision 5 and would be split by feature 002 |
| Record view assembled from several reads (appointment, cells, names through two readers) | Each `mysql` package touches only its domain's tables (notes 2) | One join across all tables in `scheduling/mysql`: breaks that rule |
| Booking-key lookup before capacity and again before `SLOT_TAKEN` / `AVAILABILITY_CHANGED`, in addition to the unique index | Without it a replay of a single-pair booking (MOT) is answered `SLOT_TAKEN`, contradicting the contract (research R2) | Lookup only on 1062 (notes 4.4): demonstrably wrong for that case |
| `X-Channel` request header outside the contract | Channel-dependent behaviour must be reachable while authentication is out of scope (research R8) | A body field changes the contract; a default channel would give anonymous callers the staff view |
| `ProblemCode()` / `ProblemExtensions()` interfaces between domain errors and `platform.EncodeError` | Keeps the status table in `platform` without `platform` importing a domain (research R5) | A switch on domain errors in `platform` (notes 4.6): breaks the architecture rule |
| Logging, instrumenting and tracing middleware | Design 7 asks for the `SLOT_TAKEN` rate at confirm and tracing to the database; notes 4.0 | Logging only: loses a design metric for one small file |
