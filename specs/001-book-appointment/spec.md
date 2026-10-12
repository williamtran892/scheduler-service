# Feature Specification: Book an appointment

**Feature Branch**: `001-book-appointment`

**Created**: 2026-10-12

**Status**: Draft

**Input**: User description: "Book an appointment: deliver `POST /appointments` (operationId `bookAppointment`) and `GET /appointments/{appointmentId}` (operationId `getAppointment`) end to end, per design 5.2, 5.3, 11.3, 11.4 "Book" and implementation notes 4.4, 4.5, 4.6. A client posts dealership, service type, customer, vehicle, start time, kind, customer requests and notes. The service generates the appointment id (also the customer-facing reference), resolves the service type into a requirement, computes the candidate (bay, technician) pairs free at that start, orders them least-loaded first with a seeded shuffle among ties, and claims one pair per transaction (appointment insert first as the idempotency guard, lock and verify the bay run and technician run, mark them booked, append the audit event and the outbox row, commit). Outcomes: 201 with the record and a Location header; the same booking again returns the existing appointment with 201; the same key with a different service type or dealership is 409 DUPLICATE_BOOKING; 409 SLOT_TAKEN with alternatives; 409 AVAILABILITY_CHANGED; 422 OFF_GRID_START, OUTSIDE_OPENING_HOURS, NO_QUALIFIED_TECHNICIAN, SERVICE_NOT_BOOKABLE_ONLINE; a deadlock victim is retried once on the same pair. The record includes the owned cell ranges for bay and technician."

**Sources**: `docs/design/SCHEDULING_DESIGN.md` ("design N.N"), `docs/design/IMPLEMENTATION_NOTES.md` ("notes N.N"; deviations D2 and D4 override the design wherever it mentions stored procedures or an idempotency table), `api/openapi.yaml` (cited by operationId), `docs/design/schema.sql`. This spec restates those documents as behaviour; it does not re-derive them.

## Clarifications

### Session 2026-10-12

Resolved without a human, from the design extract, the implementation notes, the OpenAPI contract, the schema and the decisions recorded for this release. Each answer is an assumption the reviewer can overturn.

- Q: How does the service know the calling channel (customer or staff) when this release has no authentication? → A: Assumption: every `/appointments` request carries the channel in an `X-Channel` header holding one of the contract's `Channel` values. It stands in for the bearer token's `channel` claim (contract `bearerAuth`) as implementation notes 4.0 foresee ("a header in v1"). Missing is 400 `VALIDATION_ERROR`, unknown is 422 `INVALID_VALUE`, both naming `X-Channel`. No token is validated; 401 and 403 are not produced in this release.
- Q: What does "outside opening hours" mean when the opening-hours tables are not part of this release? → A: Assumption: the ledger is the dealership's working time (design 4.2: "a cell that does not exist is time the resource does not work"; `docs/design/schema.sql` omits `opening_hours` and `opening_exception`). A booking is `OUTSIDE_OPENING_HOURS` when any grid cell of its bay or technician run, buffers included, has no ledger cell, in any status, for any bay or technician of the dealership. A start inside the booking horizon but beyond the fixture's 14 days of cells is therefore also `OUTSIDE_OPENING_HOURS`.
- Q: When is the booking key checked, so that a replay is never told its own cells are taken? → A: Assumption: after the request's references are validated and before any capacity rule (online flag, grid, horizon, opening hours, qualification, candidates), and once more before answering `SLOT_TAKEN` or `AVAILABILITY_CHANGED`. Implementation notes 4.4 check only on a unique-key violation, which would answer `SLOT_TAKEN` to a replay of a single-pair booking such as `MOT`; the extra lookups close that gap without replacing the unique key as the guard for concurrent submissions.
- Q: What is a candidate pair's "load", and where does the per-request shuffle seed come from? → A: Assumption: load is the number of BOOKED cells on the pair's bay plus on its technician on the requested local date (contract `CandidatePair.load`: "cells already booked on this pair that day"). The seed is derived from the appointment id the request generates; one id is generated per request and reused for each attempt, since a failed attempt leaves nothing behind. Unit tests fix the id and so the order; acceptance scenarios never assert which of two equally loaded pairs wins.
- Q: Where are `SLOT_TAKEN` and `AVAILABILITY_CHANGED` alternatives searched, and how are they ordered? → A: Assumption: feasible starts for the same service type at the same dealership on the requested local date and the next local date, later than now, excluding the requested start, ordered by distance from the requested start (earlier first on a tie), at most five (contract `Problem.alternatives`). An empty list is allowed when nothing is feasible in that window.

## User Scenarios & Testing *(mandatory)*

The acceptance scenarios below are the Gherkin scenarios in `features/booking.feature`, `features/claim_ledger.feature`, `features/idempotency.feature` and `features/concurrency.feature`, all tagged `@spec-001`. Each scenario is named here exactly as it appears in its feature file. They run black-box over HTTP against the containerised service and the fixture in `local/mysql/data.sql`; ledger state is asserted only through the public appointment record (its owned cell ranges) or through observable outcomes such as a second booking of the same time.

**Fixture the scenarios rely on** (dates are relative to the day the suite runs; times are Europe/London):

- Dealership `MAN01` (Europe/London, 15-minute grid, 28-day booking horizon) and dealership `LDS01` (same tenant, no resources; it exists only so a booking key can be submitted at a second dealership).
- Bays: `Bay 1` GENERAL, `Bay 2` MOT, `Bay 3` EV_HV.
- Technicians: `Sam` (GENERAL_SERVICE, EV_HV_L3), `Priya` (GENERAL_SERVICE, MOT_TESTER), `Lee` (GENERAL_SERVICE, and AIRCON_FGAS that expired yesterday).
- Service types: `INTERIM` 45 min on GENERAL or MOT bays; `MAJOR` 90 min with 15/15 buffers on GENERAL; `MOT` 45 min needing MOT_TESTER on MOT; `EV_MAJOR` 120 min with 15/15 buffers needing EV_HV_L3 on EV_HV; `EV_CHECK` 30 min needing EV_HV_L3 on GENERAL or EV_HV; `DIAGNOSTIC` 50 min on GENERAL, staff-only; `AIRCON` 30 min needing AIRCON_FGAS on GENERAL. Service types with no skill listed need GENERAL_SERVICE.
- Customers: `Alice` with vehicle `YK21 ABC` (Volkswagen ID.4), `Bob` with vehicle `BD70 XYZ` (Ford Focus), and `Customer 01` to `Customer 20`, each with one vehicle, for the race scenarios.
- FREE ledger cells for every bay and technician of `MAN01` for 14 days from the day the database starts, 07:00Z to 17:00Z on the 15-minute grid. That is 08:00 to 17:00 local in both summer and winter time, which is the window scenarios book in.

### User Story 1 - Book an appointment and read it back (Priority: P1)

An advisor (or a customer, or an integration) asks for a service at a dealership for a customer's vehicle at a chosen start time. The service picks a compatible bay and a qualified technician that are both free for the whole job, confirms the appointment in one step, and returns the full record, whose id is the reference quoted to the customer. Anyone holding the id can read the record back, including its audit trail (requirements R1 and R3, design 1.3, 5.2; `bookAppointment`, `getAppointment`).

**Why this priority**: it is the core of the product. With only this story a dealership can take bookings that are correct for a single caller.

**Independent Test**: book one appointment on an empty day through `bookAppointment` and read it through `getAppointment`; the record must name the bay, the technician, the exact times and the owned cell ranges.

**Acceptance Scenarios** (`features/booking.feature`):

1. **Booking an appointment confirms it with the full record**: **Given** the fixture, **When** Alice books `EV_MAJOR` for `YK21 ABC` tomorrow at 09:00 as a drop-off through the advisor console with customer requests and notes, **Then** the response is 201 with a server-generated UUID id and a `Location` header for it, status `CONFIRMED`, version 1, times 09:00 to 11:00, bay `Bay 3`, technician `Sam`, and 10 bay cells and 10 technician cells from 08:45 to 11:15.
2. **The confirmed appointment can be read back with its audit trail**: **Given** Alice has booked `INTERIM`, **When** the appointment is fetched with its events, **Then** the record equals the booking response and the audit trail holds one `CONFIRMED` event through the booking channel.
3. **A customer channel sees its booking without ledger cells, internal notes or technician name**: **Given** the fixture, **When** Bob books `INTERIM` through `CUSTOMER_WEB` with internal notes, **Then** the response is 201, the record has no ledger cells, no notes and a technician id without a name, and an advisor fetching the same appointment sees its cells but no notes, because customer-channel notes are ignored.
4. **The least-loaded qualified bay and technician get the job**: **Given** on one day Sam holds an `EV_MAJOR` and Priya holds an `MOT`, **When** Alice books `INTERIM` later that day, **Then** the appointment is on `Bay 1` with `Lee`.
5. **An unknown appointment is not found**: **When** an appointment id that does not exist is fetched, **Then** the response is 404 `NOT_FOUND`.

---

### User Story 2 - Capacity is never double-booked (Priority: P2)

Two requests can never both hold the same bay or the same technician for overlapping time, however they race, and no booking is confirmed unless one bay of a compatible type and one technician with every required skill are both free for the whole job including buffers. When the time cannot be had the caller is told so and offered nearby alternatives (requirement R2, design 1.3, 4.2, 5.2, 5.3, 11.3; notes 4.4, 4.5).

**Why this priority**: a false confirmation is the one failure the business cannot tolerate. Story 1 alone is correct only without contention.

**Independent Test**: book the same time twice on a service that has only one qualified pair, and race twenty clients for it; exactly one confirmation exists and every other caller is told the slot is taken.

**Acceptance Scenarios** (`features/claim_ledger.feature`, `features/concurrency.feature`, and one in `features/booking.feature`):

1. **A booking claims a run of cells on one bay and one technician**: an `INTERIM` at 09:00 owns 3 bay cells and 3 technician cells from 09:00 to 09:45, on a GENERAL or MOT bay with a GENERAL_SERVICE technician.
2. **Buffers are reserved on the ledger but not promised to the customer**: a `MAJOR` at 11:00 runs 11:00 to 12:30 for the customer and owns 8 cells from 10:45 to 12:45 on `Bay 1` and on its technician.
3. **A job that is not a whole number of cells is rounded up on the ledger only**: a 50-minute `DIAGNOSTIC` at 14:00 ends 14:50 for the customer and owns 4 cells from 14:00 to 15:00.
4. **Back-to-back bookings on the same pair are both confirmed**: `MOT` at 09:00 and `MOT` at 09:45 both succeed on `Bay 2` with `Priya`.
5. **Overlapping windows on the only qualified pair conflict**: `MOT` at 12:00 succeeds; `MOT` at 12:30 is 409 `SLOT_TAKEN`.
6. **A busy bay blocks the booking even when qualified technicians are free**: a second `MAJOR` at the same time as the first is 409 `SLOT_TAKEN` (one GENERAL bay, three qualified technicians).
7. **A free bay is not enough when the only qualified technician is busy**: after `EV_MAJOR` takes Sam, an `EV_CHECK` overlapping it is 409 `SLOT_TAKEN` although `Bay 1` is free.
8. **The same window on a different pair is confirmed**: two `INTERIM` bookings at the same time both succeed, on different bays and different technicians.
9. **When every qualified pair is taken the caller gets alternatives** (`booking.feature`): a second `MOT` at 15:00 is 409 `SLOT_TAKEN` with at most five alternatives, including 14:15 and 15:45, none at 15:00, each lasting 45 minutes.
10. **Twenty customers race for the only MOT pair** (`concurrency.feature`, `@slow`): twenty simultaneous `MOT` bookings at one time give exactly 1 confirmation and 19 `SLOT_TAKEN`; the winner owns 3 cells on `Bay 2` and 3 on `Priya`.
11. **Two customers, one slot, two pairs** (`concurrency.feature`, design 5.3): Alice and Bob book `INTERIM` at the same time simultaneously; both succeed on different bays and different technicians.
12. **Simultaneous bookings of different hours on the same pair do not interfere** (`concurrency.feature`, `@slow`): eight simultaneous back-to-back `MOT` bookings on one day all succeed.

---

### User Story 3 - Repeating a booking is safe (Priority: P3)

A client that loses a response, or a customer who presses confirm twice, can resubmit the same booking and gets the one appointment that exists, never a second one. The booking key is the vehicle, the customer, the start time and the kind; no idempotency header is needed (design 4.1.4, 5.2, 7, 11.1; notes 0 D4, 4.4; `bookAppointment` "Safe to repeat").

**Why this priority**: duplicates are a real operational nuisance, but stories 1 and 2 already prevent double-booking of capacity.

**Independent Test**: submit the same booking twice, then ten times at once; every response is 201 with the same id.

**Acceptance Scenarios** (`features/idempotency.feature`):

1. **Submitting the same booking again returns the same appointment**: the second identical submission is 201 with the same id, version 1 and the same `Location`.
2. **Resubmitting a booking whose only pair it already holds returns it**: an `MOT` (one qualified pair) submitted twice returns the same appointment, not `SLOT_TAKEN`.
3. **Resubmitting with different customer requests returns the original unchanged**: the second submission is 201 with the same id and the original customer requests.
4. **The same booking key with a different service type is a duplicate booking**: 409 `DUPLICATE_BOOKING` carrying the existing appointment's id, status and service type code.
5. **The same booking key at another dealership is a duplicate booking**: submitting at `LDS01` is 409 `DUPLICATE_BOOKING` carrying the `MAN01` appointment.
6. **A request that failed leaves nothing behind, so its retry runs again**: a `DIAGNOSTIC` refused on `CUSTOMER_WEB` with 422 `SERVICE_NOT_BOOKABLE_ONLINE` is then booked with the same key through the advisor console with 201.
7. **Simultaneous identical submissions create one appointment** (`@slow`): ten identical submissions at once all return 201 with one id.

---

### User Story 4 - Requests that cannot be booked are refused with a clear reason (Priority: P4)

A request that can never succeed as stated is refused before any capacity is touched, with a stable machine-readable code the client can act on (design 6 error contract, 11.3; notes 4.6; `bookAppointment` 400 and 422 responses).

**Why this priority**: clients can live with generic failures for a while; correctness of what is confirmed matters more.

**Independent Test**: send each malformed or unbookable request and check the status and problem code.

**Acceptance Scenarios** (`features/booking.feature`):

1. **A start off the booking grid is rejected**: 09:05 is 422 `OFF_GRID_START`.
2. **A booking outside opening hours is rejected** (outline): `INTERIM` at 06:00 or 19:00, and a `MAJOR` at 17:00 whose job and buffer run past closing, are 422 `OUTSIDE_OPENING_HOURS`.
3. **A service no technician is currently qualified for is rejected**: `AIRCON`, whose only holder's certification expired, is 422 `NO_QUALIFIED_TECHNICIAN`.
4. **A staff-only service cannot be booked from a customer channel** (outline): `DIAGNOSTIC` through `CUSTOMER_WEB` or `CUSTOMER_APP` is 422 `SERVICE_NOT_BOOKABLE_ONLINE`.
5. **A malformed or inconsistent booking request is rejected** (outline): a missing service type, a start without an offset or a missing channel is 400 `VALIDATION_ERROR`; an unknown kind, an unknown service type, a vehicle that belongs to another customer, a start in the past or a start beyond the booking horizon is 422 `INVALID_VALUE`; each names the offending field.

---

### Edge Cases

- **Replay while the slot is held by the replayed booking itself**: the service checks the booking key before looking for free capacity and again before answering `SLOT_TAKEN` or `AVAILABILITY_CHANGED`, so a replay always finds its own appointment (scenario "Resubmitting a booking whose only pair it already holds returns it").
- **Concurrent identical submissions**: the second insert waits on the booking-key unique index until the first commits (then it returns the existing appointment) or rolls back (then it proceeds) (design 5.2).
- **Every candidate pair lost to a race**: the loser moves to the next pair; when none is left it gets 409 `SLOT_TAKEN` with freshly computed alternatives (design 5.3).
- **Cells vanish between candidate selection and claim** (a resource retired, cells not yet created): the service recomputes candidates once and retries; if cells are still missing it answers 409 `AVAILABILITY_CHANGED` with alternatives (design 5.2, 11.3; notes 4.4). Nothing in this release removes cells, so this is proven by unit tests, not by a Gherkin scenario.
- **Deadlock victim** (MySQL 1213; design 11.3 describes it for a reschedule racing a booking, and concurrent inserts on one booking key can produce it too): the same pair is retried once; a second loss on that pair is treated as the pair being taken. Proven by unit tests.
- **Buffers before opening or after closing**: the bay and technician runs include buffers, so a job whose buffer falls outside working time is refused with `OUTSIDE_OPENING_HOURS` even if the customer-facing times are inside (design 4.2).
- **A rebooking of the same minute after a cancellation** is 409 `DUPLICATE_BOOKING` (the key covers every status, design 4.1.4). Cancel is feature 002, so this edge is specified here and proven there.
- **Time zone changes**: instants are stored in UTC; local dates (for load and skill validity) use the dealership time zone, so a booking on a daylight-saving change day uses that day's offset (design 7).

## Requirements *(mandatory)*

### Functional Requirements

**Booking request and validation** (`bookAppointment`)

- **FR-001**: The system MUST accept a booking with dealership, service type, customer, vehicle, start time (ISO 8601 with offset) and kind, plus optional customer requests and notes, in the shape of `BookAppointmentRequest`.
- **FR-002**: The system MUST reject a request that cannot be parsed, lacks a required field, has a start time without an offset or lacks the calling channel with 400 `VALIDATION_ERROR`, naming each offending field.
- **FR-003**: The system MUST reject with 422 `INVALID_VALUE`, naming the field, an unknown enumeration value (kind, channel), a dealership, service type, customer or vehicle that does not exist, is inactive or belongs to another tenant, a vehicle that does not belong to the customer, a start time in the past or beyond the dealership's booking horizon, and text longer than the contract allows.
- **FR-004**: The system MUST reject a staff-only service type requested through a customer channel (`CUSTOMER_WEB`, `CUSTOMER_APP`) with 422 `SERVICE_NOT_BOOKABLE_ONLINE` (design 6).
- **FR-005**: The system MUST reject a start time that is not on the dealership's grid (a whole multiple of `slot_minutes` from the top of the UTC hour, zero seconds) with 422 `OFF_GRID_START`, before touching capacity (design 11.1, notes 4.6).
- **FR-006**: The system MUST reject with 422 `OUTSIDE_OPENING_HOURS` a booking whose bay run or technician run, buffers included, covers a grid cell for which the dealership has no ledger cell at all (for any bay or technician, in any status); in this release the ledger is the dealership's working time.
- **FR-007**: The system MUST reject with 422 `NO_QUALIFIED_TECHNICIAN` a booking when no ACTIVE technician at the dealership holds every required skill valid on the appointment's local date (design 4.2, 11.4).

**Requirement and candidate selection** (design 5.1, 5.2; notes 4.2, 4.3)

- **FR-008**: The system MUST resolve the service type into a requirement: duration, buffer before, buffer after, required skills (all of them), allowed bay types (any one of them) and whether customers may book it online.
- **FR-009**: The system MUST compute the bay run and the technician run as consecutive grid cells starting at `start - buffer before` and covering `buffer before + duration + buffer after`, rounded up to whole cells; in this release both runs are identical (design 5.1, notes 4.2).
- **FR-010**: The system MUST select as candidates only pairs of one ACTIVE bay of an allowed type and one ACTIVE qualified technician where each resource individually has a free run containing its required cells; a run split across two bays or two technicians is not a candidate (design 5.1).
- **FR-011**: The system MUST order candidates least-loaded first, where a pair's load is the number of cells already booked on its bay plus on its technician on the requested local date, and MUST shuffle pairs of equal load with a seed derived from the request's generated appointment id (design 5.2, 5.3).
- **FR-012**: Clients MUST NOT be able to choose the bay or technician when booking; the request has no such fields (design 5.1).

**The booking transaction** (design 5.2, 11.4 "Book"; notes 0 D2, D4, 4.4, 4.5)

- **FR-013**: The system MUST generate the appointment id; it is the customer-facing reference and no other reference number exists.
- **FR-014**: For each candidate pair in order, the system MUST make one all-or-nothing attempt: record the appointment first (its booking key is the idempotency guard), then lock the bay run and then the technician run, verify that every cell of each run exists and is free, mark them booked by this appointment, append a `CONFIRMED` audit event and an `AppointmentConfirmed` outbox event, and commit. Bay before technician, always.
- **FR-015**: If a cell of the pair is already booked, the system MUST undo the attempt and try the next pair; when no pair is left it MUST answer 409 `SLOT_TAKEN` with alternatives.
- **FR-016**: If cells of the pair do not exist, the system MUST undo the attempt, recompute candidates once and start again; if cells are missing again it MUST answer 409 `AVAILABILITY_CHANGED` with alternatives.
- **FR-017**: If the attempt is chosen as a deadlock victim, the system MUST retry the same pair once.
- **FR-018**: A `201` MUST mean the appointment, its cells, its audit event and its outbox event are all committed; no other outcome may leave any of them behind.

**Idempotency** (design 4.1.4, 5.2, 7, 11.1; notes 0 D4, 4.4; `bookAppointment`)

- **FR-019**: The booking key MUST be vehicle, customer, start time and kind; at most one appointment may ever exist per key, in any status.
- **FR-020**: When an appointment already owns the key, has the same dealership and service type, and is neither cancelled nor a no-show, the system MUST return that appointment unchanged with 201 and the same `Location`.
- **FR-021**: When an appointment already owns the key but its dealership or service type differs, or it is cancelled or a no-show, the system MUST answer 409 `DUPLICATE_BOOKING` carrying the existing appointment's id, status, start, end and service type code.
- **FR-022**: The system MUST look the booking key up after validating the request's references and before applying any capacity rule, and again before answering `SLOT_TAKEN` or `AVAILABILITY_CHANGED`, so a replay is never told its own cells are taken; the unique key on the appointment remains the final guard for concurrent submissions.
- **FR-023**: A request that failed MUST leave nothing behind, so its retry is processed afresh.

**Outcomes and the record** (`bookAppointment`, `getAppointment`; design Appendix B)

- **FR-024**: On success the system MUST answer 201 with the `Appointment` record and a `Location` header pointing at the appointment.
- **FR-025**: The record MUST include id, status `CONFIRMED`, kind, exact start and end (end = start + duration, no buffers), dealership, customer, vehicle, service type, bay, technician, customer requests, channel, version 1, creation and update times, and, for staff channels, the internal notes and the owned cell ranges for the bay and the technician as half-open intervals with their cell counts.
- **FR-026**: For customer channels the record MUST omit the cell ranges, the internal notes and the technician's name (design open question 1, default hidden); internal notes sent through a customer channel MUST be ignored, not stored.
- **FR-027**: `SLOT_TAKEN` and `AVAILABILITY_CHANGED` MUST carry up to five alternatives, each a start and end feasible for the same service type at the same dealership, freshly computed over the requested local date and the next one, later than now, never the requested start itself, nearest to the requested start first and earlier first on a tie.
- **FR-028**: Every failure MUST be an RFC 9457 problem with the stable `code` from the contract's `ProblemCode` list.
- **FR-029**: `getAppointment` MUST return the same record shape, with the audit trail when `include=events` is given, and 404 `NOT_FOUND` for an id that does not exist.
- **FR-030**: Every `/appointments` request MUST state its calling channel in the `X-Channel` header, a stand-in for the token's channel claim until authentication exists; a missing channel is 400 `VALIDATION_ERROR` and an unknown one 422 `INVALID_VALUE`. The channel is recorded on the appointment and its audit event and decides the customer or staff view (staff: `ADVISOR_CONSOLE`, `CALL_CENTRE`, `INTEGRATION`; customer: `CUSTOMER_WEB`, `CUSTOMER_APP`).

**Data and fixture** (decisions 2 and 5)

- **FR-031**: The 15 tables this feature uses MUST be added to the database schema exactly as defined in `docs/design/schema.sql`; no stored routines, triggers or ENUM columns.
- **FR-032**: The fixture of setup rows, customers, vehicles and FREE ledger cells MUST be loaded at database start with fixed ids, with cells generated relative to the current date so it never goes stale.

### Out of scope

From decision 1 and design 1.4. None of these is delivered by this feature:

- Public availability search (`GET /dealerships/{id}/availability`); candidate selection is internal to booking.
- Listing appointments (`listAppointments`).
- Lifecycle: reschedule, reassign, cancel, check-in, start, complete, no-show, the no-show sweeper and late arrival (feature 002).
- Publishing outbox events (the relay), confirmation notifications and DMS sync; this feature only writes the outbox row.
- Setup CRUD for dealerships, opening hours, bays, technicians, skills and service types; the materialiser and rotas; changing the grid size.
- Time-boxed holds; technician absences; capacity blocks; channel reservations; per-service daily caps; reallocation and at-risk flags; overrun detection.
- Waiter versus drop-off rules (`kind` is recorded, not enforced); multi-service appointments and per-model duration overrides; courtesy cars and loaners as resources (free text only); multi-dealership search; an availability cache; reminders; planning board and utilisation reporting; OEM recall, parts pre-pick and duration learning.
- Authentication and authorisation (bearer tokens, roles, "customers may only book for themselves", tenant from the token).

### Key Entities *(include if feature involves data)*

- **Appointment**: one confirmed booking: who (customer, vehicle), where (dealership, bay, technician), what (service type, kind, customer requests, notes), when (exact start and end), status, channel and version. Its id is the reference. Unique per booking key.
- **Ledger cell**: one grid cell of one bay or one technician; FREE or BOOKED by exactly one appointment. A missing cell is time the resource does not work.
- **Service requirement**: what a service type needs: duration, buffers, required skills, allowed bay types, whether customers may book it online.
- **Candidate pair**: a bay and a technician that can both take the runs at the requested start, with its load for the day.
- **Audit event**: an append-only record of a transition with actor and channel.
- **Outbox event**: the `AppointmentConfirmed` message committed with the booking, for later publication.
- **Dealership, service bay, technician, skill, service type, customer, vehicle**: read-only fixture data in this release.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: When twenty requests race for a time that only one booking can hold, exactly 1 is confirmed and 19 are told the slot is taken; 0 false confirmations, in every run.
- **SC-002**: Submitting the same booking any number of times, one after another or up to ten at once, yields exactly one appointment reference.
- **SC-003**: 100% of refused requests carry a status and a code from the contract's problem list; every "slot taken" answer offers at least one alternative when a feasible start exists nearby.
- **SC-004**: For 100% of confirmed appointments, the record's owned cell ranges equal the customer-facing window widened by the buffers and rounded up to whole cells, on one bay of an allowed type and one qualified technician.
- **SC-005**: 95% of booking requests are answered within 200 ms at 50 requests per second on a single node (design 7 performance target).
- **SC-006**: All `@spec-001` acceptance scenarios pass in one run of the acceptance suite against a freshly started service and database.

## Assumptions

- The fixture described above is the only data in this release; nothing creates dealerships, resources, service types, customers or cells at runtime (decisions 1 and 2). The fixture refines decision 2's suggestion: `LDS01`, `EV_CHECK`, `DIAGNOSTIC`, `AIRCON`, the AIRCON_FGAS skill, `EV_MAJOR` buffers of 15/15 (as in design Appendix B), `INTERIM` on GENERAL or MOT bays, and twenty race customers are additions so every outcome above can be reached black-box.
- Scenarios book on days 1 to 8 after the day the suite runs, in disjoint time windows per scenario, because the database is not reset between scenarios; days 9 to 12 are left for feature 002.
- `kind` is recorded but not enforced; waiter and drop-off use identical runs in this release (design 1.4, 4.2).
- The tenant of a booking is the tenant of its dealership; every referenced row must share it.
- The appointment's audit actor is recorded as `anonymous` until authentication exists.
- A start in the past or beyond the dealership's booking horizon is a range violation, 422 `INVALID_VALUE` on `startAt` (contract `ProblemCode`: "Enumeration or range violated"); the contract names no dedicated code.
- "The same request" for a replay compares dealership and service type only (contract: "the same key with a different service type or dealership is 409"); differing customer requests or notes return the existing appointment unchanged.
- A service type for which the dealership has no ACTIVE bay of an allowed type has no candidates and is answered 409 `SLOT_TAKEN` with no alternatives; the contract has no code for it and the fixture has no such case.
- Outbox rows are written but not published in this release; their presence is proven by unit tests on the booking transaction.
