# Contracts: Appointment lifecycle

The HTTP contract is **`api/openapi.yaml`**. It is not duplicated here and this feature does not
edit it. This feature implements six of its operations, each answering `200` with the
`Appointment` record (the same shape `getAppointment` returns, in the caller's channel view) or a
`Problem` (`application/problem+json`):

| operationId | Route | Request body | Allowed from | Failures |
|---|---|---|---|---|
| `rescheduleAppointment` | `POST /appointments/{appointmentId}/reschedule` | `RescheduleRequest` | `CONFIRMED` | 400 `VALIDATION_ERROR`; 403 `FORBIDDEN` (pins through a customer channel); 404 `NOT_FOUND`; 409 `VERSION_CONFLICT`, `INVALID_TRANSITION`, `SLOT_TAKEN` (+`alternatives`), `AVAILABILITY_CHANGED` (+`alternatives`), `DUPLICATE_BOOKING` (+`existing`); 422 `INVALID_VALUE`, `OFF_GRID_START`, `OUTSIDE_OPENING_HOURS`, `NO_QUALIFIED_TECHNICIAN`, `SERVICE_NOT_BOOKABLE_ONLINE` |
| `cancelAppointment` | `POST /appointments/{appointmentId}/cancel` | `TransitionRequest` | `CONFIRMED`, `CHECKED_IN`; a `CANCELLED` one is returned unchanged | 400; 404; 409 `VERSION_CONFLICT`, `INVALID_TRANSITION`; 422 `INVALID_VALUE` |
| `checkInAppointment` | `POST /appointments/{appointmentId}/check-in` | `CheckInRequest` | `CONFIRMED`; `NO_SHOW` (late arrival) | 400; 403 (customer channel); 404; 409 `VERSION_CONFLICT`, `INVALID_TRANSITION`, `SLOT_TAKEN` (+`alternatives`, late arrival only); 422 `INVALID_VALUE` |
| `startAppointment` | `POST /appointments/{appointmentId}/start` | `StartRequest` | `CHECKED_IN` | 400; 403; 404; 409 `VERSION_CONFLICT`, `INVALID_TRANSITION`; 422 `INVALID_VALUE` (`startedAt` after now) |
| `completeAppointment` | `POST /appointments/{appointmentId}/complete` | `CompleteRequest` | `IN_PROGRESS` | 400; 403; 404; 409 `VERSION_CONFLICT`, `INVALID_TRANSITION`; 422 `INVALID_VALUE` (`completedAt` after now or before the actual start) |
| `markNoShow` | `POST /appointments/{appointmentId}/no-show` | `TransitionRequest` | `CONFIRMED` | 400; 403; 404; 409 `VERSION_CONFLICT`, `INVALID_TRANSITION`; 422 `INVALID_VALUE` |

Every request carries `X-Channel` (feature 001's stand-in for the token's `channel` claim): missing
is 400 `VALIDATION_ERROR`, unknown is 422 `INVALID_VALUE`, both naming `X-Channel`. Routes are
unprefixed, as for feature 001 (the contract's `/v1` is read as a gateway base path). The order in
which refusals are decided is spec FR-003; the code table is research.md R8.

The record now carries `actualStartAt` and `actualEndAt` (dealership time zone) once set; they were
always `null` in feature 001.

## Where this release differs from the contract

| Contract says | This release does | Why |
|---|---|---|
| `security: bearerAuth`; roles and ownership from token claims | No token. A customer channel (`CUSTOMER_WEB`, `CUSTOMER_APP`) asking for a staff action is `403 FORBIDDEN`; ownership is not checked; `401` is never returned | Spec Clarifications: `X-Channel` is the channel claim's stand-in; authentication is out of scope |
| `TransitionConflict`: "or (late arrival) no cells are free" on every Lifecycle operation | Only `checkInAppointment` can answer `SLOT_TAKEN` | The other operations have no claim |
| `AppointmentEvent.actor` "User id, client id or job name" | `anonymous` for requests, `no-show-sweeper` for the sweeper | No authenticated principal exists |
| `CheckInRequest.mileage`, `additionalConcerns` | Recorded in the `CHECKED_IN` audit event's payload only | The `Appointment` schema and table have no field for them |

## Background worker (no HTTP surface)

The no-show sweeper runs inside the service (research R7). Its contract is configuration:

| Variable | Default | Meaning |
|---|---|---|
| `NO_SHOW_GRACE` | `30m` | A `CONFIRMED` appointment whose `startAt` plus this is before now is marked `NO_SHOW` |
| `NO_SHOW_SWEEP_INTERVAL` | `1m` | How often the sweeper runs; `docker-compose.yml` sets `1s` |

Its effect is observable only through `getAppointment`: status `NO_SHOW`, no cells, one `NO_SHOW`
event by `no-show-sweeper` with no channel.

## Other interfaces

- [acceptance-steps.md](./acceptance-steps.md): the Gherkin step vocabulary the three feature files
  use, reused from feature 001 where it fits. It is the contract between the feature files
  (approved first) and the step definitions in `test/acceptance/` (written in the implementation
  phase).
- Database: `docs/design/schema.sql`, see [../data-model.md](../data-model.md).
