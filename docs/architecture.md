# System Architecture

This page describes how the system is built today. Update it in the same change that alters it.

## Process

The service is one binary, `cmd/scheduler`, and it runs in one mode:

1. It loads configuration from the environment (`platform.LoadConfig`).
2. It installs OpenTelemetry providers for the configured exporter (`platform.SetupOTel`).
3. It opens the MySQL pool and pings it (`platform.OpenMySQL`). The schema must already exist.
4. It wires the Scheduling service: the MySQL repositories (the Scheduling store, and the setup
   and customers repositories as its two readers), the real clock and id generator, and the
   logging, instrumenting and tracing middleware.
5. It builds the HTTP handler (`app.NewHandler`) over the endpoint specs of the wrapped Scheduling
   service (`endpoint.Specs`) and serves it.
6. It builds the no-show sweeper over the same wrapped Scheduling service and the store, with the
   grace period and interval from the configuration, and starts its worker.

The HTTP server and the sweeper's worker run in one `errgroup` under a context that SIGINT or
SIGTERM cancels. When the context ends, the server shuts down gracefully within
`SHUTDOWN_TIMEOUT` and the worker returns; the worker's `context.Canceled` is a normal exit, any
other error from either member ends the process. A background worker is a `Run(ctx) func() error`
that joins the group the same way.

## Packaging

The `Dockerfile` packages a prebuilt static Linux binary (`bin/linux/scheduler`, built by
`scripts/build-image.sh`) into an Alpine image that runs as a non-root user. `docker-compose.yml`
runs that image as the `app` service next to `mysql`. The app starts once MySQL is healthy and is
healthy itself once `GET /healthz` answers `200`.

## Packages

The code is split per domain under `internal/`. Scheduling has the full layers; Workshop Setup and
Customer & Vehicle are read-only in this release and are one `mysql` package each, which
implements Scheduling's reader interfaces. The design behind the split is in
[`docs/design/IMPLEMENTATION_NOTES.md`](./design/IMPLEMENTATION_NOTES.md), section 2.

| Package | Responsibility |
|---|---|
| `cmd/scheduler` | Wiring only. Chooses the real implementation of every dependency |
| `internal/app` | `NewHandler`: the one mux, `GET /healthz`, each domain's `EndpointSpec`s mounted through its `Register`, and the `otelhttp` wrapper |
| `internal/platform` | Cross-cutting pieces: `Config`, `Clock` (`RealClock`, `FakeClock`), `IDs` (`UUIDs`), `OpenMySQL`, `EncodeError` and `Problem`, `Health`, `SetupOTel`. Imports no domain package |
| `internal/setup/mysql` | Workshop Setup's `Repository`, which implements `scheduling.WorkshopReader` over `dealership`, `service_bay`, `technician`, `skill`, `technician_skill`, `service_type`, `service_type_bay_type`, `service_type_skill` and returns Scheduling's types. A service type and its skills and bay types are one query into a `ServiceRequirement`; only `ACTIVE` bays and technicians are listed, and a technician carries the skills valid on the date asked (`valid_from <= date`, `valid_to` null or `>= date`) |
| `internal/customers/mysql` | Customer & Vehicle's `Repository`, which implements `scheduling.CustomerReader` over `customer` and `vehicle` and returns Scheduling's types |
| `internal/scheduling` | Scheduling root, plain Go with no SQL or HTTP: the grid (`Grid` with `Align`, `Floor` and `Ceil`, `CellRun`), `ServiceRequirement` and its runs, `FeasibleStarts`, `CandidatePairs` and `NearestAlternatives`, `Appointment` and `BookingKey`, the enumerations and the appointment status transition table (`CanTransitionTo`), `Keep` (what a release leaves booked), the typed errors, and the interfaces `Store`, `Tx`, `WorkshopReader` and `CustomerReader` |
| `internal/scheduling/mysql` | `Store` and `Tx` over `appointment`, `time_slot`, `appointment_event` and `outbox`; maps MySQL errors 1062 on `uq_appointment_booking` and 1213 to Scheduling's errors. `ClaimPair` and `ReleaseCells` in `ledger.go` are the only code that writes `time_slot` |
| `internal/scheduling/service` | The go-kit service, with no HTTP routes or decoders: `Service` (`Book`, `Get`, `Reschedule`, `Cancel`, `CheckIn`, `Start`, `Complete`, `MarkNoShow`), its commands and `Record`, the business code, the appointment view per channel (`NewAppointmentView`, also the outbox snapshot), and the logging, instrumenting and tracing middleware |
| `internal/scheduling/endpoint` | Scheduling's HTTP surface, in two files. `endpoint.go` has `EndpointSpec`, `Specs` (the route table: one slice literal with a row per operation), `Register`, and the go-kit endpoints (`Make<Operation>Endpoint`) with their request types. `facade.go` has the request decoders, the response encoders and the parsing they share |
| `internal/scheduling/sweeper` | The no-show sweeper: `Service.RunOnce` marks overdue appointments through the Scheduling service's own `MarkNoShow`, and `Worker` runs it on a ticker until its context ends |

Dependency rules, each pinned by a test or a convention the reviewer checks:

- The Scheduling root imports only the standard library (`internal/scheduling` has a test that
  fails otherwise). `platform` imports no domain package.
- Scheduling reaches the other two domains only through the reader interfaces it declares,
  `WorkshopReader` and `CustomerReader`. `setup/mysql` and `customers/mysql` implement them
  directly and return Scheduling's types, so they import `scheduling`; `scheduling` never
  imports `setup` or `customers`.
- A `mysql` package reads and writes its own domain's tables. One exception: `scheduling/mysql`
  joins `dealership.slot_minutes` in `DayLedger` and `OwnedCells`, because a cell's length is
  needed to tell consecutive cells from a gap and to end a cell range, and neither method's
  signature carries the grid.
- `scheduling/endpoint` imports `scheduling/service`, `scheduling` and `platform`; `service` never
  imports `endpoint`. A domain's HTTP code lives only in its `endpoint` package, and routes are
  registered only by `endpoint.Register`, which `app.NewHandler` calls.
- The sweeper sees the Scheduling service only through two one-method interfaces, `Appointments`
  (`DueNoShows`, implemented by the store) and `Marker` (`MarkNoShow`, implemented by the
  service), so it goes through the same middleware and the same transaction as staff.
- Domain errors reach `platform.EncodeError` through two small interfaces, `Coded`
  (`ProblemCode() string`) and `Extended` (`ProblemExtensions() map[string]any`), so `platform`
  never knows a domain error type.

## HTTP

| Route | Behaviour |
|---|---|
| `GET /healthz` | `200 {"status":"ok"}` if the database answers a ping within 2 seconds, otherwise `503` with problem code `DATABASE_UNAVAILABLE` |
| `POST /appointments` | Books an appointment (`bookAppointment` in `api/openapi.yaml`): `201` with the `Appointment` record and a `Location` header, or a problem |
| `GET /appointments/{appointmentId}` | Reads one appointment (`getAppointment`), with its audit trail when `?include=events`: `200`, or `404 NOT_FOUND` (also for an id that is not a UUID) |
| `POST /appointments/{appointmentId}/reschedule` | Moves a `CONFIRMED` appointment in time, to another bay or technician, or both (`rescheduleAppointment`): `200` with the record, or a problem |
| `POST /appointments/{appointmentId}/cancel` | Cancels a `CONFIRMED` or `CHECKED_IN` appointment (`cancelAppointment`); a `CANCELLED` one is returned unchanged |
| `POST /appointments/{appointmentId}/check-in` | Checks a `CONFIRMED` appointment in, or a `NO_SHOW` one in as a late arrival (`checkInAppointment`) |
| `POST /appointments/{appointmentId}/start` | Starts a `CHECKED_IN` appointment (`startAppointment`) |
| `POST /appointments/{appointmentId}/complete` | Completes an `IN_PROGRESS` appointment (`completeAppointment`) |
| `POST /appointments/{appointmentId}/no-show` | Marks a `CONFIRMED` appointment `NO_SHOW` (`markNoShow`) |

Each route is one `EndpointSpec` in `internal/scheduling/endpoint`, and a spec has five fields:
`HTTPMethod`, `PathTemplate` (an `http.ServeMux` pattern such as
`/appointments/{appointmentId}/check-in`), `Endpoint` (the go-kit endpoint), `Decode` and `Encode`.
`Specs(svc)` in `endpoint.go` lists them all as one slice literal, each element showing its method,
path template, `Make<Operation>Endpoint(svc)`, decoder and encoder, so the whole route table reads
in one place. `Register(mux, specs, opts...)` mounts each as `kithttp.NewServer(spec.Endpoint,
spec.Decode, spec.Encode, opts...)` at `HTTPMethod + " " + PathTemplate`. The decoders and encoders
are in `facade.go`. A new route is a new element in `Specs`, its endpoint in `endpoint.go`, its
decoder in `facade.go`, and a row above. Business failures travel as the response's `Err` and are
written by `platform.EncodeError`, as are the errors of a decoder, through the
`kithttp.ServerErrorEncoder` that `app.NewHandler` passes to `Register`.

Routes have no `/v1` prefix; the contract's `/v1` is read as a gateway base path. The other
operations of the contract (listing, availability search, setup) are not served yet, and the mux
answers `405` or `404` for them. Every lifecycle route takes a JSON body with the required
integer `expectedVersion` and an optional `reason`, and answers `200` with the appointment after
the transition, in the calling channel's view.

### Channel

There is no authentication. Every `/appointments` request names its calling channel in the
`X-Channel` header, a stand-in for the `channel` claim of the contract's bearer token. A missing
header is `400 VALIDATION_ERROR` and an unknown value `422 INVALID_VALUE`, both naming
`X-Channel`; `401` is never returned. `403 FORBIDDEN` is returned when a customer channel asks
for a staff action: check-in, start, complete, no-show, or a reschedule that names a bay or a
technician. Cancel and a reschedule in time only are open to every channel, and ownership is not
checked because there is no customer identity. Staff channels are `ADVISOR_CONSOLE`,
`CALL_CENTRE` and `INTEGRATION`; customer channels are `CUSTOMER_WEB` and `CUSTOMER_APP`. The
channel is recorded on the appointment and on its audit event, and decides what the record shows:
staff see the owned ledger cells, the internal notes, the technician's name and the events'
payloads; customers see none of them, and `notes` sent through a customer channel are not stored.
The tenant of a booking is the tenant of its dealership, and the audit actor is `anonymous`; the
sweeper's is `no-show-sweeper`, with no channel.

### Times

Requests carry `startAt` as an RFC 3339 date-time with an offset. Everything is stored in UTC.
Responses render `startAt`, `endAt`, cell ranges and alternatives in the dealership's time zone,
and `createdAt`, `updatedAt` and event times in UTC. Cell ranges are half-open (`from` inclusive,
`to` exclusive) with a cell count. `actualStartAt` and `actualEndAt` are `null` until a job starts
and completes, then render in the dealership's time zone like `startAt`.

## Booking

`Book` applies these checks in order and stops at the first that fails:

1. The endpoint's decoder reads the request: JSON shape, required fields, identifiers as UUIDs,
   `startAt` with an offset (`400 VALIDATION_ERROR`), then `kind` and `X-Channel` values and text
   lengths (`422 INVALID_VALUE`). The first problem found is the answer.
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

`Tx.ClaimPair` and `Tx.ReleaseCells` in `internal/scheduling/mysql/ledger.go` are the only code
that writes `time_slot`. For the bay run and then the technician run, always in that order,
`ClaimPair` locks the run's cells with `SELECT ... FOR UPDATE` ordered by `slot_start`, checks in
Go that every cell exists and is `FREE` or already booked by the appointment it claims for, then
updates them to `BOOKED` with the appointment id and `version = version + 1`. A locking read is a
current read, so two claimers of overlapping cells serialise on the first shared row and the
loser sees `BOOKED` before it writes, at MySQL's default isolation level. A test fails if any
other production file writes `time_slot`; the acceptance suite's `@clock` step is test code and is
exempt (see [Development Standards](./development.md)).

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

## Lifecycle

Reschedule, cancel, check-in, start, complete and no-show are transitions of the appointment's
status. `scheduling.AppointmentStatus.CanTransitionTo` is the one table (design 4.3):

| From | To | Operation | Ledger effect |
|---|---|---|---|
| `CONFIRMED` | `CONFIRMED` | reschedule | claim the new runs, release every owned cell outside them |
| `CONFIRMED` | `CHECKED_IN` | check-in | none |
| `CONFIRMED` | `CANCELLED` | cancel | release every owned cell |
| `CONFIRMED` | `NO_SHOW` | no-show (staff or sweeper) | release every owned cell |
| `NO_SHOW` | `CHECKED_IN` | check-in (late arrival) | claim runs from the current grid cell on a free qualified pair |
| `CHECKED_IN` | `IN_PROGRESS` | start | none; sets `actual_start_at` |
| `CHECKED_IN` | `CANCELLED` | cancel | release every owned cell |
| `IN_PROGRESS` | `COMPLETED` | complete | release the owned cells that start at or after `Ceil(now)`; sets `actual_end_at` |

Each method checks in this order and stops at the first failure: the endpoint's decoding (`400`
and `422`); the channel may take the action (`403`, before anything is read); the appointment
exists (`404`); a cancel of a `CANCELLED` appointment returns it unchanged, whatever
`expectedVersion` says, because a retried cancel carries the version it was first sent with;
`expectedVersion` equals the stored version (`409 VERSION_CONFLICT`, the detail naming both:
"Appointment is at version 3; you sent 2."); the table allows the move (`409 INVALID_TRANSITION`);
then the operation's own rules. A stale caller's idea of the status is unreliable, so a stale
version wins over an illegal transition.

### The transaction

Each transition is one transaction, in this order: the version-guarded
`UPDATE appointment ... WHERE id = ? AND version = ?` (zero rows is `VERSION_CONFLICT`, so a change
that lands between the read and the write is also refused, and two transitions of one appointment,
the sweeper racing staff included, serialise on that row before either touches a cell); the ledger
claim and release the table lists; the audit event; the outbox row. A refusal anywhere leaves none
of it behind. The event types are `RESCHEDULED`, `CANCELLED`, `CHECKED_IN`, `STARTED`, `COMPLETED`
and `NO_SHOW`, the outbox types `AppointmentRescheduled`, `AppointmentCancelled`,
`AppointmentCheckedIn`, `AppointmentStarted`, `AppointmentCompleted` and `AppointmentNoShow`; like
booking's, the outbox row holds the staff view of the record after the transition and nothing
relays it yet. The event payload carries the `reason` when given, the previous and new placement
of a move, `mileage` and `additionalConcerns` of a check-in (the record has no field for them),
the actual time, and the number of cells a no-show or completion released.

### Release

`Tx.ReleaseCells(appointmentID, keep)` locks every cell the appointment owns
(`SELECT ... FOR UPDATE` on `appointment_id`), drops from the list the cells `Keep.Keeps` names,
and frees the rest with one `UPDATE` per resource, returning how many it freed. `Keep` has three
rules: the zero `Keep` keeps nothing (cancel, no-show); two kept runs, each with a resource id,
keep the cells inside them, matched on resource id as well as time, so a new technician frees the
old one's cells (reschedule); and `Before` keeps the cells that start before it (complete, with
`Ceil(now)`: completing at 09:40 a job booked from 09:00 to 10:00 keeps the 09:00, 09:15 and
09:30 cells and frees the 09:45 cell). Which cells go depends on `now`, never on the `completedAt`
the dealer system sends, because the ledger must show what is free from now on.

### Reschedule and reassign

A reschedule reuses booking's plan, checks, candidate search and claim loop, with the appointment
as the owner: `DayLedger` counts the owner's cells as free and leaves them out of the loads, and a
claim treats cells the appointment already owns as claimable, so a move that overlaps itself
works. The new start is `startAt`, or the current start for a pure reassign, and follows booking's
rules in booking's order (staff-only service through a customer channel, grid, after now, booking
horizon, working time), so a reassign sent after the booked start has passed is `422 INVALID_VALUE`
on `startAt`. A named `serviceBayId` must be an active bay of the dealership of an allowed type
and a named `technicianId` an active technician (`422 INVALID_VALUE` naming the field) holding the
required skills on the new local date (`422 NO_QUALIFIED_TECHNICIAN`); the candidates are
restricted to what is named. The pair tried first is the appointment's current pair with the
named bay or technician put in its place, so a move keeps its bay and technician when they are
free; the others follow booking's order. Per pair, one transaction runs the update, the claim, the
release of every owned cell outside the new runs and the event and outbox row. A taken pair moves
to the next, a deadlock victim is retried once on the same pair, missing cells recompute the
candidates once and then answer `409 AVAILABILITY_CHANGED`, and no pair left is `409 SLOT_TAKEN`;
both carry up to five alternatives computed with the owner's cells free and the named resources
respected. A new start that is the booking key of another appointment, in any status, is `409
DUPLICATE_BOOKING` carrying it (MySQL 1062 on `uq_appointment_booking` from the update). In every
refusal the appointment and its cells are unchanged. A reschedule can be the deadlock victim when
a booking races it for the cells it frees (design 11.3).

### Late arrival

Check-in of a `NO_SHOW` appointment is a reschedule into the cells free now, in the same
transaction as the check-in: the new start is the start of the grid cell that contains now, the
runs follow from the service type as in booking, the previous pair is tried first and then the
others least-loaded first, and the transaction is the update (to `CHECKED_IN` with the new times
and pair), then `ClaimPair`, then the event (with `lateArrival`, the previous and the new
placement) and the outbox row. There is no release, since a no-show owns no cells. The booking
horizon and "after now" do not apply. Every way of finding no pair, whether cells are taken or
missing or nobody is qualified, is `409 SLOT_TAKEN` with alternatives from the current grid cell,
and the appointment stays `NO_SHOW`.

### The no-show sweeper

`sweeper.Service.RunOnce` asks the store for the `CONFIRMED` appointments whose `start_at` is
before `now - NO_SHOW_GRACE`, oldest first, and calls the service's own `MarkNoShow` for each, one
transaction per appointment, with the version it read, the actor `no-show-sweeper` and no
channel (the event's `channel` column is `NULL`). An appointment that is not due (start plus
grace equal to now is not yet due), no longer `CONFIRMED`, changed by someone else first
(`VERSION_CONFLICT`) or no longer allowed to move (`INVALID_TRANSITION`) is skipped; any other
failure is logged with the appointment id and counted, and the run goes on. A failed query is
logged and returned. `Worker` runs it every `NO_SHOW_SWEEP_INTERVAL` until its context ends. One
instance runs in this release: there is no batching, leader election or locking between
instances.

| Variable | Default | Meaning |
|---|---|---|
| `NO_SHOW_GRACE` | `30m` | A `CONFIRMED` appointment whose start plus this has passed is marked `NO_SHOW` |
| `NO_SHOW_SWEEP_INTERVAL` | `1m` | How often the sweeper runs; `docker-compose.yml` sets `1s` for the acceptance suite |

Both are Go durations and must be greater than zero.

## Errors

Errors use RFC 9457 problem details (`application/problem+json`) with the fields `type`
(`https://scheduler.example.com/problems/<CODE>`), `title`, `status`, `code` and `detail`, and
where they apply `alternatives`, `existing` (the appointment that owns a booking key) and
`errors[]` (`field` and `message`). `platform.EncodeError` is the only place that maps errors to
status codes, from one table of the contract's problem codes:

| Code | Status | Produced by |
|---|---|---|
| `VALIDATION_ERROR` | 400 | a malformed request, a missing required field, `X-Channel` or `expectedVersion`, a bad identifier or date-time, a reschedule that names nothing to change |
| `NOT_FOUND` | 404 | an unknown appointment |
| `INVALID_VALUE` | 422 | an unknown `kind`, channel or `include`, text too long, `expectedVersion` below 1, a negative `mileage`, an unknown or inconsistent reference (including a named bay or technician), a start in the past or beyond the horizon, a `startedAt` or `completedAt` in the future or a `completedAt` before the actual start |
| `OFF_GRID_START`, `OUTSIDE_OPENING_HOURS`, `NO_QUALIFIED_TECHNICIAN`, `SERVICE_NOT_BOOKABLE_ONLINE` | 422 | the booking checks above, also applied to a reschedule |
| `SLOT_TAKEN`, `AVAILABILITY_CHANGED`, `DUPLICATE_BOOKING` | 409 | the booking outcomes above, also a reschedule's; a late arrival answers only `SLOT_TAKEN` |
| `VERSION_CONFLICT`, `INVALID_TRANSITION` | 409 | the lifecycle checks above |
| `DUPLICATE` | 409 | in the table, not produced yet |
| `FORBIDDEN` | 403 | a customer channel asking for a staff action |
| `UNAUTHORIZED` | 401 | in the table, not produced yet |
| anything else | 500 | `INTERNAL`, with no detail returned; the logging middleware records the error |

## Data

MySQL 8.4. The schema lives in `local/mysql/schema.sql` and the fixture in `local/mysql/data.sql`.
Both run as container init scripts, and the application never creates tables. Every connection
uses UTC (`parseTime=true&loc=UTC&time_zone='+00:00'`). There are no stored routines or triggers,
and enumerations are `VARCHAR` columns. Scheduling's allowed values are Go constants with parse and
scan functions in its root. Setup's bay types and statuses are not: Scheduling reads a bay type as
text, and the Setup repository selects `ACTIVE` in SQL.

The schema has 15 tables:

| Domain | Tables |
|---|---|
| Setup | `tenant`, `dealership`, `service_bay`, `technician`, `skill`, `technician_skill`, `service_type`, `service_type_bay_type`, `service_type_skill` |
| Customers | `customer`, `vehicle` |
| Scheduling | `appointment`, `time_slot`, `appointment_event`, `outbox` |

`time_slot` is the capacity ledger: one row per bay and per technician for every grid cell inside
working time, `FREE` or `BOOKED` by one appointment, with the primary key
`(resource_kind, resource_id, slot_start)`. A cell that does not exist is time the resource does
not work. `appointment` keeps the exact times promised to the customer (`start_at`, `end_at`) and
the actual times (`actual_start_at`, `actual_end_at`, null until the job starts and completes);
only the ledger is quantised. The unique index `uq_appointment_booking` on `(vehicle_id,
customer_id, start_at, kind)` covers every status, and it is global across dealerships. The
outbox rows are written with each booking and transition and not published: no relay exists yet.
`ix_appointment_sweep` on `(status, start_at)` serves the sweeper's query for CONFIRMED appointments past their grace period, so a tick is an index range scan, not a table scan.

The fixture creates the setup rows, the customers and the ledger's cells; the application inserts
`appointment`, `appointment_event` and `outbox` rows, updates `appointment` rows on every
transition, and flips `time_slot` cells between `FREE` and `BOOKED`. The fixture is described in
[Development Standards](./development.md).

## Observability

- Logging: `go-kit/log` in logfmt to stderr, with `ts` and `caller`. The logging middleware logs
  every Scheduling call with its method, ids, duration and error; a lifecycle call also logs the
  version sent and who acted, the channel or the actor of a background job. The sweeper logs a
  failed query and each appointment it could not mark, under `component=sweeper`.
- Traces and metrics: OpenTelemetry. `otelhttp` opens one server span per request, named after
  the matched route pattern. The tracing middleware opens a span per Scheduling call
  (`scheduling.Book`, `scheduling.Get`, `scheduling.Reschedule`, `scheduling.Cancel`,
  `scheduling.CheckIn`, `scheduling.Start`, `scheduling.Complete`, `scheduling.MarkNoShow`) and
  each transaction is a span (`scheduling.InTx`). The sweeper calls `MarkNoShow` through the same
  middleware, so its marks are logged, counted and traced like staff's.
  The instrumenting middleware counts calls (`scheduling.requests`) and records their duration
  (`scheduling.request.duration`) by `method` and problem `code`, which gives the `SLOT_TAKEN`
  rate. The exporter is `none` (default), `stdout` or `otlp`.
