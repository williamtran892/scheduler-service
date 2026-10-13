# Scheduler Service

Unified service scheduler for dealership workshops: one Go process, structured with
[go-kit](https://github.com/go-kit/kit), backed by MySQL 8.4, with behaviour specified as
Gherkin features and built through [Spec Kit](https://github.com/github/spec-kit).

The design extracts this repository implements are in [`docs/design/`](./docs/design) (start with its [README](./docs/design/README.md)); the API contract is [`api/openapi.yaml`](./api/openapi.yaml). How the system works today
is in [`docs/`](./docs). The project rules are in the
[constitution](./.specify/memory/constitution.md).

> **Status:** features 001 and 002 are delivered. Booking: `POST /appointments` and
> `GET /appointments/{appointmentId}`. Appointment lifecycle: reschedule (also reassign), cancel,
> check-in (also the late arrival of a no-show), start, complete and no-show, plus a background
> sweeper that marks overdue appointments as no-shows, with the schema, the fixture and both test
> layers behind them. Listing, availability search, authentication and the outbox relay are not
> built yet; each arrives as a Spec Kit feature under `specs/`.
> [`docs/features.md`](./docs/features.md) lists what is delivered.

## Prerequisites

| Tool | Version | Install |
|---|---|---|
| Go | 1.26+ | https://go.dev/dl |
| Docker with the compose plugin | current | Docker Desktop or OrbStack |
| golangci-lint | v2 | `brew install golangci-lint` (only for `make lint`) |
| `uv` + `specify-cli` | current | `uv tool install specify-cli` (only for Spec Kit work) |

`mockgen` needs no install. It is a Go tool dependency in `go.mod` and runs through `go tool mockgen`.

## Quick start

Everything in containers:

```bash
make up                 # build the image, start the app on :8080 and MySQL 8.4 on :3306
curl localhost:8080/healthz
# {"status":"ok"}
make down               # stop both and discard all data
```

Or run the service from source against the containerised database:

```bash
make db-up              # MySQL only
make run                # go run ./cmd/scheduler on :8080
```

`make help` lists every target.

## Tests

There are two layers. Neither one replaces the other.

| Layer | Where | Runs against | Command |
|---|---|---|---|
| Unit | `internal/**/*_test.go` | Mocks (`go.uber.org/mock`), no database | `make test` |
| Acceptance (Gherkin) | `features/*.feature`, steps in `test/acceptance/` | The service container over HTTP, with the compose MySQL behind it (the `@clock` scenarios also write to it to backdate an appointment) | `make test-integration` |

```bash
make test                                # unit tests with -race
make test-integration                    # rebuild image, compose up app + MySQL, run the suite
make test-integration TAGS=@bootstrap    # only scenarios with a tag
make test-integration TAGS=@clock        # the time-dependent scenarios: sweeper, late arrival
make test-integration TAGS='~@slow'      # skip slow scenarios
make cover                               # unit coverage report in coverage.html
make ci                                  # everything CI runs
```

How `make test-integration` works (`scripts/integration.sh`):

1. **Rebuild the application.** A static Linux binary for the Docker engine's architecture goes to
   `bin/linux/scheduler`. It uses your local Go build cache, so it is fast.
2. **Build it into a container.** `docker compose build app` packages the binary into the
   `scheduler:local` image, using the `Dockerfile` (Alpine, non-root user). The Dockerfile copies
   the binary and does not compile anything.
3. **Start the app and MySQL** with `docker compose up -d --wait`. MySQL becomes healthy once its
   init scripts have run. The app starts only after that and is healthy once `GET /healthz`
   answers `200`. If the app container is recreated because the image changed, it runs the new
   build. If the app never becomes healthy, the script prints its log and fails.
4. **Run the suite.** `go test ./test/acceptance/` runs with `SCHEDULER_BASE_URL=http://127.0.0.1:8080`
   and `SCHEDULER_TEST_DSN` pointing at the compose MySQL. The suite is a black-box client: it
   uses the app's HTTP API, and the one time it connects to the database is a `@clock` scenario's
   backdating step, which moves an appointment into the past and never reads to assert. A run
   without `@clock` scenarios never opens the connection. Strict mode is on, so an undefined or
   pending step fails the run. If the suite fails, the script prints the app container's log.
5. **Tear down.** Both containers are always removed, pass or fail, and the database goes with
   them. Every run starts from `schema.sql` and `data.sql` and leaves nothing behind.

The database is not reset between scenarios within a run. Each scenario creates or picks its own
data and must not depend on what an earlier scenario left behind.

Running `go test ./test/acceptance/` on its own fails straight away with a message telling you to
use `make test-integration`, because nothing is serving `SCHEDULER_BASE_URL`.

Mocks are generated, not written by hand. Add a `//go:generate go tool mockgen ...` directive
next to the interface, then run `make generate`. CI fails if the generated files are stale.

## Configuration

Everything is read from the environment in `internal/platform/config.go`.

| Variable | Default | Meaning |
|---|---|---|
| `HTTP_ADDR` | `:8080` | Listen address |
| `MYSQL_DSN` | the compose database (see `platform.DefaultDSN`) | Must keep `parseTime=true&loc=UTC&time_zone=%27%2B00%3A00%27` |
| `OTEL_EXPORTER` | `none` | `none`, `stdout` or `otlp`. The OTLP exporter reads the standard `OTEL_EXPORTER_OTLP_*` variables |
| `OTEL_SERVICE_NAME` | `scheduler` | `service.name` resource attribute |
| `SHUTDOWN_TIMEOUT` | `10s` | Grace period for in-flight requests on SIGINT or SIGTERM |
| `NO_SHOW_GRACE` | `30m` | A `CONFIRMED` appointment whose start plus this has passed is marked `NO_SHOW` by the sweeper. A Go duration greater than zero |
| `NO_SHOW_SWEEP_INTERVAL` | `1m` | How often the no-show sweeper runs. A Go duration greater than zero; `docker-compose.yml` sets `1s` for the acceptance suite |
| `MYSQL_PORT` | `3306` | Host port docker compose publishes MySQL on |
| `SCHEDULER_BASE_URL` | set by `scripts/integration.sh` | Service the acceptance suite sends requests to |
| `SCHEDULER_TEST_DSN` | set by `scripts/integration.sh` | The compose MySQL, for the `@clock` backdating step only (see [Development Standards](./docs/development.md)) |
| `APP_PORT` | `8080` | Host port docker compose publishes the app container on |
| `GODOG_TAGS` | none | Tag filter for the suite. `make test-integration TAGS=...` sets it |
| `GODOG_FORMAT` | `pretty` | godog formatter, for example `junit:/abs/path/acceptance.xml` |

## Database

`docker-compose.yml` runs two services: `app`, the `scheduler:local` image, and `mysql`, MySQL 8.4
on a tmpfs data directory with no volume.

- A fresh MySQL container always starts from an empty database. It runs
  `local/mysql/schema.sql`, then `local/mysql/data.sql`.
- `make db-up` starts MySQL only. `make up` starts both.
- `make db-reset` recreates MySQL from scratch. `make down` stops everything and discards the data.
- `make db-shell` opens a mysql prompt.

There is no migration tool in this release. `schema.sql` is the schema, and the application never
creates tables.

## Repository layout

```
cmd/scheduler/          main: config, logger, OTel, MySQL pool, HTTP server and no-show sweeper in an errgroup
internal/app/           NewHandler: the one mux, with each domain's endpoint specs mounted on it
internal/platform/      config, clock, ids, MySQL pool, problem-details encoder, health, OTel
internal/setup/         Workshop Setup (read-only): mysql implements scheduling.WorkshopReader over
                        dealerships, bays, technicians, service types
internal/customers/     Customer & Vehicle (read-only): mysql implements scheduling.CustomerReader
internal/scheduling/    Scheduling: grid and availability, the claim and release, appointments and
                        their lifecycle (root, mysql, service, endpoint, sweeper); the root keeps
                        every shared struct and the reader interfaces in service.go; mysql is its
                        data access and declares Store and Tx (store.go); endpoint is the HTTP
                        surface, two files: endpoint.go (route table and go-kit endpoints) and
                        facade.go (request decoders and response encoders)
local/mysql/            schema.sql and data.sql, the compose init scripts
features/               Gherkin feature files
test/acceptance/        godog suite: a black-box HTTP client with relative dates and fixture names;
                        database access only in the @clock backdating step, no reset between
                        scenarios
scripts/build-image.sh  builds the Linux binary and the scheduler:local image
scripts/integration.sh  build-image, compose up app + MySQL, wait until healthy, run the suite
Dockerfile              packages bin/linux/scheduler into a small Alpine image
docs/                   living documentation (architecture, features, development standards)
specs/                  Spec Kit feature folders (created per feature)
.specify/               Spec Kit memory, templates and scripts
```

Scheduling follows the layout of [`docs/design/IMPLEMENTATION_NOTES.md`](./docs/design/IMPLEMENTATION_NOTES.md)
section 2; Setup and Customers are only a `mysql` package each for now, and `engagement` is added
when a feature needs it. [`docs/architecture.md`](./docs/architecture.md) describes the packages
and their dependency rules.

## Feature workflow

Every feature follows the Spec Kit loop and the gates in the constitution:

```
/speckit-specify → /speckit-clarify → /speckit-plan → /speckit-checklist
→ /speckit-tasks → /speckit-analyze → /speckit-implement → /speckit-converge
```

The Gherkin scenarios are written and approved first and committed failing. A feature is done
when its tagged scenarios pass, unit tests and lint pass, and `docs/` has been updated.
