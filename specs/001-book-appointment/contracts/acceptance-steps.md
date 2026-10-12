# Acceptance step vocabulary (feature 001)

The steps used by `features/booking.feature`, `features/claim_ledger.feature`,
`features/idempotency.feature` and `features/concurrency.feature`. Adapted from implementation
notes 6.4 to decision 3: no fake clock, no database reads, no harness, no idempotency header.
Names map to the fixed ids of `local/mysql/data.sql` inside the suite, so feature files never
contain UUIDs except an explicitly unknown one.

## Conventions

- `<day>` is `tomorrow`, `yesterday` or `in N days`, counted from the suite's "today" in
  Europe/London, fixed once when the suite starts. `<HH:MM>` is local time at the dealership on
  that day; the suite converts it to an instant with the dealership's time zone.
- Defaults for every booking: dealership `MAN01`, kind `DROP_OFF`, channel `ADVISOR_CONSOLE`
  (sent as `X-Channel`), no customer requests, no notes.
- "the appointment" is the most recent appointment returned by a booking or a fetch in the
  scenario; "the first booking" is the first appointment confirmed in the scenario.
- Times in `Then` steps are local times on the appointment's day.

## Given

| Step | Meaning |
|---|---|
| `the workshop fixture for dealership "MAN01"` | Binds fixture names to ids; checks the service is healthy. Creates nothing |
| `"<customer>" has booked "<SERVICE>" for "<registration>" <day> at <HH:MM>` (optionally `with:` a table) | Same as the `When` booking step, then asserts `201` and remembers the appointment |
| `"<customer>" prepares a booking of "<SERVICE>" for "<registration>" <day> at <HH:MM>` | Builds a request without sending it |
| `the request has "<field>" removed` / `"<field>" set to "<value>"` / `no channel` / `no changes` | Edits the prepared request |

## When

| Step | Meaning |
|---|---|
| `"<customer>" books "<SERVICE>" for "<registration>" <day> at <HH:MM>` | `POST /appointments` |
| `... through "<CHANNEL>"` | Sends that `X-Channel` |
| `... at dealership "<CODE>"` | Uses that dealership |
| `... with:` + a two-column table | Sets `kind`, `waiting`, `courtesyCar`, `requestNotes` (`customerRequests.notes`) or `notes` |
| `the booking request is sent` | Sends the prepared request |
| `the appointment is fetched with its events` | `GET /appointments/{id}?include=events` |
| `the appointment is fetched through "<CHANNEL>"` | `GET /appointments/{id}` with that channel |
| `appointment "<uuid>" is fetched` | `GET /appointments/{uuid}` |
| `"<customer>" submits the same booking of "<SERVICE>" for "<registration>" <day> at <HH:MM> <N> times at the same time` | N identical requests released together |
| `customers "<first>" to "<last>" each book "<SERVICE>" for their own vehicle <day> at <HH:MM> at the same time` | One request per named customer, released together |
| `"<a>" and "<b>" each book "<SERVICE>" for their own vehicle <day> at <HH:MM> at the same time` | Two requests released together |
| `these customers each book "<SERVICE>" for their own vehicle <day> at the same time:` + table `customer`, `start` | One request per row, released together |

## Then

| Step | Asserts |
|---|---|
| `the response is <status>` | Status only |
| `the response is <status> "<CODE>"` | Status and problem `code` |
| `the response is <status> "<CODE>" naming the field "<field>"` | Also an `errors[]` entry for that field |
| `the response is 201 with an id that is a UUID` | Created; `id` parses as a UUID |
| `the response is 201 with the same appointment as the first booking` | Same `id` |
| `the Location header points at that appointment` | `Location` ends with `/appointments/{id}` |
| `the appointment status is "<STATUS>" at version <n>` | `status`, `version` |
| `the appointment runs from <HH:MM> to <HH:MM> <day>` | `startAt`, `endAt` as instants |
| `the appointment is for "<customer>" with vehicle "<reg>", service type "<CODE>" at dealership "<CODE>"` | Ids and codes in the record |
| `the appointment is on bay "<bay>"` / `... with technician "<tech>"` | `serviceBay.id`, `technician.id` |
| `the appointment's bay is of type "<A>" or "<B>"` | `serviceBay.type` |
| `the appointment's technician is one of "<a>", "<b>" or "<c>"` | `technician.id` |
| `the appointment owns <n> bay cells from <HH:MM> to <HH:MM>` / `technician cells` | `cells.bay` / `cells.technician`: `from`, `to`, `cells` |
| `the appointment records the kind "<KIND>" through "<CHANNEL>"` | `kind`, `channel` |
| `the appointment has customer requests "<text>"` (optionally `with a courtesy car`) | `customerRequests.notes` (and `courtesyCar`) |
| `the appointment has the notes "<text>"` | `notes` |
| `the record shows no ledger cells and no notes` / `the record shows no notes` | `cells` and `notes` absent |
| `the record shows the technician by id only` | `technician.displayName` absent |
| `the fetched record equals the booking response` | Same JSON apart from `events` |
| `the audit trail has exactly one "<TYPE>" event through "<CHANNEL>"` | `events[]` |
| `the response is 409 "SLOT_TAKEN" with between <a> and <b> alternatives` | Count of `alternatives` |
| `the alternatives include <HH:MM> and <HH:MM> <day>` / `no alternative starts at <HH:MM> <day>` / `every alternative ends <n> minutes after it starts` | `alternatives[]` |
| `the problem carries the first booking as the existing appointment with service type "<CODE>"` | `existing.id`, `existing.serviceTypeCode` |
| `the two appointments hold different bays and different technicians` | Last two appointments |
| `exactly <n> booking succeeded and <m> received 409 "<CODE>"` / `both bookings succeeded` / `all <n> bookings succeeded on bay "<bay>" with technician "<tech>"` | Race outcomes |
| `all <n> responses are 201 with the same appointment id` | Race outcomes |
| `the successful appointment is on bay ... / owns <n> ... cells from ... to ...` | The single winner of a race |
