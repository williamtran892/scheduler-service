# System Architecture

This page describes how the system is built today. Update it in the same change that alters it.

## Process

The service is one binary, `cmd/scheduler`, and it runs in one mode:

1. It loads configuration from the environment (`platform.LoadConfig`).
2. It installs OpenTelemetry providers for the configured exporter (`platform.SetupOTel`).
3. It opens the MySQL pool and pings it (`platform.OpenMySQL`). The schema must already exist.
4. It wires the Scheduling service: the MySQL repositories, the adapters over them, the real
   clock and id generator, and the logging, instrumenting and tracing middleware.
5. It builds the HTTP handler (`app.NewHandler`) and serves it.

The HTTP server runs in an `errgroup` under a context that SIGINT or SIGTERM cancels. When the
context ends, the server shuts down gracefully within `SHUTDOWN_TIMEOUT`. Background workers join
the same group when they exist.

## Packaging

The `Dockerfile` packages a prebuilt static Linux binary (`bin/linux/scheduler`, built by
`scripts/build-image.sh`) into an Alpine image that runs as a non-root user. `docker-compose.yml`
runs that image as the `app` service next to `mysql`. The app starts once MySQL is healthy and is
healthy itself once `GET /healthz` answers `200`.

## Packages

The code is split per domain, each under `internal/` with the same layers. The design behind the
split is in [`docs/design/IMPLEMENTATION_NOTES.md`](./design/IMPLEMENTATION_NOTES.md), section 2.

| Package | Responsibility |
|---|---|
| `cmd/scheduler` | Wiring only. Chooses the real implementation of every dependency |
| `internal/app` | `NewHandler`: the one mux, every route, the `otelhttp` wrapper |
| `internal/platform` | Cross-cutting pieces: `Config`, `Clock` (`RealClock`, `FakeClock`), `IDs` (`UUIDs`), `OpenMySQL`, `EncodeError` and `Problem`, `Health`, `SetupOTel`. Imports no domain package |
| `internal/setup` | Workshop Setup root: `Dealership`, `ServiceBay`, `Technician`, `ServiceType`, the `BayType`, `BayStatus` and `TechnicianStatus` enumerations, and the read-only `Repository` interface |
| `internal/setup/mysql` | `Repository` over `dealership`, `service_bay`, `technician`, `skill`, `technician_skill`, `service_type`, `service_type_bay_type`, `service_type_skill`. A technician carries the skills valid on the date asked (`valid_from <= date`, `valid_to` null or `>= date`) |
| `internal/customers` | Customer & Vehicle root: `Customer`, `Vehicle` and the read-only `Repository` interface |
| `internal/customers/mysql` | `Repository` over `customer` and `vehicle` |
| `internal/scheduling` | Scheduling root, plain Go with no SQL or HTTP: the grid (`Grid`, `CellRun`), `ServiceRequirement` and its runs, `FeasibleStarts`, `CandidatePairs` and `NearestAlternatives`, `Appointment` and `BookingKey`, the enumerations, the typed errors, and the interfaces `Store`, `Tx`, `WorkshopReader` and `CustomerReader` |
| `internal/scheduling/mysql` | `Store` and `Tx` over `appointment`, `time_slot`, `appointment_event` and `outbox`; maps MySQL errors 1062 on `uq_appointment_booking` and 1213 to Scheduling's errors. Only `ledger.go` writes `time_slot` |
| `internal/scheduling/adapters` | `WorkshopReader` over `setup.Repository` and `CustomerReader` over `customers.Repository`: the only place Scheduling imports another domain |
| `internal/scheduling/service` | The go-kit service: `Service` (`Book`, `Get`), endpoints, HTTP transport (decoders, `RegisterRoutes`), the appointment view per channel, and the logging, instrumenting and tracing middleware |

Dependency rules, each pinned by a test or a convention the reviewer checks:

- A domain root imports only the standard library (`internal/scheduling` has a test that fails
  otherwise). `platform` imports no domain package.
- Domains reach each other only through reader interfaces declared in the downstream root and
  implemented in the downstream `adapters` package. `scheduling` never imports `setup` or
  `customers`.
- A `mysql` package reads and writes its own domain's tables. One exception: `scheduling/mysql`
  joins `dealership.slot_minutes` in `DayLedger` and `OwnedCells`, because a cell's length is
  needed to tell consecutive cells from a gap and to end a cell range, and neither method's
  signature carries the grid.
- Domain errors reach `platform.EncodeError` through two small interfaces, `Coded`
  (`ProblemCode() string`) and `Extended` (`ProblemExtensions() map[string]any`), so `platform`
  never knows a domain error type.

## HTTP

| Route | Behaviour |
|---|---|
| `GET /healthz` | `200 {"status":"ok"}` if the database answers a ping within 2 seconds, otherwise `503` with problem code `DATABASE_UNAVAILABLE` |
| `POST /appointments` | Books an appointment (`bookAppointment` in `api/openapi.yaml`): `201` with the `Appointment` record and a `Location` header, or a problem |
| `GET /appointments/{appointmentId}` | Reads one appointment (`getAppointment`), with its audit trail when `?include=events`: `200`, or `404 NOT_FOUND` (also for an id that is not a UUID) |

Routes have no `/v1` prefix; the contract's `/v1` is read as a gateway base path. Other
operations of the contract (listing, reschedule, cancel and the day-of transitions) are not
served yet, and the mux answers `405` or `404` for them.

### Channel

There is no authentication. Every `/appointments` request names its calling channel in the
`X-Channel` header, a stand-in for the `channel` claim of the contract's bearer token. A missing
header is `400 VALIDATION_ERROR` and an unknown value `422 INVALID_VALUE`, both naming
`X-Channel`; `401` and `403` are never returned. Staff channels are `ADVISOR_CONSOLE`,
`CALL_CENTRE` and `INTEGRATION`; customer channels are `CUSTOMER_WEB` and `CUSTOMER_APP`. The
channel is recorded on the appointment and on its audit event, and decides what the record shows:
staff see the owned ledger cells, the internal notes, the technician's name and the events'
payloads; customers see none of them, and `notes` sent through a customer channel are not stored.
The tenant of a booking is the tenant of its dealership, and the audit actor is `anonymous`.

### Times

Requests carry `startAt` as an RFC 3339 date-time with an offset. Everything is stored in UTC.
Responses render `startAt`, `endAt`, cell ranges and alternatives in the dealership's time zone,
and `createdAt`, `updatedAt` and event times in UTC. Cell ranges are half-open (`from` inclusive,
`to` exclusive) with a cell count. `actualStartAt` and `actualEndAt` are always `null`: no column
holds them yet.

## Booking

`Book` applies these checks in order and stops at the first that fails:

1. The transport decodes the request: JSON shape, required fields, identifiers as UUIDs, `startAt`
   with an offset (`400 VALIDATION_ERROR`), then `kind` and `X-Channel` values and text lengths
   (`422 INVALID_VALUE`). The first problem found is the answer.
2. The dealership, service type, customer and vehicle must exist, share the dealership's tenant,
   the service type must be active, and the vehicle must belong to the customer
   (`422 INVALID_VALUE` naming the field).
3. The booking key (vehicle, customer, start, kind) is looked up. An appointment that owns it,
   with the same dealership and service type and not cancelled or a no-show, is returned
   unchanged with `201`; any other owner is `409 DUPLICATE_BOOKING` carrying it.
4. A staff-only service type requested through a customer channel is
   `422 SERVICE_NOT_BOOKABLE_ONLINE`.
5. The start must be on the grid: a whole multiple of `slot_minutes` from the top of the UTC hour,
   zero seconds (`422 OFF_GRID_START`).
6. The start must be after now and not after now plus the dealership's booking horizon
   (`422 INVALID_VALUE` on `startAt`).
7. The bay run and the technician run follow from the service type: both start in the cell that
   `start - buffer before` falls in and cover `buffer before + duration + buffer after`, rounded
   up to whole cells. They are identical today.
8. Working time is the ledger: every cell of both runs must exist for the dealership, on any
   resource and in any status, otherwise `422 OUTSIDE_OPENING_HOURS`.
9. Some active technician must hold every required skill on the local date, otherwise
   `422 NO_QUALIFIED_TECHNICIAN`.
10. Candidate pairs come from the free runs of the local day: a bay of an allowed type and a
    qualified technician, each holding the whole run on its own (a run split across two bays is
    not a candidate), least-loaded first, where load is the booked cells on the bay plus on the
    technician that day. Pairs of equal load are shuffled with a seed derived from the
    appointment id the request generated. With no candidate, the booking key is looked up once
    more, then the answer is `409 SLOT_TAKEN` with alternatives.

### The transaction and the claim

For each candidate pair, one transaction runs these statements in this order: insert the
appointment (its unique index on the booking key is the idempotency guard), `ClaimPair`, append
the `CONFIRMED` audit event, append the `AppointmentConfirmed` outbox row. A `201` means all of
it is committed; every other outcome leaves none of it behind.

`Tx.ClaimPair` in `internal/scheduling/mysql/ledger.go` is the only code that writes `time_slot`.
For the bay run and then the technician run, always in that order, it locks the run's cells with
`SELECT ... FOR UPDATE` ordered by `slot_start`, checks in Go that every cell exists and is
`FREE`, then updates them to `BOOKED` with the appointment id and `version = version + 1`. A
locking read is a current read, so two claimers of overlapping cells serialise on the first
shared row and the loser sees `BOOKED` before it writes, at MySQL's default isolation level.
A test fails if any other file writes `time_slot`.

The service reacts to the outcome of an attempt:

| Outcome | Reaction |
|---|---|
| a cell is booked | next pair; with no pair left, re-check the booking key, then `409 SLOT_TAKEN` |
| a deadlock victim (MySQL 1213) | the same pair once more, then the next pair |
| cells missing | recompute the candidates once; if cells are missing again, re-check the booking key, then `409 AVAILABILITY_CHANGED` |
| duplicate booking key (MySQL 1062) | the appointment that owns the key, as in check 3 |

`SLOT_TAKEN` and `AVAILABILITY_CHANGED` carry up to five alternatives: the feasible starts for the
same service type over the requested local date and the next one, later than now, never the
requested start, nearest first and earlier first on a tie.

The record of an appointment is assembled from several reads, each through its own domain: the
appointment and its owned cells from `scheduling/mysql`, the names of the dealership, service
type, bay and technician through `WorkshopReader`, the customer and vehicle through
`CustomerReader`.

## Errors

Errors use RFC 9457 problem details (`application/problem+json`) with the fields `type`
(`https://scheduler.example.com/problems/<CODE>`), `title`, `status`, `code` and `detail`, and
where they apply `alternatives`, `existing` (the appointment that owns a booking key) and
`errors[]` (`field` and `message`). `platform.EncodeError` is the only place that maps errors to
status codes, from one table of the contract's problem codes:

| Code | Status | Produced by |
|---|---|---|
| `VALIDATION_ERROR` | 400 | a malformed request, a missing required field or `X-Channel`, a bad identifier or `startAt` |
| `NOT_FOUND` | 404 | an unknown appointment |
| `INVALID_VALUE` | 422 | an unknown `kind`, channel or `include`, text too long, an unknown or inconsistent reference, a start in the past or beyond the horizon |
| `OFF_GRID_START`, `OUTSIDE_OPENING_HOURS`, `NO_QUALIFIED_TECHNICIAN`, `SERVICE_NOT_BOOKABLE_ONLINE` | 422 | the booking checks above |
| `SLOT_TAKEN`, `AVAILABILITY_CHANGED`, `DUPLICATE_BOOKING` | 409 | the booking outcomes above |
| `VERSION_CONFLICT`, `INVALID_TRANSITION`, `DUPLICATE` | 409 | in the table, not produced yet |
| `UNAUTHORIZED`, `FORBIDDEN` | 401, 403 | in the table, not produced yet |
| anything else | 500 | `INTERNAL`, with no detail returned; the logging middleware records the error |

## Data

MySQL 8.4. The schema lives in `local/mysql/schema.sql` and the fixture in `local/mysql/data.sql`.
Both run as container init scripts, and the application never creates tables. Every connection
uses UTC (`parseTime=true&loc=UTC&time_zone='+00:00'`). There are no stored routines or triggers,
and enumerations are `VARCHAR` columns whose allowed values are Go constants with parse and scan
functions in each domain root.

The schema has 15 tables:

| Domain | Tables |
|---|---|
| Setup | `tenant`, `dealership`, `service_bay`, `technician`, `skill`, `technician_skill`, `service_type`, `service_type_bay_type`, `service_type_skill` |
| Customers | `customer`, `vehicle` |
| Scheduling | `appointment`, `time_slot`, `appointment_event`, `outbox` |

`time_slot` is the capacity ledger: one row per bay and per technician for every grid cell inside
working time, `FREE` or `BOOKED` by one appointment, with the primary key
`(resource_kind, resource_id, slot_start)`. A cell that does not exist is time the resource does
not work. `appointment` keeps the exact times promised to the customer; only the ledger is
quantised. The unique index `uq_appointment_booking` on `(vehicle_id, customer_id, start_at,
kind)` covers every status. The outbox rows are written with the booking and not published: no
relay exists yet.

The fixture creates the setup rows, the customers and the ledger's cells; the application inserts
`appointment`, `appointment_event` and `outbox` rows and flips `time_slot` cells to `BOOKED`. The
fixture is described in [Development Standards](./development.md).

## Observability

- Logging: `go-kit/log` in logfmt to stderr, with `ts` and `caller`. The logging middleware logs
  every Scheduling call with its method, ids, duration and error.
- Traces and metrics: OpenTelemetry. `otelhttp` opens one server span per request, named after
  the matched route pattern. The tracing middleware opens a span per Scheduling call
  (`scheduling.Book`, `scheduling.Get`) and each booking transaction is a span (`scheduling.InTx`).
  The instrumenting middleware counts calls (`scheduling.requests`) and records their duration
  (`scheduling.request.duration`) by `method` and problem `code`, which gives the `SLOT_TAKEN`
  rate. The exporter is `none` (default), `stdout` or `otlp`.
