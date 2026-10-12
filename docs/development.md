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
  an interface.
- **Acceptance tests** are Gherkin scenarios run by `make test-integration`, which rebuilds the
  `scheduler:local` image, starts it with a fresh MySQL through docker compose, waits until both
  are healthy, runs `test/acceptance` against the container, then removes both containers. The
  suite is a black-box client: steps use only the HTTP API and never touch the database, not even
  for checks or cleanup. Each run starts from `local/mysql/data.sql`, but the database is not reset
  between scenarios, so a scenario creates or picks its own data and never depends on another. A step that needs to control time or run a worker uses a
  test-only control endpoint on the service. It never touches Go objects directly.
- **Time.** Never read the wall clock in logic. Take a `platform.Clock`: unit tests use
  `FakeClock` directly, and acceptance tests set it through a control endpoint.
- **Tags.** `@spec-NNN` names the feature that owns a scenario, `@slow` marks races and sleeps,
  and `@wip` is allowed on a branch but never on `main`.
- **Names** state behaviour in plain language, for example `TestHealth_UnavailableWhenThePingFails`.

## Code

- `make fmt` and `make lint` must pass. Imports are grouped as standard library, third-party,
  then `scheduler/...`.
- Errors carry enough context to find the cause: `fmt.Errorf("ping MySQL: %w", err)`.
- Only `platform.EncodeError` maps errors to HTTP status codes.
- Only the schema file creates tables. Add tables to `local/mysql/schema.sql` and fixture rows
  to `local/mysql/data.sql`.

## Before you push

```bash
make ci
```
