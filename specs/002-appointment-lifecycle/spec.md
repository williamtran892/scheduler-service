# Feature Specification: Appointment lifecycle

**Feature Branch**: `002-appointment-lifecycle`

**Created**: 2026-10-13

**Status**: Draft

**Input**: User description: "Appointment lifecycle: deliver every transition after booking per design 4.3 and the contract operations rescheduleAppointment (also reassign), cancelAppointment, checkInAppointment (including the NO_SHOW late arrival), startAppointment, completeAppointment and markNoShow, plus the no-show sweeper worker. Each transition is one transaction that honours expectedVersion, changes the ledger as design 4.3 says, appends an audit event and an outbox row and returns the Appointment record."

**Sources**: `docs/design/SCHEDULING_DESIGN.md` ("design N.N"), `docs/design/IMPLEMENTATION_NOTES.md` ("notes N.N"; deviations D2 and D4 override the design wherever it mentions stored procedures or an idempotency table), `api/openapi.yaml` (cited by operationId), `docs/design/schema.sql`. The implemented system is described in `docs/architecture.md` and `docs/development.md`. This spec restates those documents as behaviour; it does not re-derive them.

## Clarifications

### Session 2026-10-13

Resolved without a human, from the design extract, the implementation notes, the OpenAPI contract, the schema, the implemented feature 001 and the decisions recorded for this feature. Each answer is an assumption the reviewer can overturn.

- Q: Who may perform which transition, when this release has no authentication and the caller only states its channel in `X-Channel`? → A: Assumption: the contract marks check-in, the reschedule pins (`technicianId`, `serviceBayId`) and, through design 6, start, complete and no-show as staff actions, and lists `403` on every lifecycle operation. `X-Channel` already stands in for the token's `channel` claim, so a customer channel (`CUSTOMER_WEB`, `CUSTOMER_APP`) asking for a staff action is refused with 403 `FORBIDDEN` (FR-008). Cancel and a reschedule in time only are open to every channel (design 6: "owner, staff"); ownership is not checked because there is no customer identity. `401` is still never produced. This changes feature 001's release-level statement that `403` is never returned.
- Q: How are the time-dependent behaviours (the sweeper, the late arrival, completing after the booked time) accepted, when the acceptance suite runs against the real clock with no fake clock or test endpoint? → A: Assumption: scenarios tagged `@clock` may use one step that moves an appointment's start and end, and the start of every cell it owns, back by 14 days in one database transaction; the suite reads the database address from `SCHEDULER_TEST_DSN`, which `scripts/integration.sh` sets, and never reads the database to assert. 14 days puts any time in days 9 to 12 before the fixture's first cell (the London date is at most one day ahead of the database's `CURRENT_DATE`), so moved cells never collide with existing cells, and the calendar's disjoint windows stay disjoint. The sweeper's interval is configuration (default one minute, about one second in the compose configuration the suite runs against) and the grace period is configuration (default 30 minutes); `@clock` scenarios poll `getAppointment` until a 10-second deadline. Backdating cannot help a late arrival, which claims cells at the current time: the fixture's `MAN01` cells cover only 07:00Z to 17:00Z, so its outcome would depend on the hour the suite runs. `LDS01` therefore gets one bay and one technician with cells around the clock, which no feature 001 scenario can reach. The exact rules (the grace boundary, the current grid cell, the past/future split on complete) are unit tests with `platform.FakeClock`.
- Q: Where does a late arrival's job start, on which pair, and what does "nothing is free" cover? → A: Assumption: the contract says the cells are re-claimed "for the job starting at the current grid cell", so the new start is the start of the cell that contains now, and the bay and technician runs follow from the service type exactly as in booking. The previous pair is tried first, then every qualified pair least-loaded first, as booking chooses. The booking horizon and the "start after now" rule do not apply. Every way of finding no pair (cells taken, cells missing, nobody qualified on the day) is 409 `SLOT_TAKEN` (contract `checkInAppointment`), with alternatives computed as booking computes them from the current grid cell, and the appointment stays `NO_SHOW` at its version. Design 4.3 and the contract agree on this code; the response description of `TransitionConflict` lists it among the 409s.
- Q: When a request is both stale and not allowed from the current status, which answer wins, and how is a concurrent change detected? → A: Assumption: the request is decoded, then the channel is checked, then the appointment is loaded (404), then a cancel of a `CANCELLED` appointment is answered with the record unchanged whatever `expectedVersion` it carries (contract: "returns the record unchanged", which a retry with the original version must also get), then `expectedVersion` is compared with the stored version (409 `VERSION_CONFLICT`, the detail stating both), then the transition table (409 `INVALID_TRANSITION`), then the operation's own rules (FR-003). A stale caller's idea of the status is unreliable, so it is told to refetch first. The version is checked again by the write itself (notes 4.6: zero rows on the version-guarded update is `ErrStaleVersion`), so a change that lands between the read and the write is also `VERSION_CONFLICT`. This covers the sweeper racing staff.
- Q: How does a reschedule choose the pair, and which booking rules apply to the new start? → A: Assumption: the new start is `startAt`, or the current start for a pure reassign, and goes through booking's checks in booking's order (staff-only service through a customer channel, grid, after now, horizon, working time, a qualified technician exists). Named resources are validated against the service type like booking validates candidates (FR-012; decision 1). Candidates come from the day's free runs with the appointment's own cells counting as free and are restricted to the named resources; the current pair is tried first, so a move keeps its bay and technician when they can take it, then least-loaded first with booking's seeded shuffle. A move whose new start is the booking key of another appointment is 409 `DUPLICATE_BOOKING` carrying it, because the booking key is a unique index over every status (design 7, 11.1). `SLOT_TAKEN` alternatives on a reschedule are computed as for booking, with the appointment's own cells free and the named resources respected.

## User Scenarios & Testing *(mandatory)*

The acceptance scenarios below are the Gherkin scenarios in `features/lifecycle.feature`, `features/reschedule.feature` and `features/noshow.feature`, all tagged `@spec-002`. Each scenario is named here exactly as it appears in its feature file. They run over HTTP against the containerised service and the fixture in `local/mysql/data.sql`, like feature 001's. Ledger state is asserted only through the public appointment record (its owned cell ranges) or through observable outcomes such as a second booking of the freed time.

Time-dependent behaviour (the sweeper, the late arrival, completing after the booked time) runs against the real clock. Scenarios tagged `@clock` may move one appointment and the cells it owns into the past through a direct database connection, which is the only database write the suite makes and is never used to assert anything; the outcome is then read by polling the public record until a deadline. The exact time rules (the grace boundary, the current grid cell, the split between past and future cells on complete) are proven by unit tests with a controllable clock.

**Fixture the scenarios rely on** (dates are relative to the day the suite runs; times are Europe/London). Everything feature 001 relies on is unchanged: dealership `MAN01` with `Bay 1` GENERAL, `Bay 2` MOT, `Bay 3` EV_HV; technicians `Sam` (GENERAL_SERVICE, EV_HV_L3), `Priya` (GENERAL_SERVICE, MOT_TESTER), `Lee` (GENERAL_SERVICE); service types `INTERIM` (45 min, GENERAL or MOT bay), `MOT` (45 min, so only `Bay 2` with `Priya`), `EV_CHECK` (30 min, EV_HV_L3, GENERAL or EV_HV bay); customers `Alice` (`YK21 ABC`) and `Bob` (`BD70 XYZ`); FREE cells 08:00 to 17:00 local for 14 days. This feature adds rows for dealership `LDS01` only:

- bay `Bay L1` (GENERAL, ACTIVE) and technician `Jo` (ACTIVE, GENERAL_SERVICE valid from a year ago);
- FREE ledger cells for both on every 15-minute cell of the day, around the clock, for the same 14 days.

They exist because a late arrival claims cells starting at the current grid cell, so its outcome would otherwise depend on the hour the suite runs (Clarifications). `LDS01` is a separate dealership, so no feature 001 scenario can see these resources: the one feature 001 scenario that names `LDS01` is answered from the booking key before any capacity is read.

**Calendar** (feature 001 uses `MAN01` days 1 to 8, which this feature does not touch): `lifecycle.feature` books `MAN01` in 9 and 10 days' time; `reschedule.feature` books `MAN01` in 11 and 12 days' time; `noshow.feature` books `LDS01` in 9, 10 and 11 days' time, and its late arrival takes `LDS01`'s cells at the current time.

### User Story 1 - Run the day: check in, start, complete (Priority: P1)

On the day, an advisor checks the customer in, the technician starts the job and later completes it. Each step is sent with the version the caller last saw, so two people acting on the same appointment cannot overwrite each other silently. Starting records when work actually began; completing records when it ended and gives the cells from the current time onwards back to the ledger, so an early finish can be sold to a walk-in, while the cells already used stay with the job for the record (design 4.3, 5.6, 11.4 "Complete"; `checkInAppointment`, `startAppointment`, `completeAppointment`).

**Why this priority**: it is what the workshop does with every appointment it books; without it the record never leaves `CONFIRMED` and capacity freed by early finishes is lost.

**Independent Test**: book one appointment, check it in, start it, complete it; the record moves `CONFIRMED` → `CHECKED_IN` → `IN_PROGRESS` → `COMPLETED` with the version rising by one each time, the actual start and end times appear, and the cells are released.

**Acceptance Scenarios** (`features/lifecycle.feature`):

1. **Checking in and starting a job keeps its cells and records the actual start**: **Given** Alice has booked `INTERIM` at 09:00, **When** the appointment is checked in, **Then** it is `CHECKED_IN` at version 2 and still owns 3 bay and 3 technician cells from 09:00 to 09:45; **When** it is started, **Then** it is `IN_PROGRESS` at version 3 with an actual start time and no actual end time, still owning its cells, and the audit trail holds one `CHECKED_IN` and one `STARTED` event through the advisor console.
2. **A start time sent by the dealer system is recorded as the actual start**: **Given** a checked-in appointment, **When** it is started with a start time of 16:00 yesterday, **Then** it is `IN_PROGRESS` at version 3 and its actual start is 16:00 yesterday.
3. **Completing a job before its booked time frees all its cells**: **Given** Alice's `MOT` has been checked in and started, **When** it is completed, **Then** it is `COMPLETED` at version 4 with actual start and end times and owns no cells, and Bob can book the same `MOT` time on `Bay 2` with `Priya`.

---

### User Story 2 - Cancel and mark no-shows (Priority: P1)

A customer or an advisor cancels an appointment that is confirmed, or checked in but not yet started (the customer left before work began); staff mark a confirmed appointment as a no-show. Either way every cell the appointment owns returns to the ledger at once and can be booked by someone else. Cancelling again returns the record unchanged, so a retried cancel is harmless (design 4.3, 11.4 "Cancel / no-show"; `cancelAppointment`, `markNoShow`).

**Why this priority**: capacity held by appointments that will not happen is lost revenue, and cancellation is the most common change customers make.

**Independent Test**: book the only `MOT` pair, cancel it, and book the same time for another customer; the second booking succeeds on the same pair.

**Acceptance Scenarios** (`features/lifecycle.feature`):

1. **Cancelling a confirmed appointment frees its cells for another booking**: **Given** Alice has booked `MOT` at 13:00, **When** it is cancelled, **Then** it is `CANCELLED` at version 2 with no cells and one `CANCELLED` event, and Bob books `MOT` at 13:00 on `Bay 2` with `Priya`.
2. **Cancelling a checked-in appointment frees its cells**: **Given** Alice's `MOT` at 14:00 has been checked in, **When** it is cancelled, **Then** it is `CANCELLED` at version 3 with no cells, and Bob books `MOT` at 14:00.
3. **Cancelling an appointment that is already cancelled returns it unchanged**: **Given** Alice's appointment was cancelled through `CUSTOMER_WEB`, **When** the same cancel is sent again with the version it originally carried, **Then** the response is 200 with the appointment `CANCELLED` at version 2, and the audit trail holds exactly one `CANCELLED` event, through `CUSTOMER_WEB`.
4. **A cancelled appointment's booking key cannot be booked again**: **Given** Alice's `INTERIM` at 16:00 has been cancelled, **When** Alice books the same service, vehicle and time again, **Then** the response is 409 `DUPLICATE_BOOKING` carrying the cancelled appointment.
5. **Staff mark a confirmed appointment as a no-show, freeing its cells**: **Given** Alice has booked `MOT`, **When** staff mark it a no-show, **Then** it is `NO_SHOW` at version 2 with no cells and one `NO_SHOW` event through the advisor console, and Bob books the same `MOT` time.

---

### User Story 3 - Move an appointment or give it to another bay or technician (Priority: P2)

Staff move a confirmed appointment to another time, and optionally choose the bay, the technician or both; a customer may move their appointment in time only. The move is all or nothing: the new cells are claimed with the appointment's own cells counting as free (so a move of 30 minutes that overlaps itself works), then every cell outside the new runs is released, including the previous technician's when only the technician changes. If the new time cannot be had the customer keeps the original booking and is offered alternatives. Every rule that applies to a booking applies to the new start (design 4.3, 5.4, 11.3, 11.4 "Reschedule and reassign"; notes 4.4, 4.5; `rescheduleAppointment`).

**Why this priority**: moving appointments is the second most common change, and reassigning is how the controller reacts when a technician drops out (design 1.4: absences are handled by hand through the normal flow).

**Independent Test**: book `MOT` at 09:00, move it to 10:00, then book `MOT` at 09:00 for another customer; both succeed on the only `MOT` pair.

**Acceptance Scenarios** (`features/reschedule.feature`):

1. **Moving an appointment to a free time claims the new cells and frees the old ones**: **Given** Alice has booked `MOT` at 09:00, **When** it is rescheduled to 10:00, **Then** it is `CONFIRMED` at version 2, runs 10:00 to 10:45 on `Bay 2` with `Priya`, owns 3 bay and 3 technician cells from 10:00, has one `RESCHEDULED` event, and Bob books `MOT` at 09:00.
2. **A move that overlaps the appointment's own cells is claimed**: **Given** Alice has booked `MOT` at 11:00, **When** it is rescheduled to 11:30, **Then** it owns the cells from 11:30 to 12:15 on the same pair, and Bob books `MOT` at 10:45, which needs the 11:00 and 11:15 cells Alice gave back.
3. **A move onto the start of another of the customer's bookings is a duplicate booking**: **Given** Alice's `INTERIM` at 12:30 was cancelled and she has another at 13:15, **When** the second is rescheduled to 12:30, **Then** the response is 409 `DUPLICATE_BOOKING` carrying the cancelled appointment, and the second is unchanged.
4. **Reassigning the technician at the same time frees the previous technician**: **Given** Alice's `INTERIM` at 14:00 has been reassigned to `Bay 1` with `Priya`, **When** it is reassigned to technician `Lee`, **Then** it is `CONFIRMED` at version 3, still 14:00 to 14:45, on `Bay 1` with `Lee`, and Bob books `MOT` at 14:00 on `Bay 2` with `Priya`.
5. **A move to a time with no free pair is refused and the original booking is kept**: **Given** Bob holds `MOT` at 16:00 and Alice `MOT` at 15:00, **When** Alice's is rescheduled to 16:00, **Then** the response is 409 `SLOT_TAKEN` with 1 to 5 alternatives, and Alice's appointment is still at version 1, 15:00 to 15:45, owning its cells.
6. **Customers cannot choose the bay or technician when moving an appointment**: a reassignment sent through `CUSTOMER_APP` is 403 `FORBIDDEN` and changes nothing.
7. **Reassigning to a technician without the required skill is refused**: moving an `EV_CHECK` to `Lee` is 422 `NO_QUALIFIED_TECHNICIAN` and changes nothing.
8. **Reassigning to a bay of a type the service cannot use is refused**: moving an `MOT` to `Bay 1` is 422 `INVALID_VALUE` naming `serviceBayId`; the appointment stays on `Bay 2` with `Priya`.
9. **A move the booking rules do not allow is refused** (outline): a new start off the grid is 422 `OFF_GRID_START`; in the past or beyond the booking horizon is 422 `INVALID_VALUE`; before the first cell of the day is 422 `OUTSIDE_OPENING_HOURS`; each leaves the appointment unchanged.
10. **Only a confirmed appointment can be rescheduled**: rescheduling a checked-in appointment is 409 `INVALID_TRANSITION` and changes nothing.
11. **A reschedule sent with a stale version is refused**: after one move to 13:30, a second move sent with version 1 is 409 `VERSION_CONFLICT`; the appointment stays at 13:30, version 2.
12. **A customer can move their appointment to another free time**: an appointment booked through `CUSTOMER_WEB` is moved through `CUSTOMER_WEB` from 15:00 to 16:00; the response is the customer view at version 2 and the audit trail holds one `RESCHEDULED` event through `CUSTOMER_WEB`.

---

### User Story 4 - No-shows are released automatically, and late arrivals are still served (Priority: P2)

A confirmed appointment that nobody checked in is marked a no-show once its start plus the grace period (30 minutes by default) has passed, without anyone acting, and its cells are released. A no-show customer who turns up late is checked in onto the cells that are free from the current grid cell, on any qualified pair, in the same step; if nothing is free the advisor is told so and books a fresh appointment. A job completed after its booked time keeps the cells already in the past (design 4.3, 5.6, 11.4; notes 4.7; `markNoShow`, `checkInAppointment`, `completeAppointment`).

**Why this priority**: without the sweeper, capacity held by customers who never came stays blocked until someone notices; the late arrival is the only way back from `NO_SHOW`.

**Independent Test**: move a confirmed appointment's time into the past and watch its public record turn `NO_SHOW` within seconds, with its cells released.

**Acceptance Scenarios** (`features/noshow.feature`):

1. **A late arrival is checked in onto the cells free now, and refused when none are** (`@clock`): **Given** Alice's and Bob's `INTERIM` appointments at `LDS01` have both been marked no-shows, **When** Alice's is checked in, **Then** it is `CHECKED_IN` at version 3, starts at the current grid cell, is on `Bay L1` with `Jo` and owns 3 bay and 3 technician cells; **When** Bob's is checked in, **Then** the response is 409 `SLOT_TAKEN` with 1 to 5 alternatives, and Bob's appointment is still `NO_SHOW` at version 2.
2. **The sweeper marks a confirmed appointment as a no-show once its grace period has passed** (`@clock`): **Given** Alice's appointment, **When** it is backdated by 14 days, **Then** within 10 seconds it is `NO_SHOW` at version 2, owns no cells, and its audit trail holds one `NO_SHOW` event by `no-show-sweeper`.
3. **The sweeper leaves an appointment that has been checked in** (`@clock`): **Given** Bob's checked-in appointment and Alice's confirmed appointment are both backdated by 14 days, **When** Alice's has become `NO_SHOW`, **Then** Bob's is still `CHECKED_IN` at version 2 and owns its cells.
4. **Completing a job after its booked time keeps the cells already in the past** (`@clock`): **Given** Alice's appointment has been checked in, started and backdated by 14 days, **When** it is completed, **Then** it is `COMPLETED` at version 4 with actual start and end times and still owns 3 bay and 3 technician cells.

---

### User Story 5 - Refusals are explicit and change nothing (Priority: P3)

A transition that is not allowed, is sent with a stale version, is malformed, names an unknown appointment, or is a staff action sent through a customer channel is refused with a stable problem code, and the appointment, its cells and its audit trail are exactly as before (contract `TransitionConflict`, `ProblemCode`).

**Why this priority**: clients build their retry and conflict handling on these codes; a refusal that half-applied would corrupt the ledger.

**Independent Test**: send each refused request and fetch the appointment afterwards; status and version are unchanged.

**Acceptance Scenarios** (`features/lifecycle.feature`):

1. **A transition sent with a stale version is refused and changes nothing**: starting a checked-in appointment with version 1 is 409 `VERSION_CONFLICT`; it stays `CHECKED_IN` at version 2.
2. **A transition the appointment's status does not allow is refused and changes nothing** (outline): start or complete from `CONFIRMED`; check-in or no-show from `CHECKED_IN`; cancel from `IN_PROGRESS`, `COMPLETED` or `NO_SHOW`; check-in from `CANCELLED`; start from `NO_SHOW`; each is 409 `INVALID_TRANSITION` with status and version unchanged.
3. **Day-of transitions are refused to customer channels** (outline): check-in, start, complete and no-show through `CUSTOMER_WEB` or `CUSTOMER_APP` are 403 `FORBIDDEN`; the appointment stays `CONFIRMED` at version 1.
4. **A malformed transition request is rejected** (outline): a missing or non-integer `expectedVersion`, a missing `X-Channel`, a `startedAt` without an offset, a reschedule naming nothing to change, or a technician id that is not a UUID is 400 `VALIDATION_ERROR`; `expectedVersion` 0 or a negative `mileage` is 422 `INVALID_VALUE`; each names the field.
5. **A transition on an unknown appointment is not found**: cancelling an id that does not exist is 404 `NOT_FOUND`.

---

### Edge Cases

- **Cancel of a cancelled appointment** returns 200 with the record unchanged (no new version, audit event or outbound event), whatever `expectedVersion` it carries, because a retried cancel carries the version it was first sent with (contract `cancelAppointment`).
- **Two people act on one appointment at once**: the first to commit wins; the other gets 409 `VERSION_CONFLICT`, even if both sent the right version when they read it, and changes nothing.
- **The sweeper and a person act on one appointment at once** (staff check in just as the grace period ends): the same rule. If staff win, the sweeper leaves the appointment alone; if the sweeper wins, staff get `VERSION_CONFLICT`, fetch the `NO_SHOW` record, and check it in as a late arrival.
- **A reschedule races a booking for the cells it frees** (design 11.3, "the one deadlock"): the database breaks the cycle by rolling one side back; a reschedule chosen as the victim is retried once on the same pair, then treated like a taken pair. The ledger is never left half-moved.
- **A move to the same start on the same pair** (a reassignment to the bay and technician it already has) succeeds as an ordinary reschedule: the version rises by one and a `RESCHEDULED` event is recorded; no cell changes.
- **A reassignment that omits `startAt` after the booked start has passed** (a `CONFIRMED` appointment inside its grace period) is refused with 422 `INVALID_VALUE` on `startAt`: every move must land in the future.
- **The current pair can take the new time**: a move keeps the appointment's bay and technician when they are free for the new runs, and only otherwise chooses another pair as booking does.
- **A replay of the original booking request after a move**: the original booking key no longer has an appointment, so the replay books a new appointment at the old time. The contract's "safe to repeat" covers lost responses, not a client that keeps retrying after the appointment moved.
- **Moving onto the booking key of another of the customer's appointments** (same vehicle, customer, start and kind, in any status) is 409 `DUPLICATE_BOOKING` carrying that appointment.
- **A late arrival whose skills or cells have gone** (the technician's skill expired, or the current cell is outside working time): nothing is free, so 409 `SLOT_TAKEN` with alternatives.
- **Completing with a `completedAt` earlier than the actual start** is 422 `INVALID_VALUE` on `completedAt`; a `startedAt` or `completedAt` in the future is 422 `INVALID_VALUE` on that field.
- **A job that runs past its booked end** keeps all its cells on completion, because every cell is in the past; overrun detection is out of scope.

## Requirements *(mandatory)*

### Functional Requirements

**Common to every transition**

- **FR-001**: The system MUST serve `rescheduleAppointment` (`POST /appointments/{appointmentId}/reschedule`), `cancelAppointment` (`.../cancel`), `checkInAppointment` (`.../check-in`), `startAppointment` (`.../start`), `completeAppointment` (`.../complete`) and `markNoShow` (`.../no-show`), each answering 200 with the `Appointment` record after the transition, in the calling channel's view, exactly as `getAppointment` would return it.
- **FR-002**: Every request MUST carry the `X-Channel` header under the same rules as booking, and a JSON body of the contract's request type (`RescheduleRequest`, `TransitionRequest`, `CheckInRequest`, `StartRequest`, `CompleteRequest`) with a required integer `expectedVersion`. A malformed body, a missing or non-integer `expectedVersion`, an identifier that is not a UUID, or a date-time without an offset MUST be 400 `VALIDATION_ERROR` naming the field; `expectedVersion` below 1, a negative `mileage`, a `reason` longer than 500 characters or `additionalConcerns` longer than 4000 MUST be 422 `INVALID_VALUE` naming the field. A reschedule MUST name at least one of `startAt`, `technicianId`, `serviceBayId`, otherwise 400 `VALIDATION_ERROR` naming `startAt`.
- **FR-003**: Checks MUST apply in this order, stopping at the first that fails: (1) the request (FR-002); (2) the channel may perform the action (FR-008); (3) the appointment exists, otherwise 404 `NOT_FOUND`; (4) a cancel of a `CANCELLED` appointment returns it unchanged (FR-021); (5) `expectedVersion` equals the current version, otherwise 409 `VERSION_CONFLICT`; (6) the state machine allows the transition from the current status, otherwise 409 `INVALID_TRANSITION`; (7) the rules of the operation itself.
- **FR-004**: The state machine MUST be exactly design 4.3 and the contract's `AppointmentStatus`: `CONFIRMED` → `CONFIRMED` (reschedule), `CHECKED_IN`, `CANCELLED`, `NO_SHOW`; `NO_SHOW` → `CHECKED_IN` (late arrival); `CHECKED_IN` → `IN_PROGRESS`, `CANCELLED`; `IN_PROGRESS` → `COMPLETED`. `COMPLETED` and `CANCELLED` are final, and so is `NO_SHOW` apart from the late arrival.
- **FR-005**: A successful transition MUST raise `version` by exactly one and set `updatedAt`. If the appointment changed between the caller's read and the write, the transition MUST be refused with 409 `VERSION_CONFLICT` and change nothing.
- **FR-006**: Each transition MUST be all or nothing: the appointment's new state, its ledger change, one audit event and one outbound event are stored together, or none of them is.
- **FR-007**: The audit event types MUST be `RESCHEDULED`, `CANCELLED`, `CHECKED_IN`, `STARTED`, `COMPLETED`, `NO_SHOW` (contract `AppointmentEvent`), recorded with the calling channel and the actor `anonymous`, or with no channel and the actor `no-show-sweeper` when the sweeper acts. The outbound events MUST be `AppointmentRescheduled`, `AppointmentCancelled`, `AppointmentCheckedIn`, `AppointmentStarted`, `AppointmentCompleted`, `AppointmentNoShow` (design 6). The audit payload MUST carry the `reason` when given, the previous and new start, end, bay and technician for a reschedule or a late arrival, and `mileage` and `additionalConcerns` for a check-in.
- **FR-008**: Check-in, start, complete and no-show, and a reschedule that names `technicianId` or `serviceBayId`, are staff actions: sent through `CUSTOMER_WEB` or `CUSTOMER_APP` they MUST be refused with 403 `FORBIDDEN`. Cancel and a reschedule in time only are allowed on every channel. Whether a customer acts on their own appointment is not checked in this release.

**Reschedule and reassign** (`rescheduleAppointment`, design 5.4)

- **FR-010**: Reschedule MUST be allowed from `CONFIRMED` only and keep the status `CONFIRMED`. A `NO_SHOW` is brought back by check-in, not by reschedule.
- **FR-011**: The new start is `startAt` when given, otherwise the current start. The booking rules MUST apply to it in booking's order: a staff-only service type through a customer channel is 422 `SERVICE_NOT_BOOKABLE_ONLINE`; the start must be on the grid (422 `OFF_GRID_START`), after now and within the booking horizon (422 `INVALID_VALUE` on `startAt`); every cell of both runs must be working time (422 `OUTSIDE_OPENING_HOURS`). The service type, dealership, customer, vehicle and kind do not change; the new end is the new start plus the service duration.
- **FR-012**: A named `technicianId` MUST be an active technician of the appointment's dealership (otherwise 422 `INVALID_VALUE` naming `technicianId`) who holds every required skill valid on the new local date (otherwise 422 `NO_QUALIFIED_TECHNICIAN`). A named `serviceBayId` MUST be a bay of the dealership that is active and of a type the service type allows (otherwise 422 `INVALID_VALUE` naming `serviceBayId`). With no technician named, some active technician must qualify, otherwise 422 `NO_QUALIFIED_TECHNICIAN`.
- **FR-013**: Candidate pairs MUST be chosen as in booking, restricted to the named bay and technician, with the cells the appointment already owns counting as free. The appointment's current pair MUST be tried first when it is a candidate; the others follow least-loaded first.
- **FR-014**: For each pair tried, one transaction MUST update the appointment, claim the new bay run and technician run with the appointment's own cells counting as claimable, and release every cell the appointment owns outside the two new runs, matched on resource as well as time, so a new technician frees the old one's cells. A taken cell moves to the next pair; a deadlock victim is retried once on the same pair; missing cells recompute the candidates once, then 409 `AVAILABILITY_CHANGED`. With no pair left the answer MUST be 409 `SLOT_TAKEN` with up to five alternatives computed as for booking, with the appointment's own cells counting as free and the named resources respected. In every refusal the appointment and its cells MUST be unchanged.
- **FR-015**: A new start that is the booking key of another appointment (same vehicle, customer, start and kind, in any status) MUST be refused with 409 `DUPLICATE_BOOKING` carrying that appointment.

**Cancel** (`cancelAppointment`)

- **FR-020**: Cancel MUST be allowed from `CONFIRMED` and `CHECKED_IN`, set `CANCELLED` and release every cell the appointment owns.
- **FR-021**: Cancel of a `CANCELLED` appointment MUST return 200 with the record unchanged, whatever `expectedVersion` it carries, and record nothing.

**Check-in and late arrival** (`checkInAppointment`)

- **FR-030**: Check-in from `CONFIRMED` MUST set `CHECKED_IN` and leave the ledger unchanged.
- **FR-031**: Check-in from `NO_SHOW` is a late arrival. In the same transaction it MUST claim cells for the job starting at the current grid cell (the cell that contains now): the new start is that cell, the runs follow from the service type as in booking, and the pair is the appointment's previous pair if it is free, otherwise any qualified pair, least-loaded first. The appointment's start, end, bay and technician change accordingly and the status becomes `CHECKED_IN`. The booking horizon and the "after now" rule do not apply.
- **FR-032**: If no qualified pair is free for those runs, for whatever reason (cells taken, cells missing, nobody qualified), the late arrival MUST be refused with 409 `SLOT_TAKEN` and alternatives computed as for booking from the current grid cell, and the appointment MUST stay `NO_SHOW`.

**Start and complete** (`startAppointment`, `completeAppointment`)

- **FR-040**: Start MUST set `IN_PROGRESS` and record the actual start as `startedAt` when given, otherwise now; a `startedAt` after now is 422 `INVALID_VALUE`. The ledger is unchanged.
- **FR-041**: Complete MUST set `COMPLETED`, record the actual end as `completedAt` when given, otherwise now, and release the cells that start at or after the current grid cell rounded up; cells that start earlier stay booked by the appointment. A `completedAt` after now or before the actual start is 422 `INVALID_VALUE`. Which cells are released depends on now, never on `completedAt`.

**No-show** (`markNoShow` and the sweeper)

- **FR-050**: Staff MUST be able to mark a `CONFIRMED` appointment `NO_SHOW` at any time; every cell it owns is released.
- **FR-051**: A sweeper MUST run inside the service on a configurable interval (default one minute) and mark `NO_SHOW` every `CONFIRMED` appointment whose start plus the grace period is strictly before now, applying exactly the transition of FR-050, one appointment per transaction, oldest start first. The grace period MUST be configurable, default 30 minutes.
- **FR-052**: The sweeper MUST skip an appointment that changed since it was read (another actor won), keep going after a failure on one appointment, and stop when the service shuts down.

**The record and the ledger**

- **FR-060**: The `Appointment` record MUST show `actualStartAt` and `actualEndAt` once set and `null` before. Its owned cell ranges MUST reflect the ledger after the transition; a resource the appointment owns no cell on has no range.
- **FR-061**: After any transition, no cell is owned by two appointments, a `CANCELLED` or `NO_SHOW` appointment owns no cell, and a `COMPLETED` appointment owns only cells that started before the current grid cell when it was completed.
- **FR-062**: The appointment table MUST gain the columns `actual_start_at` and `actual_end_at` exactly as `docs/design/schema.sql` defines them.

### Out of scope

- Availability search (`GET /dealerships/{id}/availability`), listing appointments (`listAppointments`), the schedule view.
- The outbox relay, publishing events, notifications, the DMS adapter and repair orders: outbound events are written and not published.
- Setup CRUD, the materialiser, rotas, holds, technician absences, capacity blocks, channel reservations, daily caps (design 1.4).
- Authentication and authorisation beyond FR-008: tokens, roles, dealership scoping, and whether a customer acts on their own appointment.
- Overrun detection and at-risk flags, automatic reallocation when a technician drops out, reinstating a cancelled appointment, undoing a completion.
- Any test-only endpoint, fake clock or worker trigger in the running service, and any route beyond the six operations.

### Key Entities *(include if feature involves data)*

- **Appointment**: as in feature 001, plus the actual start and end times. Its status follows the state machine (FR-004) and its `version` rises with every transition. Start, end, bay and technician change on a reschedule or a late arrival.
- **Ledger cell**: one bay or technician for one grid cell, `FREE` or `BOOKED` by one appointment. Transitions claim cells (reschedule, late arrival) and release them (reschedule, cancel, no-show, complete).
- **Audit event**: one per transition, append-only, with type, actor, channel and payload.
- **Outbound event**: one per transition, written in the same step, for later publication.
- **No-show sweeper**: the background job that applies the no-show transition when the grace period has passed.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Every scenario named in this spec passes, and every feature 001 scenario still passes, in one run of the acceptance suite.
- **SC-002**: In every scenario, a refused transition leaves the appointment's status, version and owned cells exactly as before, and a successful one raises its version by exactly one.
- **SC-003**: Capacity given back by a cancel, a no-show, a move or an early completion can be booked by the very next request, with no delay.
- **SC-004**: A confirmed appointment nobody checked in is marked a no-show and its capacity released no later than the grace period plus one sweeper interval after its start (31 minutes with the defaults; seconds in the test configuration).
- **SC-005**: No bay or technician is ever held by two appointments for the same time, including while a move races a booking for the cells it frees.
- **SC-006**: Staff see the result of a transition in under 200 milliseconds for 95% of requests at the booking load design 7 targets (50 requests per second on one node).

## Assumptions

- The acceptance scenarios are the approval artefact (constitution Principle I); they must be approved before implementation starts.
- A `@clock` scenario's backdated window loses its cells (they are the rows that moved), so it is never booked again by any scenario; the calendar in `docs/development.md` records these windows.
- Which cells complete releases depends on now, never on `completedAt`: `completedAt` exists to back-fill clocking data from the dealer system, while the ledger must reflect what is free from now on.
- The sweeper acts through the same no-show transition staff use, with the actor `no-show-sweeper` and no channel; it marks every overdue appointment of every dealership in each run, with no batch limit, in one process (one instance runs in this release).
- Check-in, start, complete and the staff no-show have no time window: staff may act on any appointment in the right status whatever the time, which also lets the acceptance suite drive them on future days. Only the sweeper applies the grace period.
- `mileage` and `additionalConcerns` from a check-in are recorded only in the `CHECKED_IN` audit event's payload; the record has no field for them.
- `actualStartAt` and `actualEndAt` are rendered in the dealership's time zone, like `startAt` and `endAt`.
- An outbound event's payload is the appointment record after the transition, in the staff view, as for `AppointmentConfirmed`.
- Events and outbound rows are only written; publishing them is a later feature.
- `LDS01` gains one bay and one technician with cells around the clock (see the fixture above). It is the only fixture change; the customers already exist.
