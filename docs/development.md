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
  - It uses only the HTTP API. It never touches the database, not even for checks or cleanup,
    and there is no test-only control endpoint, fake clock or worker trigger.
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
  `FakeClock`, so the exact boundaries (a start equal to now, a start at the booking horizon) are
  unit tests. Acceptance scenarios run against the real clock and assert nothing that depends on
  the exact time: past is `yesterday`, beyond the horizon is `in 30 days`, and every booked day is
  in the future.
- **Tags.** Every scenario of a feature carries its `@spec-NNN` and a feature tag (`@booking`,
  `@claim-ledger`, `@idempotency`, `@concurrency`, `@bootstrap`). Others:

  | Tag | Meaning |
  |---|---|
  | `@spec-NNN` | The feature that owns the scenario; `make test-integration TAGS=@spec-001` runs one feature |
  | `@slow` | Races and sleeps; they still run in CI, and `TAGS='~@slow'` skips them |
  | `@design-N.N` | The row or section of `docs/design/SCHEDULING_DESIGN.md` the scenario reproduces |
  | `@R1`, `@R2`, `@R3` | The requirement the scenario proves: resource-constrained booking, real-time availability check, confirmed appointment record (`docs/design/SCHEDULING_DESIGN.md` 1.3) |
  | `@wip` | Allowed on a branch, never on `main` |

- **Names** state behaviour in plain language, for example `TestHealth_UnavailableWhenThePingFails`.

### Scenario calendar

All scheduling scenarios book dealership `MAN01` and pick their own days, counted from the suite's
"today", in disjoint windows (local times):

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
| 9 to 12 | free for feature 002 (appointment lifecycle) | |

The fixture holds cells for 14 days from the day the database starts, so day 8 is always inside it.
A new feature takes the next free days and records them here and in its own feature file.

### Fixture

`local/mysql/data.sql` loads one tenant, **Northern Motor Group**, with fixed ids. The suite's
`test/acceptance/fixture.go` mirrors them by name, so feature files never contain UUIDs except an
explicitly unknown one.

| Kind | Name | Id | Details |
|---|---|---|---|
| dealership | `MAN01` Manchester Central | `d0000000-0000-4000-8000-000000000001` | Europe/London, 15-minute grid, 28-day booking horizon |
| dealership | `LDS01` Leeds North | `d0000000-0000-4000-8000-000000000002` | no bays, technicians or cells; it exists so a booking key can be submitted at a second dealership |
| bay | `Bay 1`, `Bay 2`, `Bay 3` | `b0000000-0000-4000-8000-00000000000{1,2,3}` | GENERAL, MOT, EV_HV; all ACTIVE |
| technician | `Sam` | `e0000000-0000-4000-8000-000000000001` | GENERAL_SERVICE, EV_HV_L3 |
| technician | `Priya` | `e0000000-0000-4000-8000-000000000002` | GENERAL_SERVICE, MOT_TESTER |
| technician | `Lee` | `e0000000-0000-4000-8000-000000000003` | GENERAL_SERVICE; AIRCON_FGAS expired yesterday |
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

Dates in the fixture are relative to `CURRENT_DATE`, so it never goes stale and no fake clock is
needed.

## Code

- `make fmt` and `make lint` must pass. Imports are grouped as standard library, third-party,
  then `scheduler/...`.
- Errors carry enough context to find the cause: `fmt.Errorf("ping MySQL: %w", err)`.
- Only `platform.EncodeError` maps errors to HTTP status codes. A domain error exposes its
  problem code through `ProblemCode()`, and `platform` looks the status up in one table.
- A domain root imports only the standard library; domains reach each other through reader
  interfaces and `adapters` packages (see [System Architecture](./architecture.md)).
- Only `internal/scheduling/mysql/ledger.go` writes `time_slot`; a unit test enforces it.
- Only the schema file creates tables. Add tables to `local/mysql/schema.sql` and fixture rows
  to `local/mysql/data.sql`.

## Before you push

```bash
make ci
```
