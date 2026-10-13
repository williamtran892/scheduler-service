---

description: "Task list for feature 002: Appointment lifecycle"
---

# Tasks: Appointment lifecycle

**Input**: Design documents from `/specs/002-appointment-lifecycle/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Required. The constitution mandates integration-first Gherkin (Principle I) and strict TDD with an approval gate (Principle II). Every test task comes before the code task it drives, and a task that writes unit tests is done only when the tests are **written, seen failing**.

**Organization**: Phases follow plan.md "Approach and order of work": Behavior gate → schema and fixture → step definitions → unit tests → approval gate → Green layer by layer → Green gate → Refactor → Docs → Review. The six operations share one transition helper, one ledger and booking's candidate search, so stories cannot ship below their layers; each task carries the labels of the stories it serves, and the traceability table at the end maps stories to scenarios and tasks.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an incomplete task)
- **[USn]**: user story from spec.md: US1 run the day (check-in, start, complete), US2 cancel and no-show, US3 reschedule and reassign, US4 sweeper and late arrival, US5 explicit refusals

## Path Conventions

Single Go module at the repository root: `cmd/`, `internal/`, `local/mysql/`, `features/`, `test/acceptance/`, `scripts/`, `docs/`.

---

## Phase 0: Behavior gate (done)

- [x] T000 Scenarios in features/lifecycle.feature, features/reschedule.feature, features/noshow.feature approved (plan.md "Review record", 2026-10-13)

---

## Phase 1: Setup (schema, fixture, configuration of the test run)

**Purpose**: the database and compose configuration the suite and the service run against (plan step 2; research R10, R13; data-model.md).

- [x] T001 Copy `actual_start_at DATETIME(6) NULL` and `actual_end_at DATETIME(6) NULL` with their comments verbatim from docs/design/schema.sql into the `appointment` table of local/mysql/schema.sql, between `end_at` and `service_bay_id`
- [x] T002 Add to local/mysql/data.sql, for dealership `LDS01` only: bay `Bay L1` (`b0000000-0000-4000-8000-000000000011`, GENERAL, ACTIVE), technician `Jo` (`e0000000-0000-4000-8000-000000000011`, ACTIVE), Jo's GENERAL_SERVICE skill "valid from `CURRENT_DATE - 1 YEAR`, no end", and FREE cells for both "`CURRENT_DATE` to `CURRENT_DATE + 13`, every 15-minute cell 00:00Z to 23:45Z: 2 x 14 x 96 = 2 688 rows"; update the LDS01 comment to say why
- [x] T003 Verify the fixture: `make db-up`; `time_slot` holds 6 048 rows, 2 688 of them at LDS01, 3 360 at MAN01; `appointment` has the two new columns; `make db-reset`; `make down`. Done 2026-10-13: 6 048 cells (MAN01 3 360, LDS01 2 688 from 00:00Z to 23:45Z), `Bay L1`, `Jo` and Jo's skill present, both columns `datetime(6)` nullable
- [x] T004 [P] [US4] Set `NO_SHOW_SWEEP_INTERVAL: 1s` on the `app` service in docker-compose.yml
- [x] T005 [P] [US4] Export `SCHEDULER_TEST_DSN` (the compose MySQL on `127.0.0.1:${MYSQL_PORT}`, `parseTime=true&loc=UTC&time_zone=%27%2B00%3A00%27`) to the suite in scripts/integration.sh
- [x] T006 Regression on the new fixture: `GODOG_TAGS=@spec-001 ./scripts/integration.sh -v` and `GODOG_TAGS=@bootstrap ./scripts/integration.sh -v` stay green; a failure is fixed in the fixture, never in a scenario. Done: 39 and 2 scenarios passed

---

## Phase 2: Red, acceptance (step definitions)

**Purpose**: the vocabulary of contracts/acceptance-steps.md (plan step 3; research R10). After this phase every `@spec-002` scenario fails on a missing route, or on its observable consequence, and none on an undefined step.

- [x] T007 [P] Extend the world: last record seen per appointment id (the version transitions send), appointments by customer name, a prepared request that targets any path, the instants a request was sent and answered, in test/acceptance/world.go
- [x] T008 [P] Add `Bay L1` and `Jo` to test/acceptance/fixture.go
- [x] T009 [US2] [US3] [US4] [US5] Extend the booking steps in test/acceptance/steps_booking.go: `"<customer>" has booked ... at dealership "<CODE>"`, remember each booking by customer, `the appointment of "<customer>" is fetched through "<CHANNEL>"`, `the request has "<field>" set to <integer>`
- [x] T010 [US1] [US2] [US3] [US4] [US5] Transition steps in test/acceptance/steps_lifecycle.go: check in, start (also `with a start time of <HH:MM> <day>`), complete, cancel, mark a no-show, each with optional `of "<customer>"`, `with version N`, `through "<CHANNEL>"`; reschedule to `<HH:MM> <day>`; reassign to technician, to bay, to bay with technician; `Given` forms asserting 200; `the appointment has reached "<STATUS>"`; `appointment "<uuid>" is cancelled`; `a "<operation>" request is prepared for the appointment`; `the transition request is sent`
- [x] T011 [US1] [US2] [US4] Record assertions in test/acceptance/steps_lifecycle.go: `owns no ledger cells`, `owns N bay cells and M technician cells`, actual start and end present or null, `the appointment's actual start is <HH:MM> <day>`, `the audit trail has exactly one "<TYPE>" event by "<actor>"`
- [x] T012 [US4] Time steps in test/acceptance/steps_clock.go: `@clock` backdating (one transaction over `SCHEDULER_TEST_DSN`: `start_at`, `end_at` and the owned cells' `slot_start` minus N days; exactly one appointment row; the connection opens on first use, so a run without `@clock` scenarios needs no DSN), polling `becomes "<STATUS>" within N seconds` (GET every 200 ms, no fixed sleep), `starts at the current grid cell` (one cell of tolerance)
- [x] T013 Make internal/scheduling/mysql/ledger_guard_test.go skip the `test/` tree explicitly (test code, not production code) and add a guard self-test that a production file writing `time_slot` outside ledger.go is still reported while a file under `test/` is not
- [x] T014 Register the new steps in test/acceptance/suite_test.go; `go vet ./...` and `gofmt -l .` clean
- [x] T015 Red gate (acceptance): `GODOG_TAGS=@spec-002 ./scripts/integration.sh -v` fails every scenario on a missing route (404) or its consequence (the sweeper scenarios time out polling), none on an undefined or pending step; `@spec-001` and `@bootstrap` stay green. Done 2026-10-13: 50 of 50 `@spec-002` scenarios failed, 49 on a 404 from a missing route and the sweeper scenario on its polling deadline (no sweeper yet, after the backdating step succeeded); no undefined or pending step; `@spec-001` 39 passed, `@bootstrap` 2 passed. The first run exposed a calendar clash in features/noshow.feature: the booking key (vehicle, customer, start, kind) ignores the dealership, so Alice's LDS01 bookings at 10:00 in 9 days and 13:45 in 10 days collided with her MAN01 bookings in lifecycle.feature (409 `DUPLICATE_BOOKING`), and 10:00 in 11 days would collide with reschedule.feature's move to that time. The LDS01 bookings moved to evening hours, when MAN01 has no cells (scenario steps and calendar comment only; names and assertions unchanged)

---

## Phase 3: Red, unit tests (with compile-only declarations)

**Purpose**: every unit test plan.md step 4 to 8 lists. Production files get only the exported declarations the tests reference: bodies `panic("not implemented")`, sentinel errors without a code, constructors that return stubs, `MakeServerEndpoints`/`RegisterRoutes` that add nothing yet, so every failure happens inside the code under test, not in setup.

- [x] T016 Declare the stubs: `Appointment.ActualStartAt/ActualEndAt`, `AppointmentStatus.CanTransitionTo`, `Grid.Floor`/`Ceil`, `ResourceRun`, `Keep` and `Keep.Keeps`, `ErrStaleVersion`, `ErrInvalidTransition`, `ErrForbidden`, `VersionConflictError`, `Store.DayLedger(..., owner)`, `Store.DueNoShows`, `Tx.UpdateAppointment`, `Tx.ReleaseCells` in internal/scheduling/{appointment,enums,grid,errors,repository}.go; the store stubs in internal/scheduling/mysql/{ledger,appointments}.go (booking keeps passing `""` as the owner); the six `Service` methods, their commands, endpoint fields and middleware methods in internal/scheduling/service/{service,lifecycle,endpoint,middleware}.go; `Config.NoShowGrace`/`NoShowSweepInterval` in internal/platform/config.go; package internal/scheduling/sweeper (`Service`, `Report`, `Appointments`, `Marker`, `NewService`, `Worker`, `NewWorker`); `//go:generate go tool mockgen` directives and `make generate`
- [x] T017 [P] [US1] [US4] Grid tests in internal/scheduling/grid_test.go: `Floor` is the start of the cell containing t (09:40 → 09:30, 09:30 → 09:30); `Ceil` is t on the grid, else the next cell start (09:40 → 09:45, 09:45 → 09:45, 09:45:00.5 → 10:00). Done: written, seen failing
- [x] T018 [P] [US5] Transition table tests in internal/scheduling/enums_test.go: all 36 (from, to) pairs against notes 5.4's table (`CONFIRMED` → `CONFIRMED`, `CHECKED_IN`, `CANCELLED`, `NO_SHOW`; `NO_SHOW` → `CHECKED_IN`; `CHECKED_IN` → `IN_PROGRESS`, `CANCELLED`; `IN_PROGRESS` → `COMPLETED`; nothing else). Done: written, seen failing
- [x] T019 [P] [US1] [US2] [US3] Release filter tests in internal/scheduling/appointment_test.go (`Keep.Keeps`): zero `Keep` keeps nothing; kept runs keep only cells on the same resource kind and id inside the run (a reassigned technician's old cells at the same time are released); `Before` keeps cells starting before it (complete at 09:40 on a 09:00 to 10:00 job keeps 09:00, 09:15, 09:30 and releases 09:45). Done: written, seen failing
- [x] T020 [P] [US5] Error tests in internal/scheduling/errors_test.go: `ErrStaleVersion` is `VERSION_CONFLICT`, `ErrInvalidTransition` is `INVALID_TRANSITION`, `ErrForbidden` is `FORBIDDEN`; `VersionConflictError{Current: 3, Sent: 2}` is `ErrStaleVersion`, code `VERSION_CONFLICT`, message "Appointment is at version 3; you sent 2." Done: written, seen failing
- [x] T021 [P] [US4] Configuration tests in internal/platform/config_test.go: `NO_SHOW_GRACE` default `30m`, `NO_SHOW_SWEEP_INTERVAL` default `1m`, both parsed as Go durations, zero, negative or unparsable rejected naming the variable. Done: written, seen failing
- [x] T022 [P] [US1] View tests in internal/scheduling/service/view_test.go: `actualStartAt` and `actualEndAt` rendered in the dealership's offset when set, `null` when not, for both channels. Done: written, seen failing
- [x] T023 [US5] Order-of-checks tests in internal/scheduling/service/lifecycle_test.go, for each of the six methods: customer channel on a staff action → `ErrForbidden` with no store call; unknown id → `ErrNotFound`; stale `expectedVersion` → `VersionConflictError{Current, Sent}` and no transaction; wrong status → `ErrInvalidTransition` and no transaction; stale beats an illegal transition; `ErrStaleVersion` from `UpdateAppointment` (a race) → `VERSION_CONFLICT`. Done: written, seen failing
- [x] T024 [US2] Cancel and no-show tests in internal/scheduling/service/lifecycle_test.go: one `InTx` in the order update (status, version + 1, `updatedAt`) → `ReleaseCells(id, Keep{})` → event `CANCELLED`/`NO_SHOW` (actor `anonymous`, request channel, `reason`) → outbox `AppointmentCancelled`/`AppointmentNoShow`; cancel from `CHECKED_IN` too; cancel of a `CANCELLED` appointment returns it unchanged whatever the version, with no transaction; customer channels may cancel; the actor and empty channel of a `no-show-sweeper` command are recorded. Done: written, seen failing
- [x] T025 [US1] Check-in, start and complete tests in internal/scheduling/service/lifecycle_test.go: check-in from `CONFIRMED` is update → event `CHECKED_IN` with `mileage` and `additionalConcerns` → outbox, no ledger call; start sets `ActualStartAt` to `startedAt` or now (µs), refuses `startedAt` after now with `INVALID_VALUE` naming `startedAt`, no ledger call; complete sets `ActualEndAt`, refuses `completedAt` after now or before the actual start (`INVALID_VALUE` naming `completedAt`), and releases with `Keep{Before: Ceil(now)}` whatever `completedAt` says. Done: written, seen failing
- [x] T026 [US4] Late-arrival tests in internal/scheduling/service/lifecycle_test.go: from `NO_SHOW` the new start is `Floor(now)`, the end start + duration, the previous pair is tried first, then least-loaded; one `InTx` per pair: update (`CHECKED_IN`, new times and pair) → `ClaimPair(..., id)` → event `CHECKED_IN` → outbox `AppointmentCheckedIn`, no release; a taken pair moves on; no free pair, no candidate, or no qualified technician → `SlotTakenError` with alternatives; no horizon check. Done: written, seen failing
- [x] T027 [US3] Reschedule tests in internal/scheduling/service/reschedule_test.go: booking's rules on the new start (`OFF_GRID_START`, `INVALID_VALUE` on `startAt` in the past or beyond the horizon, `OUTSIDE_OPENING_HOURS`, `SERVICE_NOT_BOOKABLE_ONLINE` for a customer channel); a pure reassign keeps the start; pinned bay unknown or of a wrong type → `INVALID_VALUE` naming `serviceBayId`; pinned technician unknown → `INVALID_VALUE` naming `technicianId`, unqualified → `NO_QUALIFIED_TECHNICIAN`; pins through a customer channel → `ErrForbidden`; `DayLedger` asked with the appointment as owner; current pair first; transaction order update → claim → `ReleaseCells(id, Keep{the new bay run, the new technician run})` → event `RESCHEDULED` with previous and new → outbox `AppointmentRescheduled`; taken pair → next; deadlock → same pair once; cells missing → recompute once → `AvailabilityChangedError`; no pair → `SlotTakenError`; `ErrDuplicateBooking` from the update → `DuplicateBookingError` carrying the key's owner. Done: written, seen failing
- [x] T028 [US3] Update internal/scheduling/service/fixture_test.go for `DayLedger`'s owner (recorded per call) and the new `Tx` methods; feature 001's booking tests assert the owner is `""` and still pass
- [x] T029 [P] [US5] Transport tests in internal/scheduling/service/transport_test.go for the six routes with a mock `Service`: 200 with the record in the caller's view and the decoded command (id, `expectedVersion`, `reason`, channel, `startAt`, pins, `mileage`, `additionalConcerns`, `startedAt`, `completedAt`); 400 `VALIDATION_ERROR` for malformed JSON, missing or non-integer `expectedVersion`, a date-time without offset, a non-UUID pin, a reschedule naming nothing (field `startAt`), a missing `X-Channel`; 422 `INVALID_VALUE` for `expectedVersion` < 1, negative `mileage`, `reason` over 500 or `additionalConcerns` over 4000 characters; 404 for a non-UUID path id; 403, 409 `VERSION_CONFLICT`, 409 `INVALID_TRANSITION` from service errors. Done: written, seen failing
- [x] T030 [P] Middleware tests in internal/scheduling/service/middleware_test.go: each new method is logged with method and appointment id, counted with its problem code, and traced as `scheduling.<Method>`. Done: written, seen failing
- [x] T031 [P] [US5] Error coverage in internal/scheduling/service/errors_test.go gains `ErrStaleVersion`, `ErrInvalidTransition`, `ErrForbidden`, `VersionConflictError`. Done: written, seen failing
- [x] T032 [P] [US4] Sweeper tests in internal/scheduling/sweeper/sweeper_test.go with mocks and `FakeClock`: the store is asked for `now - grace`; an appointment with `start + grace == now` is not marked, one a microsecond earlier is; non-`CONFIRMED` rows are skipped; `MarkNoShow` is called once per appointment (its own transaction) with `ExpectedVersion` = its version, actor `no-show-sweeper`, no channel; `VERSION_CONFLICT` and `INVALID_TRANSITION` count as skipped and the run goes on; another error counts as failed and the run goes on; the report counts marked, skipped and failed; the worker runs `RunOnce` on its interval and returns when its context ends. Done: written, seen failing
- [x] T033 Red gate (unit): `make test` shows every new test failing (an assertion or a stub panic inside the code under test), no compile error, feature 001's tests still green; `go vet ./...`, `gofmt -l .` clean. Done 2026-10-13: 59 tests fail (56 new, plus 3 existing ones that now also state the new behaviour: the two whole-`Config` comparisons and the error-coverage list), each on an assertion or a stub panic inside the code under test; no compile error; every other test passes; `make lint` 0 issues

---

## Phase 4: Unit-test approval gate

- [x] T034 User (or the reviewer they delegate to) approves the unit tests of Phase 3 and the step definitions of Phase 2; no production code before this (Principle II). Approved by the reviewing session on the user's behalf, 2026-10-13, after reading the backdating and polling steps, the sweeper tests and the lifecycle test list; the evening times in noshow.feature are accepted (booking keys are global across dealerships).

---

## Phase 5: Green, scheduling root and platform

- [ ] T035 [P] Implement `Grid.Floor`, `Grid.Ceil` in internal/scheduling/grid.go until T017 passes
- [ ] T036 [P] Implement the transition table and `CanTransitionTo` in internal/scheduling/enums.go until T018 passes
- [ ] T037 [P] Implement `Keep.Keeps` in internal/scheduling/appointment.go until T019 passes
- [ ] T038 [P] Give the new errors their codes and implement `VersionConflictError` in internal/scheduling/errors.go until T020 and T031 pass
- [ ] T039 [P] Read `NO_SHOW_GRACE` and `NO_SHOW_SWEEP_INTERVAL` in internal/platform/config.go until T021 passes

---

## Phase 6: Green, scheduling MySQL

- [ ] T040 [US1] [US3] [US4] Read and write `actual_start_at`, `actual_end_at`; implement `Tx.UpdateAppointment` (`UPDATE ... WHERE id = ? AND version = ?`, zero rows → `ErrStaleVersion`, 1062 on `uq_appointment_booking` → `ErrDuplicateBooking`) and `Store.DueNoShows` (`CONFIRMED`, `start_at < ?`, oldest first) in internal/scheduling/mysql/appointments.go
- [ ] T041 [US2] [US3] Implement `Tx.ReleaseCells` in internal/scheduling/mysql/ledger.go per research R3 (lock by `appointment_id` `FOR UPDATE`, filter with `Keep.Keeps`, one `UPDATE time_slot SET status = 'FREE', appointment_id = NULL, version = version + 1` per resource); the claim treats cells owned by the claimer as claimable (R2); `DayLedger` counts the owner's cells as free and leaves them out of the loads; T013 still passes

---

## Phase 7: Green, scheduling service

- [ ] T042 [US1] [US2] [US5] Implement the transition helper (FR-003 order, version-guarded update first, event, outbox) and `Cancel`, `CheckIn` from `CONFIRMED`, `Start`, `Complete`, `MarkNoShow` in internal/scheduling/service/lifecycle.go until T023 to T025 pass
- [ ] T043 [US3] [US4] Generalise booking's plan (owner, pins, current pair first; alternatives with the owner) in internal/scheduling/service/booking.go and implement `Reschedule` and the late-arrival placement in internal/scheduling/service/reschedule.go until T026 to T028 pass; feature 001's booking tests still pass
- [ ] T044 [P] [US1] Render `actualStartAt`, `actualEndAt` in internal/scheduling/service/view.go until T022 passes
- [ ] T045 [US5] Implement the six endpoints, decoders and routes (`POST /appointments/{appointmentId}/reschedule|cancel|check-in|start|complete|no-show`) in internal/scheduling/service/endpoint.go and transport.go until T029 passes
- [ ] T046 [P] Implement the six middleware methods in internal/scheduling/service/middleware.go until T030 passes

---

## Phase 8: Green, sweeper and wiring

- [ ] T047 [US4] Implement `RunOnce` and `Worker.Run` in internal/scheduling/sweeper/sweeper.go until T032 passes
- [ ] T048 [US4] Wire the sweeper over the wrapped Service and add its worker to the errgroup in cmd/scheduler/main.go, with `NoShowGrace` and `NoShowSweepInterval` from the config

---

## Phase 9: Green gate

- [ ] T049 `make test` green with `-race`
- [ ] T050 `GODOG_TAGS=@spec-002 ./scripts/integration.sh` green (50 executed scenarios), then `make test-integration` green including `@spec-001` and `@bootstrap`
- [ ] T051 `make lint`, `make vet`, `make check-generate` clean

---

## Phase 10: Refactor gate

- [ ] T052 Review every changed file against Principle III; refactor with both suites green; record any added complexity in plan.md Complexity Tracking

---

## Phase 11: Documentation (Principle V)

- [ ] T053 [P] Update docs/architecture.md per plan.md "Documentation": the six routes, a Lifecycle section (transaction order, own-cell claim, `ReleaseCells`, reschedule's reuse of booking, late arrival, complete's `Ceil(now)`), the sweeper in Process and Packages, the 403 rule in Channel, the produced codes, actual times, configuration
- [ ] T054 [P] Update docs/features.md: an "Appointment lifecycle" row linking features/lifecycle.feature, features/reschedule.feature, features/noshow.feature
- [ ] T055 [P] Update docs/development.md: the `@clock` rule and `SCHEDULER_TEST_DSN`, tags `@lifecycle`, `@reschedule`, `@no-show`, `@clock`, the calendar for days 9 to 12 and LDS01, the fixture rows and the 6 048 cells, the ledger guard's `test/` exclusion, and the calendar rule that a customer's booking keys are global across dealerships (LDS01 scenarios book evening hours)
- [ ] T056 [P] Update README.md: status, configuration table (`NO_SHOW_GRACE`, `NO_SHOW_SWEEP_INTERVAL`, `SCHEDULER_TEST_DSN`), the `test/acceptance/` line

---

## Phase 12: Review gate

- [ ] T057 Measure SC-006 (a transition under 200 ms p95 at 50 req/s) and record it in the pull request
- [ ] T058 Reviewer checks each constitution gate (Behavior, Red, Green, Refactor, Docs) was honoured and plan.md Complexity Tracking justifies every addition

---

## Dependencies & Execution Order

- Phase 1 → Phase 2 → Phase 3 → Phase 4 (gate) → Phases 5 to 8 → Phase 9 → 10 → 11 → 12.
- T006 needs T001 to T003. Within Phase 2, T007 and T008 first, then T009 to T012, then T013 and T014; T015 closes the phase.
- Within Phase 3, T016 comes first (tests need declarations to compile); T017 to T032 are independent except T023 to T028, which share internal/scheduling/service/fixture_test.go; T033 closes the phase.
- Phases 5 to 8 follow the dependency direction: root and platform → mysql → service → sweeper and wiring. T043 needs T035 to T041; T048 needs T042 to T047.
- Phase 11 may start once T050 is green; the feature is not done until it is complete (Docs gate).

## Parallel Opportunities

- Phase 1: T004 and T005 alongside T001 to T003.
- Phase 2: T007 and T008 together.
- Phase 3: T017 to T022 and T029 to T032 are separate files.
- Phase 5: T035 to T039 together.
- Phase 11: T053 to T056 together.

## Traceability: user stories to scenarios and tasks

| Story | Scenarios | Key tasks |
|---|---|---|
| US1 Run the day (P1) | lifecycle.feature: checking in and starting, start time from the dealer system, completing before the booked time | T010, T011, T017, T019, T022, T025, T040, T042, T044 |
| US2 Cancel and no-show (P1) | lifecycle.feature: the cancel scenarios, cancelled key, staff no-show | T010, T011, T019, T024, T041, T042 |
| US3 Reschedule and reassign (P2) | reschedule.feature (12) | T009, T010, T019, T027, T028, T040, T041, T043 |
| US4 Sweeper and late arrival (P2) | noshow.feature (4) | T002, T004, T005, T012, T017, T021, T026, T032, T047, T048 |
| US5 Explicit refusals (P3) | lifecycle.feature: stale version, illegal transitions, customer channels, malformed requests, unknown appointment | T018, T020, T023, T029, T031, T038, T045 |

## Implementation Strategy

MVP is US1 and US2 on the transition helper (Phases 5 to 7 without `Reschedule`): their scenarios turn green first. US3 and US4 follow on the same helper plus booking's generalised plan and the sweeper. Proving it: the Red gates (T015, T033) show every scenario and new unit test failing for the right reason, the approval gate (T034) is passed, and the Green gate (T049 to T051) shows all of them passing.
