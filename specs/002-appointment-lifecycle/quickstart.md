# Quickstart: validate "Appointment lifecycle"

How to prove the feature works end to end once it is implemented. Shapes are in
`api/openapi.yaml` (operationIds `rescheduleAppointment`, `cancelAppointment`,
`checkInAppointment`, `startAppointment`, `completeAppointment`, `markNoShow`); release stand-ins
and codes are in [contracts/README.md](./contracts/README.md); fixture ids are in
[data-model.md](./data-model.md).

## Prerequisites

Go 1.26+, Docker with the compose plugin, `curl`, `jq`. See the repository `README.md`.

## 1. The acceptance scenarios

```bash
make test-integration TAGS=@spec-002                # the three feature files of this feature
make test-integration TAGS=@clock                   # only the time-dependent scenarios
make test-integration                               # everything: feature 001 must stay green
```

Expected: every `@spec-002` scenario passes, and the 41 existing scenarios (feature 001 and health) still pass. Before
implementation they fail with undefined steps (strict mode), which is the Red gate for
Principle I. `scripts/integration.sh` passes `SCHEDULER_TEST_DSN` to the suite; the `@clock`
scenarios fail with a clear message without it.

## 2. Unit tests

```bash
make generate   # mocks: scheduling.Store, Tx, WorkshopReader, CustomerReader; service.Service
make test       # -race, no database
```

Expected: green, including what only unit tests can reach: the grace boundary (`start + grace`
equal to now is not yet a no-show), the current grid cell of a late arrival, the split of cells
on complete at a time inside the job (design 11.3: complete at 09:40 on a 09:00 to 10:00 job keeps
3 cells per resource and frees 1), the deadlock retry and `AVAILABILITY_CHANGED` on a reschedule,
the statement order inside each transition, event and outbox payloads, and the transition table.

## 3. By hand

```bash
make up
DAY=$(TZ=Europe/London date -v+9d +%F 2>/dev/null || TZ=Europe/London date -d '+9 days' +%F)
H='-H Content-Type:application/json -H X-Channel:ADVISOR_CONSOLE'

ID=$(curl -s $H localhost:8080/appointments -d '{
  "dealershipId":"d0000000-0000-4000-8000-000000000001",
  "serviceTypeId":"5e000000-0000-4000-8000-000000000003",
  "customerId":"c0000000-0000-4000-8000-000000000001",
  "vehicleId":"f0000000-0000-4000-8000-000000000001",
  "startAt":"'"$DAY"'T09:00:00+01:00","kind":"DROP_OFF"}' | jq -r .id)

# Move it an hour later (version 1 -> 2); the cells move with it
curl -s $H localhost:8080/appointments/$ID/reschedule \
  -d '{"startAt":"'"$DAY"'T10:00:00+01:00","expectedVersion":1}' | jq '{status,version,startAt,cells}'

# Check in (2 -> 3), start (3 -> 4), complete (4 -> 5): cells released, actual times set
curl -s $H localhost:8080/appointments/$ID/check-in -d '{"expectedVersion":2,"mileage":41230}' | jq '{status,version}'
curl -s $H localhost:8080/appointments/$ID/start    -d '{"expectedVersion":3}' | jq '{status,version,actualStartAt}'
curl -s $H localhost:8080/appointments/$ID/complete -d '{"expectedVersion":4}' | jq '{status,version,actualEndAt,cells}'

# A stale version and an illegal transition
curl -s $H localhost:8080/appointments/$ID/cancel -d '{"expectedVersion":1}' | jq '{status,code,detail}'   # 409 VERSION_CONFLICT
curl -s $H localhost:8080/appointments/$ID/cancel -d '{"expectedVersion":5}' | jq '{status,code,detail}'   # 409 INVALID_TRANSITION

# The audit trail: CONFIRMED, RESCHEDULED, CHECKED_IN, STARTED, COMPLETED
curl -s -H X-Channel:ADVISOR_CONSOLE "localhost:8080/appointments/$ID?include=events" | jq '[.events[].type]'
make down
```

The sweeper is visible the same way: book an appointment, move its time into the past with the
`@clock` step's two statements in `make db-shell`, and within `NO_SHOW_SWEEP_INTERVAL` (one second
under compose) `GET` shows `NO_SHOW`, no cells and an event by `no-show-sweeper`.

## 4. Gates

```bash
make ci   # check-generate, vet, lint, unit tests, acceptance suite
```

`internal/scheduling/mysql/ledger_guard_test.go` must still pass: `ClaimPair` and `ReleaseCells`
in `ledger.go` are the only writers of `time_slot` in non-test code.
