# Contracts: Book an appointment

The HTTP contract is **`api/openapi.yaml`**, rendered by `api/docs.html`. It is not duplicated
here. This feature implements two of its operations:

| operationId | Route | Request | Success | Failures |
|---|---|---|---|---|
| `bookAppointment` | `POST /appointments` | `BookAppointmentRequest` | `201` `Appointment` + `Location` | `400` `VALIDATION_ERROR`; `409` `SLOT_TAKEN`, `AVAILABILITY_CHANGED`, `DUPLICATE_BOOKING`; `422` `OFF_GRID_START`, `OUTSIDE_OPENING_HOURS`, `NO_QUALIFIED_TECHNICIAN`, `SERVICE_NOT_BOOKABLE_ONLINE`, `INVALID_VALUE` |
| `getAppointment` | `GET /appointments/{appointmentId}` (`include=events` optional) | none | `200` `Appointment` | `404` `NOT_FOUND`; `400` `VALIDATION_ERROR` and `422` `INVALID_VALUE` for the channel header |

Every failure body is the contract's `Problem` (`application/problem+json`), with `alternatives`
(`Slot[]`) on `SLOT_TAKEN` and `AVAILABILITY_CHANGED`, `existing` on `DUPLICATE_BOOKING`, and
`errors[]` on `VALIDATION_ERROR` and `INVALID_VALUE`.

Not implemented by this feature: `listAppointments`, `rescheduleAppointment` and the Lifecycle
operations (feature 002).

## Where this release differs from the contract

These are release-level stand-ins, recorded in the spec's Clarifications and in research.md. The
contract file is not edited by this feature.

| Contract says | This release does | Why |
|---|---|---|
| `security: bearerAuth`; channel, tenant and roles come from token claims | No token is read; `401` and `403` are never returned. The caller states its channel in an **`X-Channel`** request header with a `Channel` value, required on every `/appointments` request. The tenant is the dealership's | Authentication is out of scope (decision 1); behaviour that depends on the channel (customer view, `SERVICE_NOT_BOOKABLE_ONLINE`) must still be reachable. Notes 4.0 anticipate a header in v1 |
| `servers: http://localhost:8080/v1` | Routes are served without the `/v1` prefix, next to `/healthz` | The feature description and notes 4.0 use unprefixed routes; `/v1` is read as a gateway base path. Reported as a conflict |
| `Appointment.actualStartAt`, `actualEndAt` | Always `null` | No column holds them (`docs/design/schema.sql`); nothing in this feature sets them |
| `AppointmentEvent.actor` "User id, client id or job name" | `anonymous` | No authenticated principal exists |

## Other interfaces

- [acceptance-steps.md](./acceptance-steps.md): the Gherkin step vocabulary the four feature files
  use. It is the contract between the feature files (approved first) and the step definitions in
  `test/acceptance/` (written in the implementation phase).
- Database: `docs/design/schema.sql`, see [../data-model.md](../data-model.md).
