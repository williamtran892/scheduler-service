# Quickstart: validate "Book an appointment"

How to prove the feature works end to end once it is implemented. Shapes are in
`api/openapi.yaml` (operationIds `bookAppointment`, `getAppointment`); release stand-ins are in
[contracts/README.md](./contracts/README.md); fixture ids are in [data-model.md](./data-model.md).

## Prerequisites

Go 1.26+, Docker with the compose plugin, `curl`, `jq`. See the repository `README.md`.

## 1. The acceptance scenarios

```bash
make test-integration TAGS=@spec-001              # all 29 scenarios of this feature
make test-integration TAGS='@spec-001 && ~@slow'  # without the three race scenarios
make test-integration                             # everything, including @bootstrap
```

Expected: every `@spec-001` scenario passes. Before implementation they fail with undefined steps
(strict mode), which is the Red gate for Principle I.

## 2. Unit tests

```bash
make generate   # mocks for scheduling.Store, Tx, WorkshopReader, CustomerReader
make test       # -race, no database
```

Expected: green, including the cases only unit tests can reach (research.md R16): deadlock retry,
`AVAILABILITY_CHANGED`, statement order inside the transaction, outbox and event payloads,
split-across-bays, seeded tie shuffle, grid and DST arithmetic, the grep guard on `time_slot`
writes, and the problem-code table.

## 3. By hand

```bash
make up    # app on :8080, MySQL on :3306, fixture loaded
```

Book an EV major service for Alice tomorrow at 09:00 London time, as an advisor:

```bash
START=$(TZ=Europe/London date -v+1d +%Y-%m-%dT09:00:00%z | sed -E 's/([0-9]{2})([0-9]{2})$/\1:\2/')
# GNU date: TZ=Europe/London date -d tomorrow +%Y-%m-%dT09:00:00%:z
curl -si localhost:8080/appointments \
  -H 'Content-Type: application/json' -H 'X-Channel: ADVISOR_CONSOLE' \
  -d '{"dealershipId":"d0000000-0000-4000-8000-000000000001",
       "serviceTypeId":"5e000000-0000-4000-8000-000000000004",
       "customerId":"c0000000-0000-4000-8000-000000000001",
       "vehicleId":"f0000000-0000-4000-8000-000000000001",
       "startAt":"'"$START"'","kind":"DROP_OFF",
       "customerRequests":{"courtesyCar":true,"notes":"Charging port cover loose"},
       "notes":"Customer prefers a morning call when ready."}'
```

Expected: `201`, a `Location: /appointments/<id>` header, and an `Appointment` with `status`
`CONFIRMED`, `version` 1, `serviceBay` Bay 3, `technician` Sam, `startAt` 09:00 and `endAt` 11:00
with the London offset, and `cells.bay` and `cells.technician` from 08:45 to 11:15 with 10 cells
each (design Appendix B).

Then:

| Do | Expect |
|---|---|
| Send the same request again | `201`, the same `id` |
| Same request with the MAJOR service type id `5e000000-0000-4000-8000-000000000002` | `409` `DUPLICATE_BOOKING` with `existing.id` = the first id |
| Same request for Bob (`c0000000-...-000000000002`, `f0000000-...-000000000002`) | `409` `SLOT_TAKEN` with `alternatives` (Sam is the only EV technician and Bay 3 the only EV bay) |
| `startAt` at 09:05 | `422` `OFF_GRID_START` |
| `startAt` at 19:00 | `422` `OUTSIDE_OPENING_HOURS` |
| `X-Channel: CUSTOMER_WEB` with DIAGNOSTIC `5e000000-0000-4000-8000-000000000006` | `422` `SERVICE_NOT_BOOKABLE_ONLINE` |
| No `X-Channel` header | `400` `VALIDATION_ERROR` naming `X-Channel` |
| `curl -s 'localhost:8080/appointments/<id>?include=events' -H 'X-Channel: ADVISOR_CONSOLE'` | The same record plus one `CONFIRMED` event |
| The same `GET` with `X-Channel: CUSTOMER_APP` | No `cells`, no `notes`, technician without `displayName` |

Inspect the ledger directly if you want to (`make db-shell`):

```sql
SELECT resource_kind, MIN(slot_start), MAX(slot_start), COUNT(*)
  FROM time_slot WHERE appointment_id = '<id>' GROUP BY resource_kind;
SELECT event_type, published_at FROM outbox WHERE aggregate_id = '<id>';  -- one row, unpublished
```

```bash
make down
```

## 4. Latency (SC-005)

Not part of the acceptance suite. With `make up` running, drive `POST /appointments` at 50
requests per second for 30 seconds with any HTTP load tool (for example `hey` or `vegeta`) over
distinct customer, start and kind combinations within the fixture window, and read the p95.
Expected: under 200 ms on a developer machine (design 7). Most requests will be `409` once the
window fills; measure confirmations and refusals separately.
