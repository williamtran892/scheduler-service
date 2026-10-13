# Development Standards

These are the day-to-day rules that follow from the [constitution](../.specify/memory/constitution.md).
Where this page and the constitution disagree, the constitution wins. Fix this page.

## Order of work for a feature

1. Write the Gherkin scenarios in `features/`, tag them `@spec-NNN`, and get them approved.
2. Run them and watch them fail: `make test-integration TAGS=@spec-NNN`.
3. Write the unit tests, get them approved, and watch them fail: `make test`.
4. Implement until both layers pass. Refactor with them still green.
5. Update `docs/architecture.md`, `docs/features.md` and this page as needed.

## Tests

- **Unit tests** live next to the code and use no database. Each dependency is an interface
  passed to a constructor. Mocks come from `mockgen` through a `//go:generate go tool mockgen`
  directive and are written to `mock_<name>_test.go` files. Run `make generate` after you change
  an interface. Locking, isolation and unique indexes are not unit-testable; the acceptance
  suite proves them.
- **Acceptance tests** are Gherkin scenarios run by `make test-integration`, which rebuilds the
  `scheduler:local` image, starts it with a fresh MySQL through docker compose, waits until both
  are healthy, runs `test/acceptance` against the container, then removes both containers. The
  suite is a black-box client with these rules:
  - It uses only the HTTP API. It never reads the database to assert and never cleans up, and
    there is no test-only control endpoint, fake clock or worker trigger. The one exception is
    the `@clock` backdating step (see Time below), which writes to the database.
  - Each run starts from `local/mysql/data.sql`, but the database is not reset between scenarios.
    A scenario books its own days and windows (see the calendar below) and never depends on
    another scenario.
  - Dates are relative to the day the suite runs, in Europe/London: `tomorrow`, `yesterday`,
    `in N days`, with times as local `HH:MM`. "Today" is fixed once per run, so a run that crosses
    midnight stays consistent. The fixture's cells start at the database's `CURRENT_DATE` (UTC),
    so the London date is at most one day ahead of them.
  - The ledger is read only through the cell ranges on the appointment record, or through what a
    second booking of the same time is told.
  - A race is N requests released together by closing one channel; outcomes are counted, never
    which request won.
  - Every request sends `X-Channel`, `ADVISOR_CONSOLE` unless a step says otherwise.
- **Time.** Never read the wall clock in logic. Take a `platform.Clock`: unit tests use
  `FakeClock`, so the exact boundaries (a start equal to now, a start at the booking horizon, the
  grace period of the no-show sweeper, the grid cell a late arrival starts in, the cells a
  completion keeps) are unit tests. Acceptance scenarios run against the real clock and assert
  nothing that depends on the exact time: past is `yesterday`, beyond the horizon is `in 30 days`,
  and every booked day is in the future. The scenarios whose outcome does depend on the time
  carry `@clock`, and follow these rules:
  - Only a `@clock` scenario may backdate an appointment ("the appointment is backdated by 14
    days"). The step moves `start_at` and `end_at` and the `slot_start` of every cell the
    appointment owns back by whole days in one transaction over the connection in
    `SCHEDULER_TEST_DSN`, and checks that exactly one appointment row changed. It is the only
    write the suite makes to the database; it never reads the database to assert, and never
    touches `status` or `version`.
  - 14 days puts the cells before the fixture's first cell (`CURRENT_DATE`), so the moved cells
    never collide with an existing cell and a disjoint window stays disjoint. A backdated
    window loses its cells, so no scenario books it again.
  - The outcome is read by polling the public record ("becomes ... within 10 seconds", every
    200 ms, with a deadline). Never sleep for a fixed time. `docker-compose.yml` sets
    `NO_SHOW_SWEEP_INTERVAL: 1s`, so the sweeper ticks ten times inside that deadline.
  - `SCHEDULER_TEST_DSN` is the compose MySQL on the host port
    (`scheduler:scheduler@tcp(127.0.0.1:${MYSQL_PORT})/scheduler?parseTime=true&loc=UTC&time_zone=%27%2B00%3A00%27`).
    `scripts/integration.sh` sets it. The suite opens the connection on first use, so a run
    without `@clock` scenarios needs no DSN, and a `@clock` step run without it fails with a
    message naming `make test-integration`.
  - A late arrival claims cells at the current time, which `MAN01`'s cells (07:00Z to 16:45Z) do
    not cover around the clock, so the late-arrival scenario uses `LDS01`, whose bay and
    technician have cells for every grid cell of the day.
- **Tags.** Every scenario of a feature carries its `@spec-NNN` and a feature tag (`@booking`,
  `@claim-ledger`, `@idempotency`, `@concurrency`, `@bootstrap`, `@lifecycle`, `@reschedule`,
  `@no-show`). Others:

  | Tag | Meaning |
  |---|---|
  | `@spec-NNN` | The feature that owns the scenario; `make test-integration TAGS=@spec-001` runs one feature |
  | `@slow` | Races and sleeps; they still run in CI, and `TAGS='~@slow'` skips them |
  | `@clock` | The outcome depends on the real time: the scenario backdates an appointment, polls for the sweeper, or claims cells at the current time. `make test-integration TAGS=@clock` runs only these |
  | `@design-N.N` | The row or section of `docs/design/SCHEDULING_DESIGN.md` the scenario reproduces |
  | `@R1`, `@R2`, `@R3` | The requirement the scenario proves: resource-constrained booking, real-time availability check, confirmed appointment record (`docs/design/SCHEDULING_DESIGN.md` 1.3) |
  | `@wip` | Allowed on a branch, never on `main` |

- **Names** state behaviour in plain language, for example `TestHealth_UnavailableWhenThePingFails`.

### Scenario calendar

Scheduling scenarios pick their own days, counted from the suite's "today", in disjoint windows
(local times). Feature 001 and the lifecycle and reschedule features book dealership `MAN01`; the
no-show feature books `LDS01`:

| Day | Feature file | Windows |
|---|---|---|
| 1 (`tomorrow`) | `booking.feature` | 08:45 to 11:15, 11:30 to 12:15, 13:00 to 13:45, 15:00 to 15:45; the refusals touch nothing |
| 2 | `booking.feature` | the least-loaded scenario, whole day |
| 3 | `claim_ledger.feature` | 09:00 to 09:45, 10:45 to 12:45, 14:00 to 15:00 |
| 4 | `claim_ledger.feature` | 09:00 to 10:30, 12:00 to 12:45, 13:45 to 15:45 |
| 5 | `claim_ledger.feature` | 09:45 to 12:15, 14:00 to 14:45 |
| 6 | `idempotency.feature` | 09:00 to 16:30 in six disjoint windows |
| 7 | `idempotency.feature`, `concurrency.feature` | 10:00 to 10:45; 13:00 to 13:45; 15:00 to 15:45 |
| 8 | `concurrency.feature` | 09:00 to 15:00 |
| 9 | `lifecycle.feature` (`MAN01`) | 08:00 to 16:45, one window per scenario |
| 9 | `noshow.feature` (`LDS01`) | 18:00 to 19:45 |
| 10 | `lifecycle.feature` (`MAN01`) | 08:00 to 16:45 |
| 10 | `noshow.feature` (`LDS01`) | 18:00 to 20:30 |
| 11 | `reschedule.feature` (`MAN01`) | 09:00 to 16:45, one window per scenario |
| 11 | `noshow.feature` (`LDS01`) | 18:00 to 18:45 |
| 12 | `reschedule.feature` (`MAN01`) | 08:00 to 16:45, one window per scenario |
| 13 and later | free | |

The fixture holds cells for 14 days from the day the database starts, so day 12 is always inside
it. A new feature takes the next free days and records them here and in its own feature file.

**A customer's booking keys are global across dealerships.** The unique index
`uq_appointment_booking` on `(vehicle_id, customer_id, start_at, kind)` ignores the dealership, so
two scenarios that book the same customer's vehicle at the same instant collide even at different
dealerships (`409 DUPLICATE_BOOKING`). `noshow.feature` therefore books `LDS01` in the evening,
18:00 to 20:30, when `MAN01` has no cells and no other scenario books the same customer there. A
backdated window (`@clock`) is never booked again.

### Fixture

`local/mysql/data.sql` loads one tenant, **Northern Motor Group**, with fixed ids. The suite's
`test/acceptance/fixture.go` mirrors them by name, so feature files never contain UUIDs except an
explicitly unknown one.

| Kind | Name | Id | Details |
|---|---|---|---|
| dealership | `MAN01` Manchester Central | `d0000000-0000-4000-8000-000000000001` | Europe/London, 15-minute grid, 28-day booking horizon |
| dealership | `LDS01` Leeds North | `d0000000-0000-4000-8000-000000000002` | Europe/London, 15-minute grid, 28-day booking horizon; one bay, one technician and cells around the clock, which the `@clock` late-arrival scenario needs; it is also where a booking key is submitted at a second dealership |
| bay | `Bay 1`, `Bay 2`, `Bay 3` | `b0000000-0000-4000-8000-00000000000{1,2,3}` | GENERAL, MOT, EV_HV; all ACTIVE |
| technician | `Sam` | `e0000000-0000-4000-8000-000000000001` | GENERAL_SERVICE, EV_HV_L3 |
| technician | `Priya` | `e0000000-0000-4000-8000-000000000002` | GENERAL_SERVICE, MOT_TESTER |
| technician | `Lee` | `e0000000-0000-4000-8000-000000000003` | GENERAL_SERVICE; AIRCON_FGAS expired yesterday |
| bay | `Bay L1` | `b0000000-0000-4000-8000-000000000011` | `LDS01`, GENERAL, ACTIVE |
| technician | `Jo` | `e0000000-0000-4000-8000-000000000011` | `LDS01`, GENERAL_SERVICE valid from a year ago, no end |
| skill | GENERAL_SERVICE, MOT_TESTER, EV_HV_L3, AIRCON_FGAS | `5c000000-0000-4000-8000-00000000000{1,2,3,4}` | valid from a year ago, no end, except Lee's AIRCON_FGAS |
| service type | `INTERIM` | `5e000000-0000-4000-8000-000000000001` | 45 min, no buffers, GENERAL_SERVICE, on GENERAL or MOT bays |
| service type | `MAJOR` | `5e000000-0000-4000-8000-000000000002` | 90 min, buffers 15/15, GENERAL_SERVICE, on GENERAL bays |
| service type | `MOT` | `5e000000-0000-4000-8000-000000000003` | 45 min, MOT_TESTER, on MOT bays |
| service type | `EV_MAJOR` | `5e000000-0000-4000-8000-000000000004` | 120 min, buffers 15/15, EV_HV_L3, on EV_HV bays |
| service type | `EV_CHECK` | `5e000000-0000-4000-8000-000000000005` | 30 min, EV_HV_L3, on GENERAL or EV_HV bays |
| service type | `DIAGNOSTIC` | `5e000000-0000-4000-8000-000000000006` | 50 min, GENERAL_SERVICE, on GENERAL bays, staff only |
| service type | `AIRCON` | `5e000000-0000-4000-8000-000000000007` | 30 min, AIRCON_FGAS, on GENERAL bays; nobody qualifies today |
| customer, vehicle | `Alice`, `YK21 ABC` | `c0000000-0000-4000-8000-000000000001`, `f0000000-0000-4000-8000-000000000001` | Volkswagen ID.4 |
| customer, vehicle | `Bob`, `BD70 XYZ` | `c0000000-0000-4000-8000-000000000002`, `f0000000-0000-4000-8000-000000000002` | Ford Focus |
| customer, vehicle | `Customer 01` to `Customer 20`, `TEST 01` to `TEST 20` | `c0000000-0000-4000-8000-000000000101` to `...120`, `f0000000-0000-4000-8000-000000000101` to `...120` | one vehicle each; twenty distinct booking keys for the races |
| ledger cells | every bay and technician of `MAN01` | primary key | FREE, `CURRENT_DATE` to `CURRENT_DATE + 13`, cell starts 07:00Z to 16:45Z (40 per resource-day, 3 360 in all), which is 08:00 to 17:00 local in both summer and winter time |
| ledger cells | `Bay L1` and `Jo` | primary key | FREE, `CURRENT_DATE` to `CURRENT_DATE + 13`, every cell start 00:00Z to 23:45Z (96 per resource-day, 2 688 in all); the ledger holds 6 048 cells |

Dates in the fixture are relative to `CURRENT_DATE`, so it never goes stale and no fake clock is
needed.

## Code

- `make fmt` and `make lint` must pass. Imports are grouped as standard library, third-party,
  then `scheduler/...`.
- Errors carry enough context to find the cause: `fmt.Errorf("ping MySQL: %w", err)`.
- Only `platform.EncodeError` maps errors to HTTP status codes. A domain error exposes its
  problem code through `ProblemCode()`, and `platform` looks the status up in one table.
- The Scheduling root imports only the standard library; Scheduling reaches the other domains
  through the reader interfaces it declares, which `setup/mysql` and `customers/mysql` implement
  (see [System Architecture](./architecture.md)).
- Only `internal/scheduling/mysql/ledger.go` writes `time_slot` (`ClaimPair` and `ReleaseCells`);
  a unit test enforces it for production code. It skips `_test.go` files and the `test/` tree,
  where the acceptance suite's `@clock` step backdates cells, and it checks that it still reports
  a production writer.
- Only the schema file creates tables. Add tables to `local/mysql/schema.sql` and fixture rows
  to `local/mysql/data.sql`.

## Before you push

```bash
make ci
```
