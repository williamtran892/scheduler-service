# System Architecture

This page describes how the system is built today. Update it in the same change that alters it.

## Process

The service is one binary, `cmd/scheduler`, and it runs in one mode:

1. It loads configuration from the environment (`platform.LoadConfig`).
2. It installs OpenTelemetry providers for the configured exporter (`platform.SetupOTel`).
3. It opens the MySQL pool and pings it (`platform.OpenMySQL`). The schema must already exist.
4. It builds the HTTP handler (`app.NewHandler`) and serves it.

The HTTP server runs in an `errgroup` under a context that SIGINT or SIGTERM cancels. When the
context ends, the server shuts down gracefully within `SHUTDOWN_TIMEOUT`. Background workers join
the same group when they exist.

## Packaging

The `Dockerfile` packages a prebuilt static Linux binary (`bin/linux/scheduler`, built by
`scripts/build-image.sh`) into an Alpine image that runs as a non-root user. `docker-compose.yml`
runs that image as the `app` service next to `mysql`. The app starts once MySQL is healthy and is
healthy itself once `GET /healthz` answers `200`.

## Packages

| Package | Responsibility |
|---|---|
| `cmd/scheduler` | Wiring only. Chooses the real implementation of every dependency |
| `internal/app` | `NewHandler`: the one mux, every route, the `otelhttp` wrapper |
| `internal/platform` | Cross-cutting pieces: `Config`, `Clock` (`RealClock`, `FakeClock`), `IDs` (`UUIDs`), `OpenMySQL`, `EncodeError` and `Problem`, `Health`, `SetupOTel`. Imports no domain package |

## HTTP

| Route | Behaviour |
|---|---|
| `GET /healthz` | `200 {"status":"ok"}` if the database answers a ping within 2 seconds, otherwise `503` with problem code `DATABASE_UNAVAILABLE` |

Errors use RFC 9457 problem details (`application/problem+json`) with the fields `type`, `title`,
`status`, `code` and `detail`. `platform.EncodeError` is the only place that maps errors to
status codes:

| Error | Status | Code |
|---|---|---|
| wraps `platform.ErrBadRequest` | 400 | `BAD_REQUEST` |
| anything else | 500 | `INTERNAL` (no detail is returned) |

## Data

MySQL 8.4. The schema lives in `local/mysql/schema.sql` and the fixture in `local/mysql/data.sql`.
Both run as container init scripts, and the application never creates tables. Every connection
uses UTC (`parseTime=true&loc=UTC&time_zone='+00:00'`). There are no tables yet.

## Observability

- Logging: `go-kit/log` in logfmt to stderr, with `ts` and `caller`.
- Traces and metrics: OpenTelemetry. `otelhttp` opens one server span per request, named after
  the matched route pattern. The exporter is `none` (default), `stdout` or `otlp`.
