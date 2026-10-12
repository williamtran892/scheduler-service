# Implementation notes

How the appointment booking and lifecycle features are built: the deviations from the design, the repository layout, the Go shape of the booking path, database rules and the test design. Section numbers are stable identifiers that specs cite ("notes 4.4"); they are not contiguous. Where these notes and `SCHEDULING_DESIGN.md` disagree, the deviations in section 0 win.

---

## 0. Deviations from the design documents

The design documents were written before the implementation stack was chosen. Four decisions taken while writing this plan change *how* the design is realised without changing *what* it guarantees. They are listed here so the design documents can stay as written and a reader knows where the two disagree.

| # | Design says | This plan does | Why | Where |
|---|---|---|---|---|
| D1 | Four bounded contexts with contracts packages, inbox tables and an event bus | One process with one directory per context (`setup`, `scheduling`, `customers`, `engagement`), each owning its types, its data access and its services; cross-domain calls only through reader interfaces and an adapter package in the downstream domain. No contracts packages, inbox tables or bus until the physical split | Smallest thing that delivers v1 with the context boundaries already in the code, so the split is a move, not a refactor | Section 2 |
| D2 | Capacity is claimed through stored procedures `sp_claim_pair`, `sp_release_cells`, `sp_materialise_resource_day` and a `version` trigger (design 11.2 to 11.4) | The same statements issued from Go: `SELECT ... FOR UPDATE`, inspect in Go, `UPDATE`. No routines, no triggers in the schema | The guarantee is the primary key and the locking read, not the procedure. Logic in Go is testable, debuggable and changeable in a normal code review; the phase 2 predicates become Go, not routine migrations. The materialiser itself is deferred (4.7); the fixture seeds its cells | Section 4.5, 5.1 |
| D4 | An `idempotency_key` reservation table, inserted first in the booking transaction, with the response stored before commit (design 5.2, 7) | No table and no header. The natural booking key `(vehicle_id, customer_id, start_at, kind)` is a unique index on `appointment`; the `INSERT INTO appointment` is the first statement of the transaction and that index gives the same blocking and duplicate semantics. A duplicate loads the existing appointment and returns it, or `409 DUPLICATE_BOOKING` if the rest differs. The service generates the appointment id, which is also the reference | One table and one sweeper fewer; clients cannot forget or mis-scope a key; a double submit from any channel is caught by the data, not the header | Section 4.4, `schema.sql` |
| D3 | `ENUM(...)` column types for bay type, statuses, kind, channel, resource kind (`schema.sql`) | `VARCHAR` columns; the allowed values are Go constants with parse and validate functions. The database keeps only cross-column `CHECK` constraints | Adding a value is a code change with tests, not an `ALTER TABLE` on a hot table; the Go type is the single definition used by the API, the store and the harness | Section 5.4 |

The correctness argument in design 11.3 (an exclusive row lock per cell, taken in primary-key order, bay before technician) is unchanged by D2. What changes is which process evaluates "is every cell claimable": InnoDB inside a procedure before, Go code over locked rows now. The Gherkin concurrency scenarios re-validate the argument against the Go implementation (section 6.5).

---

## 2. Repository layout

One Go module, one binary, one process, one MySQL schema, exactly as `local/mysql/schema.sql` is written. The code is split **per domain**, following four bounded contexts: Workshop Setup, Scheduling, Customer & Vehicle, Engagement. Each domain is one directory under `internal/` and owns its types, its data access and its services. The go-kit layering from `examples/shipping` applies *inside* each scheduling. The physical machinery of separate contexts (contracts packages, inbox tables, an in-process bus, boundary linting) is still **not** built; the directory boundary and one adapter package per dependency are the seams, and they are enough for v1. Everything is under `internal/` so nothing leaks as a public API.

Every domain has the same four layers:

| Layer | Where | Contains | May import |
|---|---|---|---|
| Domain root | `internal/<domain>/`, one package named after the domain | Plain types, enumerations, pure logic, typed errors, and the **repository interfaces** that are this domain's data access contract. Also the **reader interfaces** for anything it needs from another domain, expressed in its own types | Standard library only. Never another domain, never go-kit, never `database/sql` |
| Data access | `internal/<domain>/mysql/` | The repository implementation on `database/sql`, touching only this domain's tables; the MySQL error mapping for those tables | Its own domain root, `platform` |
| Service | `internal/<domain>/service/`, one package per domain, always named `service`: the application service the parent domain provides. Workers that only run on a ticker get their own package beside it | go-kit `service.go`, `endpoint.go`, `transport.go`, `middleware.go`, one file per capability when the domain has several, unit tests and generated mocks | Its own domain root, `platform`, go-kit, `engagement` for the publisher interface |
| Adapters | `internal/<domain>/adapters/`, only in a domain that depends on another | Implementations of the root's reader interfaces over the upstream domain's repository. The anti-corruption layer, and the only place one domain imports another | Its own domain root, the upstream domain's root |

```
scheduler-service/
├── go.mod                              module scheduler
├── cmd/scheduler/main.go               the one entry point: config, logger, OTel, pool, wire every domain, errgroup (section 4.7)
├── internal/
│   ├── setup/                          WORKSHOP SETUP: tenant, dealership, opening hours, exceptions, bays, technicians, skills, service types
│   │   ├── model.go                    Dealership, OpeningHours, OpeningException, ServiceBay, Technician, Skill, ServiceType
│   │   ├── enums.go                    BayType, BayStatus, TechnicianStatus with Parse/Valid/Scan/Value (section 5.4)
│   │   ├── errors.go                   ErrNotFound, ErrDuplicateName, ErrOffGridOpening, ...
│   │   ├── repository.go               Repository interface: CRUD per aggregate, Requirement(serviceTypeID), DealershipConfig, Resources(dealershipID, date)
│   │   ├── mysql/                      package mysql: repository.go, resources.go, catalogue.go, errors.go
│   │   └── service/                    the Setup service: service.go endpoint.go transport.go middleware.go service_test.go mock_repository_test.go
│   ├── scheduling/                     SCHEDULING: the ledger, availability, appointments, lifecycle, the outbox
│   │   ├── grid.go                     Grid, CellRun, Align, Ceil, CellsFor
│   │   ├── requirement.go              ServiceRequirement, Runs; DealershipConfig, ServiceBay, Technician as this domain sees them
│   │   ├── availability.go             Island, FeasibleStarts, CandidatePairs (pure functions)
│   │   ├── appointment.go              Appointment, BookingKey, the status transition table
│   │   ├── enums.go                    AppointmentKind, AppointmentStatus, Channel, ResourceKind, CellStatus
│   │   ├── errors.go                   ErrSlotTaken, ErrCellsMissing, ErrOffGrid, ErrDuplicateBooking, ErrStaleVersion, ErrDeadlockVictim, ...
│   │   ├── repository.go               Store and Tx (its data access, section 4.0); WorkshopReader and CustomerReader (what it needs from other domains)
│   │   ├── mysql/                      package mysql: db.go (Store, InTx), ledger.go, appointments.go, events.go, outbox.go, errors.go
│   │   ├── adapters/                   workshop.go: WorkshopReader over setup.Repository; customers.go: CustomerReader over customers.Repository
│   │   ├── service/                    the Scheduling service, one file per capability inside one package and one Service interface
│   │   │   ├── service.go              Service interface, service struct, NewService
│   │   │   ├── availability.go         Search
│   │   │   ├── booking.go              Book, Get, the candidate loop (section 4.4)
│   │   │   ├── lifecycle.go            Reschedule, Reassign, Cancel, CheckIn, Start, Complete, NoShow
│   │   │   ├── endpoint.go transport.go middleware.go
│   │   │   └── *_test.go mock_scheduling_test.go
│   │   ├── sweeper/                    worker: RunOnce marks no-shows and releases cells
│   │   └── relay/                      worker: RunOnce drains the outbox to an engagement.Publisher
│   ├── customers/                      CUSTOMER & VEHICLE: snapshots of DMS data
│   │   ├── model.go                    Customer, Vehicle
│   │   ├── repository.go               Repository interface
│   │   ├── mysql/                      package mysql: repository.go, errors.go
│   │   └── service/                    the Customers service: create and get customers and vehicles (v1 has no DMS feed; the fixture seeds them)
│   ├── engagement/                     ENGAGEMENT: what happens after a booking event
│   │   ├── event.go                    Event, Publisher interface
│   │   └── publishers/                 log, in-memory (tests), confirmation stub recording one message per event id
│   ├── harness/                        HTML harness: a second transport over the same Endpoints (not in this release)
│   └── platform/                       config, clock, ids, MySQL pool opener, problem-details error encoder, OTel setup
├── api/
│   ├── openapi.yaml                    the API contract (OpenAPI 3.1): every endpoint, schema and problem code; linted in CI
│   ├── docs.html                       Redoc page rendering openapi.yaml; `scheduler serve` embeds both at /docs and /openapi.yaml
│   ├── openapi-appointments.yaml       generated subset: Appointments and Lifecycle tags only, with just the components they use
│   ├── docs-appointments.html          Redoc page for the subset
│   └── subset.js                       regenerates the subset from openapi.yaml (`node api/subset.js`); never edit the subset by hand
├── docker-compose.yml                  MySQL 8.4 with the two init scripts below (section 5.1)
├── local/mysql/schema.sql              the schema: every table, no routines, no ENUM; executed on every container start
├── local/mysql/data.sql                the fixture: MAN01, bays, technicians, catalogue, customers, FREE cells for a fixed window
├── features/                           Gherkin feature files (section 6)
├── test/acceptance/                    godog suite, world, step definitions, database reset
├── specs/                              Spec Kit feature folders
├── .specify/                           Spec Kit memory, templates, scripts, feature.json
└── .claude/skills/speckit-*/           installed by `specify init --integration claude`
```

Rules that keep the domains apart:

- **A domain root imports only the standard library.** No go-kit, no `database/sql`, no other scheduling. It is the part of the domain that would survive any change of framework or storage.
- **One `mysql` package per domain, touching only that domain's tables.** `appointment_event` and `outbox` belong to Scheduling because they are written inside its booking transaction. The grep test in 4.5 pins every write to `time_slot` to `scheduling/mysql/ledger.go`.
- **Cross-domain calls go through a reader interface in the downstream root and an adapter in the downstream scheduling.** `scheduling` never imports `setup`; `scheduling/adapters` does, and it translates `setup.ServiceType` plus its skills and bay types into `scheduling.ServiceRequirement`. In v1 the adapter is a function call over the upstream repository in the same process. In the later split it becomes an HTTP client or an event consumer behind the same interface.
- **Events leave a domain only through the outbox and the publisher interface.** `scheduling/relay` depends on `engagement.Publisher`; Engagement never calls into Scheduling.
- **Every domain's service package is named `service`.** `setup/service`, `scheduling/service`, `customers/service`: the name says "the service this domain provides", and the import path says which domain. `main` imports them under aliases (`setupsvc`, `schedsvc`, `custsvc`); nothing else imports more than one.
- **Dependency direction inside a domain** is go-kit's: `transport.go` → `endpoint.go` → `service.go` → root interfaces ← `mysql`. **Across domains** only `scheduling/adapters` → `setup`, `customers` and `scheduling/relay` → `engagement`. `harness` depends on the service packages' `Endpoints` only. A short test per domain root runs `go list -deps` and fails if the root imports anything outside the standard library; that is the whole boundary tooling.
- **Deferred and therefore absent:** a `scheduling/materialiser` worker and `scheduling/mysql/materialise.go`, `setup/mysql/rota.go`, and any migration tooling (sections 4.7, 5.1).

If and when the split into processes happens, each domain directory becomes a service as it stands: its `mysql` package keeps its tables, its services keep their endpoints and transports, and its adapters are replaced by go-kit clients. Nothing in the v1 layout prevents that split.

## 4. Go architecture

### 4.0 The go-kit shape, once

Every domain's `service` package (`setup/service`, `scheduling/service`, `customers/service`) is a go-kit service and follows the layout of the go-kit `examples/shipping` and `examples/profilesvc` packages: one `Service` interface per domain, the same four files in every package, so a reader who has seen one has seen them all. When a domain has several capabilities, the service methods are spread over one file per capability (`availability.go`, `booking.go`, `lifecycle.go` in Scheduling) but stay on the one interface, so there is one `Endpoints`, one transport and one middleware chain per domain.

| File | Contains | May import |
|---|---|---|
| `service.go` | `type Service interface { ... }` with one method per use case, taking `context.Context` first and returning `(result, error)`. The unexported `service` struct holding the domain root's repository and reader interfaces, clock, id generator and publisher. `NewService(...)` | Its domain root, `platform`, standard library |
| `endpoint.go` | `type Endpoints struct { BookEndpoint endpoint.Endpoint; ... }`, `MakeServerEndpoints(s Service) Endpoints`, one `makeXEndpoint(s Service) endpoint.Endpoint` per method, and the request and response structs. Every response struct carries an `Err error` field (tagged to be omitted from JSON) and implements `endpoint.Failer`, so a business failure travels as data through the endpoint chain and is visible to middleware | `service.go`, `go-kit/kit/endpoint` |
| `transport.go` | `RegisterRoutes(mux *http.ServeMux, e Endpoints, options ...kithttp.ServerOption)`: one `kithttp.NewServer(endpoint, decodeRequest, encodeResponse, options...)` per route, registered with exact Go 1.22 method and path patterns on the mux `main` owns. The route table of a domain is therefore this one file. Decoders parse JSON, path and query values and validate enumerations with `scheduling.Parse*`; the encoder writes JSON on success and defers to the shared problem-details encoder on `Failed()` | `endpoint.go`, `go-kit/kit/transport/http` |
| `middleware.go` | `type Middleware func(Service) Service`. `LoggingMiddleware(logger log.Logger) Middleware` logs method, key identifiers, `took` and `err` for every call. `InstrumentingMiddleware(meter metric.Meter) Middleware` records a request counter and a latency histogram per method with `service`, `method` and `error` attributes. `TracingMiddleware(tracer trace.Tracer) Middleware` opens a span per method call, named `booking.Book`, and records the error on it | `service.go`, `go-kit/log`, `go.opentelemetry.io/otel/metric`, `go.opentelemetry.io/otel/trace` |

The service interface is the contract; everything else is adapters around it. For the booking service:

```go
package service // internal/scheduling/service/service.go

type Service interface {
    Search(ctx context.Context, q AvailabilityQuery) (AvailabilityResult, error)
    Book(ctx context.Context, cmd BookCommand) (scheduling.Appointment, error)
    Reschedule(ctx context.Context, cmd RescheduleCommand) (scheduling.Appointment, error)
    Reassign(ctx context.Context, cmd ReassignCommand) (scheduling.Appointment, error)
    Cancel(ctx context.Context, cmd TransitionCommand) (scheduling.Appointment, error)
    CheckIn(ctx context.Context, cmd TransitionCommand) (scheduling.Appointment, error)
    Start(ctx context.Context, cmd TransitionCommand) (scheduling.Appointment, error)
    Complete(ctx context.Context, cmd TransitionCommand) (scheduling.Appointment, error)
    NoShow(ctx context.Context, cmd TransitionCommand) (scheduling.Appointment, error)
    Get(ctx context.Context, tenantID, appointmentID string) (scheduling.Appointment, error)
}

type service struct {
    store     scheduling.Store
    workshop  scheduling.WorkshopReader
    customers scheduling.CustomerReader
    clock     platform.Clock
    ids       platform.IDs
}

func NewService(store scheduling.Store, workshop scheduling.WorkshopReader, customers scheduling.CustomerReader,
    clock platform.Clock, ids platform.IDs) Service {
    return &service{store, workshop, customers, clock, ids}
}
```

The data access contract lives in the domain root, is shared by every service in the domain, and is implemented once in `scheduling/mysql`. What the domain needs from other domains is declared the same way and implemented in `scheduling/adapters`:

```go
package scheduling // internal/scheduling/repository.go

// Store is the Scheduling domain's persistence. scheduling/mysql.Store satisfies it.
type Store interface {
    InTx(ctx context.Context, fn func(tx Tx) error) error
    Islands(ctx context.Context, dealershipID string, day DayWindow) ([]Island, error)
    Appointment(ctx context.Context, id string) (Appointment, error)
    AppointmentByBookingKey(ctx context.Context, key BookingKey) (Appointment, error)   // vehicle, customer, start, kind
    ConfirmedBefore(ctx context.Context, cutoff time.Time) ([]Appointment, error)        // sweeper
    UnpublishedOutbox(ctx context.Context, limit int) ([]OutboxRow, error)               // relay
    MarkPublished(ctx context.Context, id int64) error
}

// Tx is the slice of Store that runs inside one transaction.
type Tx interface {
    InsertAppointment(a Appointment) error
    UpdateAppointment(a Appointment, expectedVersion int) error
    ClaimPair(dealershipID, bayID string, bayRun CellRun, techID string, techRun CellRun, apptID, excludeApptID string) error
    ReleaseCells(apptID string, keepBay, keepTech *CellRun, keepBefore *time.Time) error
    AppendEvent(apptID, kind, actor string, channel Channel, snapshot any) error
    AppendOutbox(aggregate, id, eventType string, payload any) error
}

// WorkshopReader is what Scheduling needs from Workshop Setup, in Scheduling's own types.
// scheduling/adapters implements it over setup.Repository today and over a client later.
type WorkshopReader interface {
    Requirement(ctx context.Context, serviceTypeID string) (ServiceRequirement, error)
    DealershipConfig(ctx context.Context, dealershipID string) (DealershipConfig, error)
    Resources(ctx context.Context, dealershipID string, on time.Time) (bays []ServiceBay, techs []Technician, err error) // ACTIVE, skills valid on the date
}

// CustomerReader is what Scheduling needs from Customer & Vehicle.
type CustomerReader interface {
    VehicleOwner(ctx context.Context, vehicleID string) (customerID string, err error)
}
```

Endpoints and transport, in the shape go-kit expects:

```go
package service // internal/scheduling/service/endpoint.go

type bookRequest struct {
    TenantID      string            // from the auth context in v2; a header in v1
    Body          bookBody          // dealership, service type, customer, vehicle, start, kind, channel
}
type bookResponse struct {
    Appointment  *appointmentView `json:"appointment,omitempty"`
    Alternatives []time.Time       `json:"alternatives,omitempty"`
    Err          error             `json:"-"`
}
func (r bookResponse) Failed() error { return r.Err }

func makeBookEndpoint(s Service) endpoint.Endpoint {
    return func(ctx context.Context, request any) (any, error) {
        req := request.(bookRequest)
        appt, err := s.Book(ctx, req.command())
        if err != nil { return bookResponse{Err: err}, nil }      // business failure is data, not a transport error
        return bookResponse{Appointment: view(appt)}, nil
    }
}
```

```go
package service // internal/scheduling/service/transport.go

// RegisterRoutes puts this domain's routes on the mux that main owns, with exact Go 1.22 method+path patterns.
// MakeHTTPHandler (same routes on a fresh mux) exists for the day the domain runs as its own process.
func RegisterRoutes(mux *http.ServeMux, e Endpoints, opts ...kithttp.ServerOption) {
    mux.Handle("GET /dealerships/{id}/availability", kithttp.NewServer(e.SearchEndpoint, decodeSearchRequest, encodeResponse, opts...))
    mux.Handle("POST /appointments", kithttp.NewServer(e.BookEndpoint, decodeBookRequest, encodeCreated, opts...))
    mux.Handle("GET /appointments/{id}", kithttp.NewServer(e.GetEndpoint, decodeGetRequest, encodeResponse, opts...))
    mux.Handle("POST /appointments/{id}/reschedule", kithttp.NewServer(e.RescheduleEndpoint, decodeRescheduleRequest, encodeResponse, opts...))
    // ... one line per use case
}

func encodeCreated(ctx context.Context, w http.ResponseWriter, response any) error {
    if f, ok := response.(endpoint.Failer); ok && f.Failed() != nil {
        platform.EncodeError(ctx, f.Failed(), w)                   // section 4.6
        return nil
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    return json.NewEncoder(w).Encode(response)
}
```

Wiring in `main.go`, outermost middleware last so it runs first:

```go
import schedsvc "scheduler/internal/scheduling/service"

var schedulingSvc schedsvc.Service
schedulingSvc = schedsvc.NewService(schedStore, workshop, customersReader, clock, ids)
schedulingSvc = schedsvc.LoggingMiddleware(log.With(logger, "component", "scheduling"))(schedulingSvc)
schedulingSvc = schedsvc.InstrumentingMiddleware(otel.Meter("scheduler"))(schedulingSvc)
schedulingSvc = schedsvc.TracingMiddleware(otel.Tracer("scheduler"))(schedulingSvc)

schedsvc.RegisterRoutes(mux,
    schedsvc.MakeServerEndpoints(schedulingSvc),
    kithttp.ServerErrorEncoder(platform.EncodeError),
    kithttp.ServerErrorLogger(logger),
    kithttp.ServerBefore(platform.PopulateRequestContext),      // request id, tenant, channel into ctx
)
```

Rules that keep the shape honest:

- **Transport decodes and encodes, endpoints adapt, the service decides.** A decoder never calls a repository. An endpoint never contains an `if` about the scheduling. A service method never sees `http.Request`.
- **Business outcomes are `Err` on the response struct.** `SLOT_TAKEN`, `VERSION_CONFLICT`, `AVAILABILITY_CHANGED` are returned by the service, placed on the response by the endpoint, and encoded by the transport. Transport-level failures (malformed JSON, unknown enumeration value, missing header) are returned from the decoder. Both paths end in `platform.EncodeError`, so there is one place that knows HTTP status codes (4.6). Because the service returns the error, the logging middleware records every business failure with its code without any extra plumbing.
- **Repository interfaces live in the domain root, next to the types they return.** `scheduling.Store` and `scheduling.Tx` are implemented once by `scheduling/mysql`, `setup.Repository` by `setup/mysql`, and every service in a domain shares them. What a domain needs from another domain is an interface in its root too (`scheduling.WorkshopReader`), implemented in `scheduling/adapters`. This is the `cargo.Repository` / `mysql.NewCargoRepository` pattern from the go-kit shipping example applied per domain, and it is what makes the service unit tests possible: `mockgen` generates a mock of each interface next to the tests (6.0). The acceptance suite never uses a mock.
- **Cross-cutting concerns are middleware; transactions are not.** Logging, metrics and tracing wrap the service interface. The HTTP server span comes from `otelhttp` around the mux, the service span from `TracingMiddleware`, and the store adds a span per transaction, so one request is one trace from handler to `COMMIT`. The booking transaction stays explicit inside `service.Book`, because the statement order inside it is the correctness argument (4.4) and must be readable in one place.
- **Worker services follow the same shape minus transport.** `relay.Service` and `sweeper.Service` (and later `materialiser.Service`) each expose `RunOnce(ctx) (Report, error)`, get the same logging, instrumenting and tracing middleware, and are driven by a `Worker` ticker in production and called directly by Gherkin steps and the harness (4.7).

### 4.1 Core domain types

Everything in a domain root (`internal/scheduling`, `internal/setup`, `internal/customers`, `internal/engagement`) is plain Go with no SQL, no HTTP and no go-kit. The Scheduling root holds the grid algebra, which is tiny and is the thing property tests target.

```go
package scheduling // internal/scheduling/grid.go

type Grid struct{ SlotMinutes int }           // 10, 15, 20 or 30, from dealership.slot_minutes

// Align returns t if it sits on the grid, otherwise ErrOffGrid. The anchor is the UTC clock,
// the same rule the design's procedure used: MOD(TIMESTAMPDIFF(SECOND,'2000-01-01',t), slot*60) = 0.
func (g Grid) Align(t time.Time) (time.Time, error)
func (g Grid) Ceil(t time.Time) time.Time           // used by complete: keep_before = ceil(now); the cell in progress stays BOOKED
func (g Grid) CellsFor(d time.Duration) int         // ceil(d / slot)

type CellRun struct {
    First time.Time
    Cells int
}
func (r CellRun) Last() time.Time                   // First + (Cells-1)*slot
func (r CellRun) End(g Grid) time.Time              // First + Cells*slot
func (r CellRun) Within(i Island) bool              // i.From <= First && End <= i.To
```

```go
package platform // internal/platform/clock.go

type Clock interface{ Now() time.Time }
type RealClock struct{}
type FakeClock struct{ mu sync.Mutex; t time.Time }  // harness and Gherkin set this
```

### 4.2 Requirement resolution

A booking or availability request is resolved into a requirement once, through `scheduling.WorkshopReader.Requirement`. The adapter in `scheduling/adapters` answers it from `setup.Repository`, which joins `service_type`, `service_type_skill` and `service_type_bay_type`, and translates the result into Scheduling's own struct. Everything downstream works from the struct, never from the catalogue tables and never from `setup` types.

```go
package scheduling // internal/scheduling/requirement.go

type ServiceRequirement struct {
    ServiceTypeID   string
    Duration        time.Duration
    BufferBefore    time.Duration
    BufferAfter     time.Duration
    RequiredSkills  []string      // AND
    AllowedBayTypes []string      // OR
    OnlineBookable  bool
}

// Runs returns the bay run and the technician run for a start time: both begin at start - BufferBefore;
// the bay run covers BufferBefore + Duration + BufferAfter, the technician run the same in v1 (design 5.1).
func (r ServiceRequirement) Runs(start time.Time, g Grid) (bay, tech CellRun)

type DealershipConfig struct {
    DealershipID string; Location *time.Location; Grid Grid
    BookingHorizonDays, MaterialiseHorizonDays int
}

type WorkingInterval struct{ LocalDate string; OpenUTC, CloseUTC time.Time } // YYYY-MM-DD; a closed date has no interval
```

### 4.3 Availability: a pure function over islands

The SQL "free runs per resource" query (design 11.4) returns islands. Everything after that is pure Go, which is where the design's named test case lives.

```go
package scheduling // internal/scheduling/availability.go

type Island struct{ Kind, ResourceID string; From, To time.Time }

type Candidate struct{ Start time.Time; BayID, TechID string; Load int }

// FeasibleStarts emits every grid start c inside the day where SOME bay individually contains the
// bay run and SOME technician individually contains the technician run. Bay 1 free 09:00-09:15 and
// Bay 2 free 09:15-09:30 do not make a 30-minute slot at 09:00; see availability_test.go/TestSplitAcrossBays.
func FeasibleStarts(day DayWindow, g Grid, req ServiceRequirement,
    bays []ServiceBay, techs []Technician, islands []Island) []time.Time

// CandidatePairs returns the (bay, technician) pairs that can take `start`, least-loaded first,
// ties shuffled with the request's seed. Used by booking at confirm time.
func CandidatePairs(start time.Time, g Grid, req ServiceRequirement,
    bays []ServiceBay, techs []Technician, islands []Island, seed int64) []Candidate
```

Property tests with `rapid`: for random islands and requirements, every emitted start has a witnessing bay island and technician island; no emitted start is off-grid; emitting is monotone (freeing a cell never removes a start).

`Search(ctx, query) (AvailabilityResult, error)` on the Scheduling `Service`, in `scheduling/service/availability.go`, loads the requirement, config and resources through `scheduling.WorkshopReader` and the islands through `scheduling.Store`, calls `scheduling.FeasibleStarts`, ranks, and for staff channels attaches `scheduling.CandidatePairs`. Its endpoint and transport are one route, `GET /dealerships/{id}/availability`.

### 4.4 The booking transaction

This is the heart of feature 003 and the place the constitution's principles II, III and IV are enforced. It is the body of `booking.service.Book`; the endpoint and transport around it are the boilerplate from 4.0.

```go
package service // internal/scheduling/service/booking.go

func (s *service) Book(ctx context.Context, cmd BookCommand) (scheduling.Appointment, error) {
    req, err := s.workshop.Requirement(ctx, cmd.ServiceTypeID)            // 422 SERVICE_NOT_BOOKABLE_ONLINE for customer channels
    cfg, err := s.workshop.DealershipConfig(ctx, cmd.DealershipID)
    start, err := cfg.Grid.Align(cmd.StartAt)                                // 422 OFF_GRID_START, no retry
    bayRun, techRun := req.Runs(start, cfg.Grid)                             // buffers widen the runs; design 5.1

    pairs, err := s.candidatePairs(ctx, cmd.DealershipID, start, req, cmd.Seed) // islands query + scheduling.CandidatePairs
    if len(pairs) == 0 { return scheduling.Appointment{}, s.slotTaken(ctx, cmd, start) } // 409 SLOT_TAKEN + alternatives

    recomputed := false
    for i := 0; i < len(pairs); i++ {
        p := pairs[i]
        appt := scheduling.NewAppointment(s.ids.New(), cmd, p, start, req)       // id generated here; it is also the reference
        err := s.store.InTx(ctx, func(tx Tx) error {
            if err := tx.InsertAppointment(appt); err != nil { return err }  // first statement; uq_appointment_booking is the idempotency guard
            if err := tx.ClaimPair(cmd.DealershipID, p.BayID, bayRun, p.TechID, techRun, appt.ID, ""); err != nil {
                return err                                                   // section 4.5
            }
            if err := tx.AppendEvent(appt.ID, "CONFIRMED", cmd.Actor, cmd.Channel, appt); err != nil { return err }
            return tx.AppendOutbox("Appointment", appt.ID, "AppointmentConfirmed", appt)
        })
        switch {
        case err == nil:
            return appt, nil
        case errors.Is(err, scheduling.ErrSlotTaken):
            continue                                                        // next pair
        case errors.Is(err, scheduling.ErrDeadlockVictim):
            if !p.retried { p.retried = true; i-- }                          // same pair, once
            continue
        case errors.Is(err, scheduling.ErrCellsMissing):
            if recomputed { return scheduling.Appointment{}, s.availabilityChanged(ctx, cmd, start) }
            recomputed = true
            pairs, _ = s.candidatePairs(ctx, cmd.DealershipID, start, req, cmd.Seed); i = -1
            continue
        case errors.Is(err, scheduling.ErrDuplicateBooking):                      // 1062 on uq_appointment_booking: a retry or a double submit
            return s.existing(ctx, cmd)                                      // same request → the existing appointment, same 201; different → 409 DUPLICATE_BOOKING
        default:
            return scheduling.Appointment{}, err
        }
    }
    return scheduling.Appointment{}, s.slotTaken(ctx, cmd, start)               // nothing stored; a retry recomputes
}
```

Rules the sketch encodes:

- The appointment id is generated by the service before the transaction, so the event and outbox rows can reference it in the same transaction. It doubles as the customer-facing reference; there is no counter and no separate reference column.
- `InsertAppointment` is the **first** statement in the transaction. The unique index `uq_appointment_booking (vehicle_id, customer_id, start_at, kind)` is the idempotency guard (deviation D4): a concurrent retry or double submit blocks on that index until the first attempt commits or rolls back, so two appointments for one booking key are impossible, and no header is needed.
- `existing` loads the appointment that owns the booking key, checks it belongs to the caller's tenant, compares dealership, service type and channel-visible fields with the request, and returns the existing appointment so the transport emits the same `201`, or `scheduling.ErrDuplicateBooking` (`409 DUPLICATE_BOOKING`, carrying the existing appointment) if any differ. A retried request that originally failed has no row and runs again, which is the better answer anyway. Because the key covers every status, a cancelled or no-show appointment for the same minute also yields `409 DUPLICATE_BOOKING`; reinstating is a later feature.
- The service never issues SQL against `time_slot` except through `tx.ClaimPair` (section 4.5).
- The same shape, with the appointment's own id as the exclusion and `tx.ReleaseCells` after the claim, is `Reschedule`; `Reassign` is `Reschedule` with the same times. The other transitions are `UpdateAppointment` with the expected `version`, then `ReleaseCells` where the state machine says cells are freed, then event and outbox, all in one `InTx`.
- The `SlotTakenError` returned by `slotTaken` carries the alternatives, so the endpoint can put them on the response and the encoder can emit them in the problem-details body.

### 4.5 The claim: lock, inspect, write

The design wrapped the claim in `sp_claim_pair` and relied on `UPDATE ... WHERE status = 'FREE'` plus a row count. This plan issues the equivalent statements from Go in a read-then-write shape. The guarantee is the same: `SELECT ... FOR UPDATE` is a locking read, so it takes an exclusive lock on every matching cell and returns the latest committed values, not the transaction snapshot, at any isolation level. Two claimers for overlapping cells serialise on the first shared row; the loser resumes after the winner commits, sees `BOOKED`, and stops before writing anything.

```go
package mysql // internal/scheduling/mysql/ledger.go

// ClaimPair is the only code that marks cells BOOKED. Bay first, then technician, always, so that
// every claimer takes locks in the same order and plain claims cannot deadlock each other (design 11.3).
func (tx *Tx) ClaimPair(dealershipID, bayID string, bayRun scheduling.CellRun,
    techID string, techRun scheduling.CellRun, apptID, excludeApptID string) error {
    if err := tx.claimRun(scheduling.ResourceBay, bayID, bayRun, apptID, excludeApptID, scheduling.ErrBayNotOpen); err != nil {
        return err
    }
    return tx.claimRun(scheduling.ResourceTechnician, techID, techRun, apptID, excludeApptID, scheduling.ErrTechnicianNotWorking)
}

func (tx *Tx) claimRun(kind scheduling.ResourceKind, id string, run scheduling.CellRun,
    apptID, exclude string, missing error) error {
    rows, err := tx.QueryContext(tx.ctx, `
        SELECT slot_start, status, appointment_id
          FROM time_slot
         WHERE resource_kind = ? AND resource_id = ? AND slot_start BETWEEN ? AND ?
         ORDER BY slot_start
         FOR UPDATE`, kind, id, run.First, run.Last())
    if err != nil { return mapMySQL(err) }                          // 1213 → ErrDeadlockVictim
    cells, err := scanCells(rows)
    if err != nil { return err }

    if len(cells) < run.Cells { return missing }                    // cells do not exist: not open / not working
    var taken []scheduling.Cell
    for _, c := range cells {
        if c.Status != scheduling.CellFree && c.AppointmentID != exclude { taken = append(taken, c) }
    }
    if len(taken) > 0 { return &scheduling.SlotTakenError{Kind: kind, Cells: taken} }

    _, err = tx.ExecContext(tx.ctx, `
        UPDATE time_slot SET status = ?, appointment_id = ?
         WHERE resource_kind = ? AND resource_id = ? AND slot_start BETWEEN ? AND ?`,
        scheduling.CellBooked, apptID, kind, id, run.First, run.Last())
    return mapMySQL(err)
}
```

`ReleaseCells(apptID, keepBay *CellRun, keepTech *CellRun, keepBefore *time.Time)` has the same shape: `SELECT ... FOR UPDATE` on `appointment_id = ?`, filter in Go to the cells outside the kept runs and at or after `keepBefore`, one `UPDATE ... WHERE appointment_id = ? AND slot_start IN (...)`. Cancel and no-show pass no kept runs; complete passes `keepBefore = grid.Ceil(now)`; reschedule passes the two new runs.

**What this buys over the procedure.**

- The diagnosis is free. The rows are in hand, so missing cells, taken cells and the owning appointment are known without a second query or `SQLSTATE` codes. `SlotTakenError` carries the cells, so the advisor can be told "09:15 and 09:30 are taken by appointment 3f9c…".
- No dependence on affected-rows semantics. The design's `version = version + 1` guard and the `ROW_COUNT()` gotcha in 11.3 exist only because `UPDATE` reports changed rows, not matched rows. Reading the rows first removes the problem; the reschedule self-overlap case is a plain comparison against `exclude`.
- Phase 2 predicates are Go. A `BLOCKED` status, a `reserved_for` channel or a per-service cap are checks over the fetched cells with unit tests on plain structs, not edits to a routine shipped by migration.

**What it costs.** Two round trips per resource instead of one, so four per claim, each well under a millisecond on a local network against a 200 ms p95 target. The row locks are held for one extra round trip.

**Rules that must hold.**

1. `FOR UPDATE` is not optional. A plain `SELECT` under `REPEATABLE READ` reads the snapshot and would double book. The twenty-client race scenario exists to catch this regression, and a code review of `ledger.go` should check for it first.
2. Bay statement before technician statement, in every caller, and never merged into one `OR` query where the optimizer chooses the scan order. Within a statement InnoDB locks in primary-key order; across statements the fixed order is what makes plain claims deadlock-free.
3. Only `ledger.go` (and later `materialise.go`) in `scheduling/mysql` writes `time_slot`. A `go test` in `internal/scheduling/mysql` greps the module for `UPDATE time_slot` and `INSERT` into it and fails if they appear anywhere else.

**Optional: fail fast under contention.** `FOR UPDATE NOWAIT` makes the loser fail at once with MySQL error 3572 instead of waiting for the winner to commit. Mapped to `ErrSlotTaken` it moves the booking to the next pair immediately and shortens the tail under contention. The cost is a false negative when the holder later rolls back, which costs one retry. Start without it; add it if the race lab shows waits.

**The known deadlock stays known.** A reschedule locks its new cells, then its old ones through `ix_slot_appt`; a booking racing it across both runs can form the cycle described in design 11.3. InnoDB resolves it with error 1213, the store maps it to `ErrDeadlockVictim`, the service retries once. The Gherkin scenario asserts the invariant (eight cells, all Alice's), not the victim.

### 4.6 Error mapping

Two functions carry every error from where it happens to the wire, and nothing else knows status codes or MySQL numbers.

- Each domain's `mysql/errors.go` maps that domain's MySQL errors to its root's typed errors, and nothing outside those files knows an error number. `internal/scheduling/mysql/errors.go` is the only one that knows the ledger numbers (1213, 3572) and the booking-key 1062. Everything the procedures used to report through `SQLSTATE 45xxx` is now a Go error produced by `ClaimPair` itself.
- `platform.EncodeError(ctx, err, w)` is the only place that knows HTTP status codes. It is installed on every `kithttp.NewServer` through `kithttp.ServerErrorEncoder` (for decoder failures) and is called by every response encoder when `Failed()` is non-nil (for business failures). The harness transport calls the same function to render the same problem as HTML.

| Source | Go error | HTTP |
|---|---|---|
| `ClaimPair`: a cell in the run is `BOOKED` by another appointment | `*scheduling.SlotTakenError` (is `scheduling.ErrSlotTaken`) | internal, next pair; final `409 SLOT_TAKEN` with `alternatives[]` |
| `ClaimPair`: fewer cells exist than the run needs | `scheduling.ErrBayNotOpen`, `scheduling.ErrTechnicianNotWorking` (both are `scheduling.ErrCellsMissing`) | internal, recompute once; then `409 AVAILABILITY_CHANGED` |
| `Grid.Align` before any SQL | `scheduling.ErrOffGrid` | `422 OFF_GRID_START` |
| MySQL 1213 | `scheduling.ErrDeadlockVictim` | internal, retry once |
| MySQL 3572 (only with `NOWAIT`) | `scheduling.ErrSlotTaken` | as `SLOT_TAKEN` |
| MySQL 1062 on `uq_appointment_booking` | `scheduling.ErrDuplicateBooking` | load the existing appointment: same request → the same `201`; different → `409 DUPLICATE_BOOKING` with the existing appointment |
| 0 rows on optimistic `UPDATE appointment ... AND version = ?` | `scheduling.ErrStaleVersion` | `409 VERSION_CONFLICT` |
| no technician qualifies at all | `scheduling.ErrNoQualifiedTechnician` | `422 NO_QUALIFIED_TECHNICIAN` |
| decoder: malformed body, unknown enumeration, missing required field | `platform.ErrBadRequest` wrapping `scheduling.ErrInvalidEnum` or a field name | `400` or `422 INVALID_VALUE` naming the field |
| anything else | unchanged | `500`, logged by `kithttp.ServerErrorLogger`, code `INTERNAL` |

HTTP responses use RFC 9457 problem details: `{"type","title","status","code","detail","alternatives":[...]}` with content type `application/problem+json`.

```go
package platform // internal/platform/problem.go

type Problem struct {
    Type, Title string; Status int; Code, Detail string
    Alternatives []time.Time `json:"alternatives,omitempty"`
}

func EncodeError(_ context.Context, err error, w http.ResponseWriter) {
    p := problemFor(err)                      // the table above, as a switch on errors.Is / errors.As
    w.Header().Set("Content-Type", "application/problem+json")
    w.WriteHeader(p.Status)
    _ = json.NewEncoder(w).Encode(p)
}
```

A unit test in `internal/platform` walks every error exported by the `scheduling`, `setup` and `customers` roots and asserts a status and a code, so adding an error without a mapping fails the build.

### 4.7 Process model and workers

**One process, one mode.** `cmd/scheduler/main.go` has no subcommands. It loads configuration, opens the pool, wires the services, and runs an `errgroup` containing the HTTP server and the two workers under a signal-cancelled context. There is nothing else to invoke. Schema and data come from the database container, not from the binary:

| Need | Where it lives in this release | Why |
|---|---|---|
| Create the schema | `local/mysql/schema.sql`, mounted into the MySQL container's init directory and executed on every fresh start (section 5.1) | No migration tool and no migrate step in this release. The binary assumes the schema exists |
| Seed data | `local/mysql/data.sql`, the second init script. One fixture for the acceptance suite, the harness and local development | Seeding is a fixture, not a command. Everyone starts from the same rows |
| Materialise cells | **Deferred.** `data.sql` inserts the `FREE` cells for the fixture dealership over a fixed date window, so availability and booking work without a materialiser. The `materialiser` package is added later as a third worker with the shape below, if time permits | The ledger tables exist in the schema from day one; the job that fills them for real dealerships is not needed to prove booking |
| Run a job once, deterministically | The worker service's `RunOnce(ctx)` method. Gherkin steps call it in-process; the harness calls it through `POST /harness/api/jobs/{name}` | The test and the person use exactly the code the ticker uses, with the injected clock |
| Operate a worker on its own | Not needed in v1. If a worker ever needs its own process, it is a second `main.go` that wires only that service, which the go-kit layering makes a few lines | Keeps the production binary to one shape until there is a reason for two |

```go
package main // cmd/scheduler/main.go, abridged

func main() {
    cfg := platform.LoadConfig()                                     // environment variables, documented in README
    logger := log.NewLogfmtLogger(os.Stderr)
    logger = log.With(logger, "ts", log.DefaultTimestampUTC, "caller", log.DefaultCaller)

    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    otelShutdown := platform.SetupOTel(ctx, cfg)                     // tracer and meter providers, OTLP exporters, resource attributes
    defer otelShutdown(context.Background())
    tracer := otel.Tracer("scheduler")
    meter := otel.Meter("scheduler")

    db := platform.OpenMySQL(cfg.DSN)                                // section 5.2; schema already present (5.1)
    setupRepo := setupmysql.New(db)                                  // internal/setup/mysql
    customerRepo := customersmysql.New(db)                           // internal/customers/mysql
    schedStore := schedulingmysql.New(db)                            // internal/scheduling/mysql
    workshop := adapters.NewWorkshopReader(setupRepo)                // Scheduling's view of Workshop Setup
    customersReader := adapters.NewCustomerReader(customerRepo)

    clock := platform.ClockFor(cfg)                                  // RealClock, or FakeClock when HARNESS_ENABLED
    ids := platform.UUIDs{}
    publisher := publishers.NewLog(logger)                           // engagement.Publisher

    setupSvc := wire(setupsvc.NewService(setupRepo),                 // setupsvc = internal/setup/service
        setupsvc.LoggingMiddleware(log.With(logger, "component", "setup")),
        setupsvc.InstrumentingMiddleware(meter),
        setupsvc.TracingMiddleware(tracer))
    customersSvc := wire(custsvc.NewService(customerRepo), ...)        // custsvc = internal/customers/service
    schedulingSvc := wire(schedsvc.NewService(schedStore, workshop, customersReader, clock, ids), ...) // schedsvc = internal/scheduling/service
    relaySvc := wire(relay.NewService(schedStore, publisher), ...)
    sweeperSvc := wire(sweeper.NewService(schedStore, clock, cfg.NoShowGrace), ...)

    mux := http.NewServeMux()
    setupEndpoints := setupsvc.MakeServerEndpoints(setupSvc)
    customersEndpoints := custsvc.MakeServerEndpoints(customersSvc)
    schedulingEndpoints := schedsvc.MakeServerEndpoints(schedulingSvc)
    setupsvc.RegisterRoutes(mux, setupEndpoints, httpOpts...)         // each transport registers its exact method+path patterns
    custsvc.RegisterRoutes(mux, customersEndpoints, httpOpts...)      // on the one mux main owns; a conflict fails at startup
    schedsvc.RegisterRoutes(mux, schedulingEndpoints, httpOpts...)
    mux.HandleFunc("GET /healthz", platform.Health(db))
    if cfg.HarnessEnabled {
        mux.Handle("/harness/", harness.New(setupEndpoints, customersEndpoints, schedulingEndpoints,
            harness.Control{Clock: clock, Relay: relaySvc, Sweeper: sweeperSvc}))
    }
    handler := otelhttp.NewHandler(mux, "http", otelhttp.WithSpanNameFormatter(platform.RouteName)) // one server span and the HTTP metrics per request

    srv := &http.Server{Addr: cfg.Addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
    g, gctx := errgroup.WithContext(ctx)
    g.Go(func() error {
        if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) { return err }
        return nil
    })
    g.Go(func() error {                                              // stop the listener when the group's context ends
        <-gctx.Done()
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        return srv.Shutdown(shutdownCtx)
    })
    g.Go(relay.NewWorker(relaySvc, 500*time.Millisecond).Run(gctx))
    g.Go(sweeper.NewWorker(sweeperSvc, time.Minute).Run(gctx))
    logger.Log("exit", g.Wait())                                     // first error or signal cancels the rest
}
```

The group is the standard library plus `errgroup`: `signal.NotifyContext` turns a signal into context cancellation, `errgroup.WithContext` cancels every goroutine when any one returns an error, and the HTTP server is shut down from a goroutine that waits on that context. No actor library; a worker's `Run` returns a plain `func() error` that ends when its context does.

Every dependency a service has (`Store`, `WorkshopReader`, `CustomerReader`, `Clock`, `IDs`, `Publisher`, the grace duration) is an interface or a value passed to its constructor, and `main` is the only place that chooses the real implementation. That is the whole dependency-injection story: constructor injection, no container, no global state. The unit tests in each service package construct the same service with generated mocks (section 6.0); the acceptance suite constructs it with the real store, the fake clock and the in-memory publisher.

Each worker is a service with the same shape as the others, minus transport:

```go
package sweeper

type Service interface{ RunOnce(ctx context.Context) (Report, error) }   // Report: appointments marked, cells released
type Middleware func(Service) Service                                     // logging and instrumenting, as in 4.0

type Worker struct{ svc Service; every time.Duration }
func (w Worker) Run(ctx context.Context) func() error {                   // returns the func the errgroup runs
    return func() error {
        t := time.NewTicker(w.every); defer t.Stop()
        for { select { case <-ctx.Done(): return ctx.Err(); case <-t.C: _, _ = w.svc.RunOnce(ctx) } }
    }
}
```

| Worker | Ticker | `RunOnce` behaviour |
|---|---|---|
| No-show sweeper | every minute | `CONFIRMED` with `start_at + grace < now` → `NO_SHOW`, `tx.ReleaseCells` with no kept runs, event, outbox. Grace default 30 minutes (design open question 2) |
| Outbox relay | every 500 ms | Reads unpublished rows in id order, calls `engagement.Publisher.Publish(event)` for each, sets `published_at`. v1 publishers are simple: a structured-log publisher, an in-memory publisher the Gherkin suite inspects, and a confirmation stub that records one message per `AppointmentConfirmed` with the event id as its key so a replay is a no-op. A real bus and the DMS adapter are later swaps behind the same interface |
| Materialiser (deferred) | nightly, when added | For each dealership, each local date to `materialise_horizon_days`, each ACTIVE resource: resolve open/close in Go with the dealership `*time.Location` for that date, convert to UTC, generate the cell starts (`grid.Align` rejects an off-grid opening time), `INSERT IGNORE` them in one batch per resource-day, then delete `FREE` cells outside the window. Until it exists, `data.sql` plays this role for the fixture dealership |

In the acceptance suite the tickers are never started: the suite constructs the services exactly as `main` does and the steps "When the no-show sweeper runs" and "When the relay runs once" call `RunOnce` directly. The harness does the same through its control endpoints (7.4).

---

## 5. Database

### 5.1 Schema delivery: compose init scripts, no migrations in this release

The database for development, the harness and the acceptance suite is a MySQL 8.4 container defined in `docker-compose.yml` at the repository root. Two files under `local/mysql/` are mounted into the image's init directory and executed, in name order, every time the container starts with an empty data directory:

| File | Contents | Rule |
|---|---|---|
| `local/mysql/schema.sql` | The reviewed `schema.sql` with deviations D2 and D3 applied: tables, keys, indexes and cross-column `CHECK` constraints only. No routines, no triggers, no `ENUM`. It contains **every table the design defines**, including `rota_assignment` and `time_slot` that only the deferred materialiser fills, so adding the materialiser later is code only | This is the schema. It is the file reviewed in pull requests; nothing else creates tables |
| `local/mysql/data.sql` | The fixture: one tenant; dealership `MAN01` in `Europe/London` on a 15-minute grid, open Monday to Friday 08:00 to 18:00, closed 2026-12-25; bays `Bay 1` GENERAL, `Bay 2` MOT, `Bay 3` EV_HV; technicians `Sam`, `Priya`, `Lee` with skills and validity dates; service types `INTERIM`, `MAJOR`, `MOT`, `EV_MAJOR`; two customers with vehicles; and `FREE` `time_slot` cells for every ACTIVE resource for each open local date from 2026-10-19 to 2026-10-31, in UTC | Ids are fixed UUIDs so scenarios and the harness can name things. The cell rows are generated inside the script with a recursive CTE over the date window and the opening hours, not listed by hand, so the file stays reviewable and a change to the window is one line |

```yaml
# docker-compose.yml
services:
  mysql:
    image: mysql:8.4
    command: --default-time-zone=+00:00 --character-set-server=utf8mb4
    environment:
      MYSQL_ROOT_PASSWORD: root
      MYSQL_DATABASE: scheduler
      MYSQL_USER: scheduler
      MYSQL_PASSWORD: scheduler
    ports: ["3306:3306"]
    tmpfs: ["/var/lib/mysql"]                       # no volume: every `up` initialises from the two scripts
    volumes:
      - ./local/mysql/schema.sql:/docker-entrypoint-initdb.d/01-schema.sql:ro
      - ./local/mysql/data.sql:/docker-entrypoint-initdb.d/02-data.sql:ro
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "127.0.0.1", "-uroot", "-proot"]
      interval: 2s
      timeout: 2s
      retries: 30
```

Rules:

- **No persistent volume.** The data directory is a `tmpfs`, so `docker compose up` always runs the init scripts and `docker compose down` discards everything. The fixture cannot drift between runs.
- **The init scripts are the only writer of schema and fixture.** The application never creates tables, and no test writes fixture rows except through the public API or the per-scenario reset in 6.1, which replays `data.sql`.
- **Migrations are deferred.** This release has no migration tool and no migrate step. When the schema must evolve in a deployed environment, the plan is goose with `local/mysql/schema.sql` as the baseline migration; it is not part of this release.
- **The materialiser is deferred.** `data.sql` provides the cells the fixture needs. Opening hours and `rota_assignment` rows are present in the fixture for completeness and are read by the `setup` service, but nothing generates cells from them yet.

What was dropped from the design's `schema.sql` and where its job moves:

| Routine | Replaced by |
|---|---|
| `sp_claim_pair` | `Tx.ClaimPair` in `scheduling/mysql` (section 4.5) |
| `sp_release_cells` | `Tx.ReleaseCells` in `scheduling/mysql` |
| `sp_materialise_resource_day` | Deferred with the materialiser. In this release `data.sql` inserts the fixture's cells; later `Tx.InsertCells` in `scheduling/mysql` called by `materialiser.Service.RunOnce` with Go-generated cell starts |
| `trg_appointment_version` | the optimistic update itself: `UPDATE appointment SET ..., version = version + 1 WHERE id = ? AND version = ?` |
| `time_slot.version` | no longer needed for the claim; kept as a flip counter for debugging, bumped by `ClaimPair` and `ReleaseCells` |
| `idempotency_key` table and its expiry sweep | `uq_appointment_booking`, the natural key on `appointment` (deviation D4) |

The comment block above `sp_claim_pair` in the original `schema.sql`, which explains the locking argument, is worth keeping as a comment on `time_slot`, and `schema.sql` in this repo has been updated that way.

The acceptance suite's reset connection has `multiStatements=true` so it can replay `data.sql`; the application pool does not.

### 5.2 Connection rules

```
DSN: user:pw@tcp(host:3306)/scheduler?parseTime=true&loc=UTC&time_zone=%27%2B00%3A00%27&charset=utf8mb4
```

- `parseTime=true&loc=UTC` makes every `DATETIME` a UTC `time.Time`.
- `time_zone='+00:00'` on every connection, as the schema comments require.
- Default isolation (`REPEATABLE READ`). The claim is a locking read and does not depend on the level; the suite runs at the default to prove it.
- No `clientFoundRows`. Nothing in the store depends on affected-row counts any more, so the flag is irrelevant rather than dangerous; leaving it unset keeps the DSN boring.
- Pool: `SetMaxOpenConns(50)`, `SetConnMaxLifetime(30m)`. The booking transaction holds a connection for milliseconds.

### 5.3 Time zones

The design's note (11.6) that `CONVERT_TZ` returns `NULL` without the MySQL time zone tables is sidestepped: the (deferred) materialiser will do the local-to-UTC conversion in Go with `time.LoadLocation(dealership.timezone)` for each date, so DST days of 23 or 25 hours come out right and the database never needs `mysql_tzinfo_to_sql`. The Gherkin suite has a scenario for the last Sunday of March and of October to prove it.

### 5.4 Enumerations live in Go, not in MySQL

Deviation D3. Every `ENUM(...)` column in the original schema becomes a `VARCHAR`, and the set of allowed values is defined once, in Go, in the `enums.go` of the domain that owns the column: `setup/enums.go` for resources, `scheduling/enums.go` for appointments and the ledger.

| Table.column | Go type | Values |
|---|---|---|
| `service_bay.type`, `service_type_bay_type.bay_type` | `setup.BayType` | `GENERAL`, `MOT`, `EV_HV`, `ALIGNMENT`, `BODYSHOP`, `EXPRESS` |
| `service_bay.status` | `setup.BayStatus` | `ACTIVE`, `OUT_OF_SERVICE`, `RETIRED` |
| `technician.status` | `setup.TechnicianStatus` | `ACTIVE`, `INACTIVE` |
| `appointment.kind` | `scheduling.AppointmentKind` | `WAITER`, `DROP_OFF`, `LOANER` |
| `appointment.status` | `scheduling.AppointmentStatus` | `CONFIRMED`, `CHECKED_IN`, `IN_PROGRESS`, `COMPLETED`, `CANCELLED`, `NO_SHOW` |
| `appointment.channel`, `appointment_event.channel` | `scheduling.Channel` | `CUSTOMER_WEB`, `CUSTOMER_APP`, `ADVISOR_CONSOLE`, `CALL_CENTRE`, `INTEGRATION` |
| `time_slot.resource_kind` | `scheduling.ResourceKind` | `BAY`, `TECHNICIAN` |
| `time_slot.status` | `scheduling.CellStatus` | `FREE`, `BOOKED` (phase 2 adds `BLOCKED`, `HELD` here, with no migration) |

The pattern, once per type:

```go
package scheduling // internal/scheduling/enums.go

type AppointmentStatus string

const (
    StatusConfirmed  AppointmentStatus = "CONFIRMED"
    StatusCheckedIn  AppointmentStatus = "CHECKED_IN"
    StatusInProgress AppointmentStatus = "IN_PROGRESS"
    StatusCompleted  AppointmentStatus = "COMPLETED"
    StatusCancelled  AppointmentStatus = "CANCELLED"
    StatusNoShow     AppointmentStatus = "NO_SHOW"
)

var appointmentStatuses = []AppointmentStatus{StatusConfirmed, StatusCheckedIn, StatusInProgress, StatusCompleted, StatusCancelled, StatusNoShow}

func ParseAppointmentStatus(s string) (AppointmentStatus, error) {
    for _, v := range appointmentStatuses { if string(v) == s { return v, nil } }
    return "", fmt.Errorf("%w: appointment status %q", ErrInvalidEnum, s)
}
func (s AppointmentStatus) Valid() bool { _, err := ParseAppointmentStatus(string(s)); return err == nil }

// Scan rejects unknown values coming out of the database; Value writes the string.
func (s *AppointmentStatus) Scan(src any) error        { v, err := ParseAppointmentStatus(asString(src)); *s = v; return err }
func (s AppointmentStatus) Value() (driver.Value, error) { return string(s), nil }

// The state machine is data next to the type, so a transition test is a table test.
var appointmentTransitions = map[AppointmentStatus][]AppointmentStatus{
    StatusConfirmed:  {StatusConfirmed, StatusCheckedIn, StatusCancelled, StatusNoShow},
    StatusNoShow:     {StatusCheckedIn},
    StatusCheckedIn:  {StatusInProgress, StatusCancelled},
    StatusInProgress: {StatusCompleted},
}
func (s AppointmentStatus) CanTransitionTo(t AppointmentStatus) bool
```

Rules:

- **Validate at the edges.** The API layer parses incoming strings with the `Parse*` functions and returns `422 INVALID_VALUE` naming the field. The store's `Scan` rejects an unknown value coming from the database, so a bad row fails loudly at read time instead of propagating.
- **Columns are `VARCHAR(20) NOT NULL`** with the same defaults the `ENUM` columns had. No `CHECK (col IN (...))` constraints: that would put the value list back in the database. The one cross-column constraint that happens to mention values, `ck_slot_owner` (a `FREE` cell has no appointment, a `BOOKED` cell has one), stays, because it encodes an invariant rather than a value list.
- **Lint for it.** A unit test in each domain root asserts that every constant appears in its type's value slice and that `Parse*` round-trips every constant, so a new value cannot be added half-way.
- **Harness and Gherkin use the same constants.** Step definitions build requests from `scheduling.StatusConfirmed`, not from literal strings, so a renamed value breaks the build rather than a test at runtime.

The trade-off accepted: the database will store any 20-character string if something bypasses the Go code. In v1 nothing does, and the Gherkin suite plus `Scan` validation is the guard. If a second writer ever appears, the `CHECK` constraints are one migration away.

---

## 6. Tests

Two layers, with a clear job each. Unit tests prove the logic of a service in isolation against mocks of its dependencies. Gherkin acceptance tests prove the behaviour of the whole process against the real database. Neither replaces the other: the unit tests make the retry loop, the state machine and the error branches cheap to cover exhaustively; the acceptance tests are the only proof that the claim, the locks and the schema do what the design says.

### 6.0 Unit tests: mocks and constructor injection

Every service package depends only on interfaces declared by its domain root and by `platform` (`scheduling.Store`, `scheduling.Tx`, `scheduling.WorkshopReader`, `scheduling.CustomerReader`, `platform.Clock`, `platform.IDs`, `engagement.Publisher`). Its unit tests construct the service with generated mocks of those interfaces and drive the methods directly, with no HTTP and no database.

- **Mocks are generated, not hand-written.** `go.uber.org/mock` (`mockgen`) with a `//go:generate` directive on each interface, output to `mock_<name>_test.go` in the same package. Being `_test.go` files, the mocks are invisible to production code and to other packages. `go generate ./...` is run in CI and the diff must be empty.
- **Constructor injection only.** `NewService(store, clock, ids)` is the single way to build a service, in `main` and in tests alike. No package-level variables, no init-time wiring, no DI container.
- **What a service unit test covers.** For `Book`: each branch of the candidate loop (slot taken moves to the next pair; deadlock victim retries the same pair once; missing cells recompute once then fail with `AVAILABILITY_CHANGED`; duplicate booking key returns the existing appointment or `DUPLICATE_BOOKING`); that `InsertAppointment` is the first call inside `InTx` and `ClaimPair` the second; that the event and outbox appends happen inside the same `InTx`. The mock `Tx` records the call order so the correctness argument from 4.4 is asserted, not assumed. For `sweeper.RunOnce`: the grace boundary, with the mock clock. For every service: the transition table in the domain root is consulted and an illegal transition never reaches the store.
- **What they deliberately do not cover.** Locking, isolation, deadlocks, unique indexes and anything else the database decides. Those belong to 6.1 and can only be proved there.
- **Endpoints and transports** get table tests with `httptest` against a mock `Service`: every status code and problem-details body in 4.6, and every decoder rejection.
- **Domain roots** need no mocks; they are pure functions and types with unit and `rapid` property tests (4.3).

```go
package service // internal/scheduling/service

//go:generate mockgen -destination=mock_scheduling_test.go -package=service scheduler/internal/scheduling Store,Tx,WorkshopReader,CustomerReader

func TestBook_SlotTakenMovesToNextPair(t *testing.T) {
    ctrl := gomock.NewController(t)
    store, tx := NewMockStore(ctrl), NewMockTx(ctrl)
    store.EXPECT().InTx(gomock.Any(), gomock.Any()).DoAndReturn(runWith(tx)).Times(2)
    gomock.InOrder(
        tx.EXPECT().InsertAppointment(gomock.Any()).Return(nil),
        tx.EXPECT().ClaimPair(gomock.Any(), "bay-1", gomock.Any(), "sam", gomock.Any(), gomock.Any(), "").Return(scheduling.ErrSlotTaken),
        tx.EXPECT().InsertAppointment(gomock.Any()).Return(nil),
        tx.EXPECT().ClaimPair(gomock.Any(), "bay-2", gomock.Any(), "sam", gomock.Any(), gomock.Any(), "").Return(nil),
        tx.EXPECT().AppendEvent(gomock.Any(), "CONFIRMED", gomock.Any(), gomock.Any(), gomock.Any()).Return(nil),
        tx.EXPECT().AppendOutbox("Appointment", gomock.Any(), "AppointmentConfirmed", gomock.Any()).Return(nil),
    )
    svc := NewService(store, workshopWith(req, cfg), ownerOf("veh-1", "cust-1"), platform.FixedClock(t0), platform.FixedIDs("appt-1"))
    appt, err := svc.Book(ctx, cmdWithPairs("bay-1", "bay-2"))
    // ...
}
```

### 6.1 Gherkin acceptance tests: what they are and are not

- **Black-box through HTTP.** Steps call the real handlers over an `httptest.Server` wired exactly as `main` wires them (same middleware chain, same error encoder), against the real MySQL 8.4 from `docker-compose.yml`. No step touches a repository directly, with three exceptions: ledger assertions (`Then exactly 8 cells are BOOKED`) read `time_slot` through a read-only helper, clock steps set the injected `clock.Fake`, and `Given "Bay 1" is booked from ... to ...` pins a booking to a named pair through the `harness/control` package, because the public API never accepts a pair.
- **One database per `go test` run.** The suite connects to the compose database (`SCHEDULER_TEST_DSN`, default the compose values) and fails fast with a clear message if it is not up. It does not start containers itself: `docker compose up -d --wait` is the one manual step, locally and in CI, and the init scripts have already created the schema and the fixture by the time the health check passes.
- **Every scenario starts from the fixture.** The `Before` hook truncates every table in dependency order and replays `local/mysql/data.sql` through the `multiStatements` reset connection (well under 100 ms). Scenarios therefore refer to the fixture by name (`MAN01`, `Bay 1`, `Sam`, `INTERIM`) and `Background` steps only add to it; nothing a scenario does survives it.
- **Deterministic.** The clock is fake and set to a date inside the fixture's cell window, the candidate-pair shuffle seed is fixed per scenario, and worker tickers are never started: a job runs only when a step calls its `RunOnce`.
- **Strict.** `Strict: true` so an undefined or pending step fails the build.

### 6.2 Layout

```
features/
  dealership.feature            opening hours, exceptions, bays, technicians, skills with validity
  catalogue.feature             service types, skills AND, bay types OR, online flag
  availability.feature          feasible starts, split-across-bays case, skill validity by date, buffers
  booking.feature               happy path, 409 with alternatives, 422s, id returned as reference
  claim_ledger.feature          every row of design 11.3's validation table
  idempotency.feature           replay returns the same appointment, same booking key with a different service type is 409 DUPLICATE_BOOKING, concurrent double submit yields one appointment, retry after failure runs again, rebooking the same minute after a cancel is 409 DUPLICATE_BOOKING
  concurrency.feature           N-client race, lock scope, reschedule-vs-book deadlock
  reschedule.feature            move, self-overlap, reassign, failed claim keeps original, stale version
  lifecycle.feature             check-in, start, complete frees future cells, cancel, no-show, late arrival
  outbox.feature                event per transition, relay marks published, one confirmation per event, replay is a no-op
  harness_smoke.feature         harness pages render; one booking through the harness form
test/acceptance/
  suite_test.go                       TestFeatures: godog.TestSuite, flags bound as -godog.*
  world.go                            per-scenario state: server, client, last response, ids by name
  db.go                               connect to the compose database, truncate in dependency order, replay local/mysql/data.sql
  steps_setup.go steps_availability.go steps_booking.go steps_lifecycle.go steps_ledger.go steps_clock.go steps_events.go
```

```go
func TestFeatures(t *testing.T) {
    suite := godog.TestSuite{
        TestSuiteInitializer: initSuite,        // connect to the compose MySQL, build the app once
        ScenarioInitializer:  initScenario,     // Before: truncate + replay data.sql + fresh fake clock; register steps
        Options: &godog.Options{
            Format: "pretty", Paths: []string{"../../features"},
            Strict: true, Tags: opts.Tags, TestingT: t, Concurrency: 1,
        },
    }
    if suite.Run() != 0 { t.Fatal("acceptance suite failed") }
}
```

Run everything: `go test ./test/acceptance/ -run TestFeatures`. One feature: `-godog.tags=@spec-003`. Skip slow races: `-godog.tags='~@slow'`.

### 6.3 Tags for traceability

| Tag | Meaning |
|---|---|
| `@R1` `@R2` `@R3` | Which brief requirement the scenario proves (design 1.3) |
| `@spec-001` ... `@spec-006` | Which Spec Kit feature owns it; `/speckit-converge` filters on this |
| `@design-11.3` | Row of a design table it reproduces |
| `@slow` | Races and sleeps; still run in CI, skippable locally |
| `@wip` | Allowed red on a feature branch, never on `main` |

### 6.4 Step vocabulary

Steps are written in the domain's language and reused across features. Names map to ids inside the world, so features never contain UUIDs.

```
Given a dealership "MAN01" in timezone "Europe/London" with a 15 minute grid
  And it is open Monday to Friday from 08:00 to 18:00
  And it is closed on 2026-12-25 for "Christmas Day"
  And bay "Bay 1" of type "GENERAL" and bay "Bay 3" of type "EV_HV"
  And technician "Sam" with skills "EV_HV_L3,GENERAL_SERVICE"
  And technician "Priya" with skill "MOT_TESTER" valid until 2026-10-31
  And service type "EV_MAJOR" taking 120 minutes with buffers 15/15 requiring skills "EV_HV_L3" on bay types "EV_HV"
  And customer "J. Patel" with vehicle "YK21 ABC"
  And the ledger has cells for 2026-10-20          # asserts the fixture window; nothing is generated
  And the clock reads 2026-10-19T09:00:00Z

When I ask for availability for "EV_MAJOR" on 2026-10-20 as a customer
When "Alice" books "EV_MAJOR" for "YK21 ABC" at 2026-10-20T09:00+01:00   # vehicle, customer, start and kind are the booking key; no header
When 20 customers book "INTERIM" at 2026-10-20T08:00+01:00 at the same time
When the appointment is rescheduled to 2026-10-20T13:30+01:00
When the appointment is reassigned to technician "Priya"
When the no-show sweeper runs
When the relay runs once

Then the available starts include 09:00, 09:15 and 09:30
Then the available starts do not include 09:00
Then the response is 201 with an id that is a UUID
Then the response is 409 "SLOT_TAKEN" with at least 2 alternatives
Then exactly 1 booking succeeded and 19 received SLOT_TAKEN
Then exactly 8 cells are BOOKED and 1 appointment exists
Then bay "Bay 3" cells from 08:45 to 11:15 on 2026-10-20 belong to that appointment
Then technician "Sam" has 4 FREE cells from 10:00 on 2026-10-20
Then the appointment status is "COMPLETED"
Then an "AppointmentConfirmed" event is in the outbox for that appointment
Then one confirmation message was recorded for that appointment
```

### 6.5 Example features

**`features/availability.feature`** (the design's named unit test, as a scenario)

```gherkin
@R2 @spec-002
Feature: Availability search
  A start time is offered only when some single bay and some single technician
  each have a free run long enough for the whole job including buffers.

  Background:
    Given a dealership "MAN01" in timezone "Europe/London" with a 15 minute grid
    And it is open Monday to Friday from 08:00 to 18:00
    And bay "Bay 1" of type "GENERAL" and bay "Bay 2" of type "GENERAL"
    And technician "Sam" with skill "GENERAL_SERVICE"
    And service type "INTERIM" taking 30 minutes with no buffers requiring skill "GENERAL_SERVICE" on bay types "GENERAL"
    And the ledger has cells for 2026-10-20
    And the clock reads 2026-10-19T09:00:00Z

  Scenario: A run split across two bays is not a slot
    Given "Bay 1" is booked from 09:15 to 10:00 on 2026-10-20
    And "Bay 2" is booked from 08:00 to 09:15 on 2026-10-20
    When I ask for availability for "INTERIM" on 2026-10-20 as a customer
    Then the available starts do not include 09:00
    And the available starts include 08:00 and 10:00

  Scenario: Buffers are hidden from the customer but reserved on the bay
    Given service type "MAJOR" taking 90 minutes with buffers 15/15 requiring skill "GENERAL_SERVICE" on bay types "GENERAL"
    When "Alice" books "MAJOR" for "YK21 ABC" at 2026-10-20T09:00+01:00
    Then the response is 201
    And the appointment runs from 09:00 to 10:30
    And bay "Bay 1" cells from 08:45 to 10:45 on 2026-10-20 belong to that appointment

  Scenario: A skill that has expired no longer qualifies the technician
    Given technician "Priya" with skill "MOT_TESTER" valid until 2026-10-19
    And service type "MOT" taking 45 minutes with no buffers requiring skill "MOT_TESTER" on bay types "GENERAL"
    When I ask for availability for "MOT" on 2026-10-20 as a customer
    Then the response is 422 "NO_QUALIFIED_TECHNICIAN"
```

**`features/concurrency.feature`** (design 5.3 and the validated race)

```gherkin
@R2 @spec-003 @slow
Feature: Concurrent bookings never double book
  Two requests can never both get the same bay or technician for overlapping cells,
  with no distributed lock and at MySQL's default isolation level.

  Background:
    Given a dealership "MAN01" in timezone "Europe/London" with a 15 minute grid
    And it is open Monday to Friday from 08:00 to 18:00
    And bay "Bay 1" of type "GENERAL"
    And technician "Sam" with skill "GENERAL_SERVICE"
    And service type "INTERIM" taking 60 minutes with no buffers requiring skill "GENERAL_SERVICE" on bay types "GENERAL"
    And the ledger has cells for 2026-10-20

  @design-11.3
  Scenario: Twenty clients race for one pair and one hour
    When 20 customers book "INTERIM" at 2026-10-20T08:00+01:00 at the same time
    Then exactly 1 booking succeeded and 19 received SLOT_TAKEN
    And exactly 8 cells are BOOKED and 1 appointment exists

  Scenario: Two customers, one slot, two pairs
    Given bay "Bay 2" of type "GENERAL" and technician "Priya" with skill "GENERAL_SERVICE"
    When "Alice" and "Bob" book "INTERIM" at 2026-10-20T09:00+01:00 at the same time
    Then both bookings succeeded
    And they hold different bays and different technicians

  @design-11.3
  Scenario: A reschedule racing a booking across its old and new cells ends consistent
    Given "Alice" has booked "INTERIM" at 2026-10-20T13:00+01:00
    When the appointment is rescheduled to 2026-10-20T13:30+01:00 while "Bob" books "INTERIM" at 2026-10-20T13:15+01:00 at the same time
    Then "Bob" received SLOT_TAKEN
    And exactly 8 cells are BOOKED, all owned by Alice's appointment
    And no cell is owned by a missing appointment
```

The race step fires N goroutines that each build their request, wait on a shared `sync.WaitGroup`-style barrier, then send together; outcomes are collected into the world and asserted in `Then` steps. The deadlock scenario asserts the invariant (one winner, ledger consistent), not which side wins, because InnoDB chooses the victim.

**`features/lifecycle.feature`** (excerpt)

```gherkin
@R3 @spec-004
Feature: Appointment lifecycle

  Scenario: Completing early frees the remaining cells for a walk-in
    Given "Alice" has booked "INTERIM" at 2026-10-20T09:00+01:00 on bay "Bay 1" with technician "Sam"
    And the appointment is checked in and started
    And the clock reads 2026-10-20T09:40:00+01:00
    When the appointment is completed
    Then the appointment status is "COMPLETED"
    And the appointment owns 6 BOOKED cells, 3 on bay "Bay 1" and 3 on technician "Sam"
    And bay "Bay 1" cell 08:45 UTC on 2026-10-20 is FREE
    And technician "Sam" cell 08:45 UTC on 2026-10-20 is FREE
    And an "AppointmentCompleted" event is in the outbox for that appointment

  Scenario: A no-show past the grace period releases every cell
    Given "Alice" has booked "INTERIM" at 2026-10-20T09:00+01:00
    And the clock reads 2026-10-20T09:31:00+01:00
    When the no-show sweeper runs
    Then the appointment status is "NO_SHOW"
    And technician "Sam" has 4 FREE cells from 08:00 UTC on 2026-10-20
```
