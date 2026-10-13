# Data model: Appointment lifecycle

The schema is `docs/design/schema.sql`; `local/mysql/schema.sql` is its copy for the tables this
release uses. This feature proposes no table and no column of its own. It uses four tables that
already exist and copies two columns the design schema already defines.

## Tables

| Table (`docs/design/schema.sql`) | Change in `local/mysql/schema.sql` | How this feature uses it |
|---|---|---|
| `appointment` | Gains `actual_start_at DATETIME(6) NULL` and `actual_end_at DATETIME(6) NULL`, copied verbatim, with their comments, between `end_at` and `service_bay_id`, as in the design file | `status`, `version`, `updated_at` change on every transition; `start_at`, `end_at`, `service_bay_id`, `technician_id` on a reschedule or late arrival; `actual_start_at` on start; `actual_end_at` on complete. The version-guarded `UPDATE ... WHERE id = ? AND version = ?` is the first statement of every transition (research R1). `uq_appointment_booking` also guards a reschedule's new start |
| `time_slot` | none | Claimed by `ClaimPair` (reschedule, late arrival), released by `ReleaseCells` (reschedule, cancel, no-show, complete). `ix_slot_appt` serves the release's locking read |
| `appointment_event` | none | One row per transition. `channel` is `NULL` for the sweeper (the column is nullable) |
| `outbox` | none | One row per transition, unpublished |

No migration: the compose database is created from `local/mysql/schema.sql` on every fresh start
(notes 5.1).

## Appointment state machine

The transition table lives in `internal/scheduling/enums.go`, next to `AppointmentStatus`, as notes
5.4 shows it:

| From | To | Operation | Ledger effect (design 4.3) |
|---|---|---|---|
| `CONFIRMED` | `CONFIRMED` | reschedule | claim the new runs (own cells claimable), release every owned cell outside them |
| `CONFIRMED` | `CHECKED_IN` | check-in | none |
| `CONFIRMED` | `CANCELLED` | cancel | release every owned cell |
| `CONFIRMED` | `NO_SHOW` | no-show (staff or sweeper) | release every owned cell |
| `NO_SHOW` | `CHECKED_IN` | check-in (late arrival) | claim runs from the current grid cell on a free qualified pair |
| `CHECKED_IN` | `IN_PROGRESS` | start | none; sets `actual_start_at` |
| `CHECKED_IN` | `CANCELLED` | cancel | release every owned cell |
| `IN_PROGRESS` | `COMPLETED` | complete | release owned cells starting at or after `Ceil(now)`; sets `actual_end_at` |

`CANCELLED → CANCELLED` is not a transition: a cancel of a cancelled appointment returns the record
unchanged before the table is consulted (spec FR-021).

Invariants after every committed transition (spec FR-061): no cell has two owners (primary key and
`ck_slot_owner`); `CANCELLED` and `NO_SHOW` appointments own no cell; a `COMPLETED` appointment owns
only cells that started before the grid cell it was completed in.

## Go types (domain root `internal/scheduling`, standard library only)

| Type | Change |
|---|---|
| `Appointment` | Gains `ActualStartAt, ActualEndAt *time.Time` |
| `AppointmentStatus` | Gains the transition table and `CanTransitionTo(AppointmentStatus) bool` |
| `Grid` | Gains `Floor(t)` (start of the cell containing `t`) and `Ceil(t)` (`t` if on the grid, else the next cell start) |
| `Keep` (new) | What `ReleaseCells` keeps: `Bay`, `Technician` (each a resource id and a `CellRun`, optional) and `Before time.Time` (zero: none) |
| `Store` | `DayLedger` gains an `owner` appointment id whose cells count as free (`""` for booking); new `DueNoShows(ctx, before time.Time) ([]Appointment, error)`: `CONFIRMED` with `start_at < before`, oldest first |
| `Tx` | New `UpdateAppointment(a Appointment, expectedVersion int) error` (`ErrStaleVersion` on zero rows, `ErrDuplicateBooking` on 1062) and `ReleaseCells(appointmentID string, keep Keep) (int, error)`; `ClaimPair` unchanged in signature, cells owned by the claimer count as claimable |
| errors | New `ErrStaleVersion` (`VERSION_CONFLICT`), `ErrInvalidTransition` (`INVALID_TRANSITION`), `ErrForbidden` (`FORBIDDEN`); `VersionConflictError{Current, Sent int}` is `ErrStaleVersion` and renders the contract's detail |

`WorkshopReader` and `CustomerReader` do not change: pinned resources are validated against
`Resources(dealershipID, localDate)` (research R4).

## Validation rules (from the contract's request schemas)

| Field | Operations | Rule | Refusal |
|---|---|---|---|
| `expectedVersion` | all | required, integer | 400 `VALIDATION_ERROR` |
| `expectedVersion` | all | `>= 1` | 422 `INVALID_VALUE` |
| `reason` | all | at most 500 characters | 422 `INVALID_VALUE` |
| `startAt` | reschedule | RFC 3339 with an offset | 400 `VALIDATION_ERROR` |
| `technicianId`, `serviceBayId` | reschedule | UUID | 400 `VALIDATION_ERROR` |
| one of `startAt`, `technicianId`, `serviceBayId` | reschedule | present | 400 `VALIDATION_ERROR` naming `startAt` |
| `mileage` | check-in | integer `>= 0` | 400 if not an integer, 422 if negative |
| `additionalConcerns` | check-in | at most 4000 characters | 422 `INVALID_VALUE` |
| `startedAt` | start | RFC 3339 with an offset; not after now | 400; 422 |
| `completedAt` | complete | RFC 3339 with an offset; not after now; not before the actual start | 400; 422 |

## Fixture rows (`local/mysql/data.sql`)

Added for the late arrival only (research R10, R13); everything else the scenarios need is already
there.

| Kind | Name | Id | Details |
|---|---|---|---|
| bay | `Bay L1` | `b0000000-0000-4000-8000-000000000011` | `LDS01`, GENERAL, ACTIVE |
| technician | `Jo` | `e0000000-0000-4000-8000-000000000011` | `LDS01`, ACTIVE |
| technician skill | `Jo`: GENERAL_SERVICE | (`e0...011`, `5c...001`) | valid from `CURRENT_DATE - 1 YEAR`, no end |
| ledger cells | `Bay L1`, `Jo` | primary key | FREE, `CURRENT_DATE` to `CURRENT_DATE + 13`, every 15-minute cell 00:00Z to 23:45Z: 2 x 14 x 96 = 2 688 rows |

The ledger then holds 3 360 + 2 688 = 6 048 cells. `test/acceptance/fixture.go` mirrors the two
names.
