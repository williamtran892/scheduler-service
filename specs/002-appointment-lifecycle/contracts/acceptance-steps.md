# Acceptance step vocabulary (feature 002)

The steps used by `features/lifecycle.feature`, `features/reschedule.feature` and
`features/noshow.feature`. Feature 001's vocabulary (`docs/development.md`, `test/acceptance/`) is
reused wherever it fits; new steps exist only for the transitions, the backdating of `@clock`
scenarios and polling, plus the few record assertions the transitions need. Names map to the fixed
ids of `local/mysql/data.sql`, so feature files never contain UUIDs except an explicitly unknown
one.

## Conventions

- `<day>` is `tomorrow`, `yesterday` or `in N days`, counted from the suite's "today" in
  Europe/London; `<HH:MM>` is local time at the dealership. As in feature 001, booking steps say
  `<day> at <HH:MM>`; reschedule and record steps say `<HH:MM> <day>`, like "runs from 09:00 to
  09:45 in 9 days".
- "the appointment" is the most recent appointment returned (201 or 200) in the scenario. A
  transition answered 200 makes its record the appointment. `the appointment of "<customer>"` is
  the last appointment that customer booked in the scenario.
- Every transition sends `expectedVersion` equal to the version of the appointment's last record
  the suite saw, unless the step says `with version N`, and `X-Channel: ADVISOR_CONSOLE` unless it
  says `through "<CHANNEL>"`. Bodies carry nothing else unless the step says so.
- Only steps marked **@clock** touch the database, and only to backdate.

## Reused from feature 001, unchanged

`the workshop fixture for dealership "<CODE>"`; `"<customer>" has booked ...` and
`"<customer>" books "<SERVICE>" for "<registration>" <day> at <HH:MM>` (with `through
"<CHANNEL>"`); `the appointment is fetched with its events`; `the appointment is fetched through
"<CHANNEL>"`; `the request has "<field>" removed` / `set to "<value>"` / `no channel`; `the
response is <status>`; `the response is <status> "<CODE>"`; `... naming the field "<field>"`;
`the response is 201 with an id that is a UUID`; `the response is 409 "SLOT_TAKEN" with between N
and M alternatives`; `the appointment status is "<STATUS>" at version N`; `the appointment runs
from <HH:MM> to <HH:MM> <day>`; `the appointment is on bay "<bay>" with technician "<name>"`; `the
appointment owns N bay|technician cells from <HH:MM> to <HH:MM>`; `the record shows no ledger
cells and no notes`; `the audit trail has exactly one "<TYPE>" event through "<CHANNEL>"`; `the
problem carries the first booking as the existing appointment with service type "<SERVICE>"`.

## Extended (same wording, one more option)

| Step | Extension |
|---|---|
| `"<customer>" has booked "<SERVICE>" for "<registration>" <day> at <HH:MM> at dealership "<CODE>"` | The `Given` booking step accepts the optional dealership the `When` step already has |
| `the appointment of "<customer>" is fetched through "<CHANNEL>"` | Fetch a named customer's appointment rather than the current one |
| `the request has "<field>" set to <integer>` | Sets a JSON number (the existing step sets a string), e.g. `"expectedVersion" set to 0`, `"mileage" set to -1` |
| `the workshop fixture for dealership "LDS01"` | `LDS01` now has `Bay L1` and `Jo` |

## New: transitions

| Step | Request |
|---|---|
| `the appointment[ of "<customer>"] is checked in` | `POST .../check-in` |
| `the appointment is started` | `POST .../start` |
| `the appointment is started with a start time of <HH:MM> <day>` | `POST .../start` with `startedAt` |
| `the appointment is completed` | `POST .../complete` |
| `the appointment is cancelled` | `POST .../cancel` |
| `the appointment is marked a no-show` | `POST .../no-show` |
| any of the five above `... with version N` | sends `expectedVersion: N` |
| any of the five above `... through "<CHANNEL>"` | sends that `X-Channel` (after `with version N` when both) |
| `the appointment is rescheduled to <HH:MM> <day>[ with version N][ through "<CHANNEL>"]` | `POST .../reschedule` with `startAt` |
| `the appointment is reassigned to technician "<name>"[ through "<CHANNEL>"]` | `POST .../reschedule` with `technicianId` only |
| `the appointment is reassigned to bay "<bay>"` | `POST .../reschedule` with `serviceBayId` only |
| `appointment "<uuid>" is cancelled` | `POST /appointments/<uuid>/cancel` with `expectedVersion: 1` |
| `a "<operation>" request is prepared for the appointment` | Builds, without sending, the request for `reschedule`, `cancel`, `check-in`, `start`, `complete` or `no-show` with the current `expectedVersion` (a reschedule also carries the current `startAt`), for the `the request has ...` edits |
| `the transition request is sent` | Sends the prepared request |

`Given` forms, each of which also asserts `200`: `the appointment has been checked in`, `... started`,
`... completed`, `... cancelled`, `... marked a no-show`, `the appointment has been rescheduled to
<HH:MM> <day>`, `the appointment has been reassigned to bay "<bay>" with technician "<name>"`.

`the appointment has reached "<STATUS>"` drives a confirmed appointment along the legal path to
that status (`CONFIRMED`: nothing; `CHECKED_IN`: check in; `IN_PROGRESS`: check in, start;
`COMPLETED`: check in, start, complete; `CANCELLED`: cancel; `NO_SHOW`: mark a no-show), asserting
`200` at each step.

## New: record assertions

| Step | Checks |
|---|---|
| `the appointment owns no ledger cells` | The staff record has no `cells.bay` and no `cells.technician` |
| `the appointment owns N bay cells and M technician cells` | Counts only, for records whose local times a backdated or late-arrival appointment makes unpredictable |
| `the appointment has an actual start time and no actual end time` | `actualStartAt` is an RFC 3339 instant, `actualEndAt` is `null` |
| `the appointment has an actual start time and an actual end time` | Both are RFC 3339 instants |
| `the appointment's actual start is <HH:MM> <day>` | `actualStartAt` equals that instant |
| `the audit trail has exactly one "<TYPE>" event by "<actor>"` | One event of that type, with that actor and no channel |

## New: time (`@clock` scenarios only)

| Step | Meaning |
|---|---|
| `the appointment[ of "<customer>"] is backdated by N days` | **@clock.** In one transaction over `SCHEDULER_TEST_DSN`: subtract N days from the appointment's `start_at` and `end_at` and from the `slot_start` of every `time_slot` row it owns; fail unless exactly one appointment row changed. Nothing else is read or written. Lives in a `_test.go` file so the ledger guard does not see it |
| `the appointment[ of "<customer>"] becomes "<STATUS>" within N seconds` | Polls `GET /appointments/{id}` every 200 ms until the status matches, failing at the deadline with the last record; the last fetch becomes the appointment |
| `the appointment starts at the current grid cell` | `startAt` is the 15-minute cell containing the moment the check-in was sent or answered, allowing one cell of clock difference between the suite and the container |
