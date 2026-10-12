# Data Model: Book an appointment

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Date**: 2026-10-12

The schema is `docs/design/schema.sql`. This feature copies its 15 tables verbatim into `local/mysql/schema.sql` and proposes no table, column, index or constraint of its own. Below, every entity is mapped to its table by name, with the columns this feature reads or writes as they are named in that file, the Go type that represents it, and the rules the feature enforces on it.

## Tables added to `local/mysql/schema.sql`

Copied verbatim from `docs/design/schema.sql`, in this order so every foreign key points at an existing table:

| # | Table | Domain (owner of its `mysql` package) | Used by this feature for |
|---|---|---|---|
| 1 | `tenant` | setup | tenant of the dealership; every referenced row must share it |
| 2 | `dealership` | setup | time zone, `slot_minutes` (the grid), `booking_horizon_days`, code and name for the record |
| 3 | `service_bay` | setup | ACTIVE bays and their type; name and type for the record |
| 4 | `technician` | setup | ACTIVE technicians; name for the record |
| 5 | `skill` | setup | skill codes |
| 6 | `technician_skill` | setup | skills valid on the appointment's local date (`valid_from`, `valid_to`) |
| 7 | `service_type` | setup | duration, buffers, `online_bookable`, `active`; code and name for the record |
| 8 | `service_type_bay_type` | setup | allowed bay types (any one of them) |
| 9 | `service_type_skill` | setup | required skills (all of them) |
| 10 | `customer` | customers | existence, tenant, `display_name` for the record |
| 11 | `vehicle` | customers | existence, tenant, owner (`customer_id`), registration, make and model for the record |
| 12 | `appointment` | scheduling | the booking; `uq_appointment_booking` is the idempotency guard |
| 13 | `time_slot` | scheduling | the ledger: islands, loads, working time, the claim, owned cell ranges |
| 14 | `appointment_event` | scheduling | the `CONFIRMED` audit event; `include=events` |
| 15 | `outbox` | scheduling | the `AppointmentConfirmed` event, written, not published |

The schema's header comment, the comment above `appointment` and the claim comment block after `time_slot` are copied with them.

## Entities

### Appointment (`appointment`)

Go: `scheduling.Appointment` in `internal/scheduling/appointment.go` (notes 2, 4.4); `scheduling.BookingKey{VehicleID, CustomerID, StartAt, Kind}`.

| Column | Written on book | Rule |
|---|---|---|
| `id` | service-generated UUID (`platform.IDs`), one per request | also the customer-facing reference (design 11.1) |
| `tenant_id` | the dealership's tenant | customer, vehicle and service type must have the same tenant, else 422 `INVALID_VALUE` |
| `dealership_id`, `customer_id`, `vehicle_id`, `service_type_id` | from the request | must exist; `vehicle.customer_id` must equal `customer_id`; `service_type.active` must be true |
| `kind` | from the request | `scheduling.AppointmentKind`: `WAITER`, `DROP_OFF`, `LOANER` (notes 5.4); recorded, not enforced |
| `start_at` | the requested instant, UTC | on the grid; after now; at most `booking_horizon_days` ahead |
| `end_at` | `start_at + duration_minutes` | no buffers (design 4.2) |
| `service_bay_id`, `technician_id` | the claimed pair | denormalised from the cells (design 11.1) |
| `status` | `CONFIRMED` | `scheduling.AppointmentStatus`; this feature only creates `CONFIRMED` |
| `customer_requests` | the request's `customerRequests` (only `waiting`, `courtesyCar`, `collectionAndDelivery`, `notes`), or `{}` | `notes` at most 2000 characters |
| `notes` | the request's `notes` for staff channels; `NULL` for customer channels | at most 4000 characters; ignored for customer channels (contract) |
| `channel` | the `X-Channel` header | `scheduling.Channel` |
| `version` | `1` (column default) | |
| `created_at`, `updated_at` | column defaults | |

Uniqueness: `uq_appointment_booking (vehicle_id, customer_id, start_at, kind)`, every status (design 4.1.4, D4). Read paths: `AppointmentByBookingKey` (the key), `Appointment` (by id).

State: this feature has one transition, `[*] → CONFIRMED` (design 4.3 `book`). The transition table in `scheduling/enums.go` (notes 5.4) is added with feature 002, which is the first feature to use it.

### Ledger cell (`time_slot`)

Go: `scheduling.Cell`, `scheduling.CellRun{First, Cells}`, `scheduling.Grid{SlotMinutes}` (notes 4.1); `scheduling.ResourceKind` (`BAY`, `TECHNICIAN`), `scheduling.CellStatus` (`FREE`, `BOOKED`).

- Read: islands of FREE cells and BOOKED counts per resource for one local day (`dealership_id`, `slot_start`, `status` through `ix_slot_day`); count of distinct `slot_start` in a range for working time (R6); owned cells of an appointment (`appointment_id` through `ix_slot_appt`) for the record.
- Written: only by `Tx.ClaimPair`: `status` `FREE` → `BOOKED`, `appointment_id` set, `version + 1`, for every cell of the bay run, then of the technician run (R1).
- Rules: the primary key `(resource_kind, resource_id, slot_start)` is the uniqueness guarantee; `ck_slot_owner` keeps `appointment_id` set iff `BOOKED`; a missing row is time the resource does not work (design 4.2).
- Exposed: as `cells.bay` and `cells.technician` (`CellRange{from, to, cells}`, half-open) on the staff view of the record.

### Service requirement (`service_type`, `service_type_skill`, `service_type_bay_type`, `skill`)

Go: `scheduling.ServiceRequirement{ServiceTypeID, Duration, BufferBefore, BufferAfter, RequiredSkills, AllowedBayTypes, OnlineBookable}` (notes 4.2), built by `scheduling/adapters` from `setup.Repository`. `Runs(start, grid)` gives the bay run and the technician run: both start at `start - BufferBefore` and cover `BufferBefore + Duration + BufferAfter` rounded up to whole cells (notes 4.2; identical in this release).

### Resources (`service_bay`, `technician`, `technician_skill`)

Go: `scheduling.ServiceBay{ID, Type}`, `scheduling.Technician{ID, Skills}` as Scheduling sees them; `setup.BayType`, `setup.BayStatus`, `setup.TechnicianStatus` (notes 5.4). Only `ACTIVE` bays and technicians are candidates; a skill counts when `valid_from <= local date` and `valid_to` is null or `>= local date` (design 11.4).

### Dealership configuration (`dealership`)

Go: `scheduling.DealershipConfig{DealershipID, TenantID, Code, Name, Location, Grid, BookingHorizonDays}` (notes 4.2).

### Customer and vehicle (`customer`, `vehicle`)

Go: `customers.Customer`, `customers.Vehicle`; Scheduling sees them through `scheduling.CustomerReader` (notes 4.0).

### Candidate pair and island (computed, not stored)

Go: `scheduling.Island{Kind, ResourceID, From, To}`, `scheduling.Candidate{BayID, TechID, Load}` (notes 4.3). Load = BOOKED cells on the bay + BOOKED cells on the technician on the requested local date.

### Audit event (`appointment_event`)

Written once per booking: `appointment_id`, `event_type = 'CONFIRMED'`, `payload` (the appointment snapshot), `actor = 'anonymous'`, `channel`. Read for `include=events` as `AppointmentEvent{id, type, actor, channel, occurredAt, payload}`.

### Outbox event (`outbox`)

Written once per booking in the same transaction: `aggregate_type = 'Appointment'`, `aggregate_id` = appointment id, `event_type = 'AppointmentConfirmed'`, `payload` (the staff view of the record: names, times, cells, customer contact). `event_id` and `created_at` come from column defaults; `published_at` stays `NULL` (no relay in this release).

## Contract fields with no column

| Contract field | Situation in this feature |
|---|---|
| `Appointment.actualStartAt`, `Appointment.actualEndAt` | No column in `appointment`. Always `null` here. Feature 002 (start, complete) must resolve this against the schema; see the plan's open items |
| `Appointment.technician.skills` (design Appendix B only) | Not in the contract's `Appointment`; not returned |
| `Appointment.createdBy` (design Appendix B only) | Not in the contract; channel is returned as `channel`; the actor is only in the audit trail |

## Fixture (`local/mysql/data.sql`)

Rows, not columns: every value goes into a column that `docs/design/schema.sql` already defines. Ids are fixed so scenarios and the suite can name them; the suite's `fixture.go` mirrors this table.

| Kind | Name | Id | Details |
|---|---|---|---|
| tenant | Northern Motor Group | `a0000000-0000-4000-8000-000000000001` | |
| dealership | MAN01 Manchester Central | `d0000000-0000-4000-8000-000000000001` | Europe/London, `slot_minutes` 15, `booking_horizon_days` 28, `materialise_horizon_days` 60 |
| dealership | LDS01 Leeds North | `d0000000-0000-4000-8000-000000000002` | Europe/London; no bays, technicians or cells |
| bay | Bay 1 | `b0000000-0000-4000-8000-000000000001` | GENERAL, ACTIVE |
| bay | Bay 2 | `b0000000-0000-4000-8000-000000000002` | MOT, ACTIVE |
| bay | Bay 3 | `b0000000-0000-4000-8000-000000000003` | EV_HV, ACTIVE |
| technician | Sam | `e0000000-0000-4000-8000-000000000001` | ACTIVE; GENERAL_SERVICE, EV_HV_L3 |
| technician | Priya | `e0000000-0000-4000-8000-000000000002` | ACTIVE; GENERAL_SERVICE, MOT_TESTER |
| technician | Lee | `e0000000-0000-4000-8000-000000000003` | ACTIVE; GENERAL_SERVICE; AIRCON_FGAS expired yesterday |
| skill | GENERAL_SERVICE, MOT_TESTER, EV_HV_L3, AIRCON_FGAS | `5c000000-0000-4000-8000-00000000000{1,2,3,4}` | valid from a year ago, no end, except Lee's AIRCON_FGAS |
| service type | INTERIM | `5e000000-0000-4000-8000-000000000001` | 45 min, no buffers, GENERAL_SERVICE, bays GENERAL or MOT, online |
| service type | MAJOR | `5e000000-0000-4000-8000-000000000002` | 90 min, buffers 15/15, GENERAL_SERVICE, GENERAL, online |
| service type | MOT | `5e000000-0000-4000-8000-000000000003` | 45 min, MOT_TESTER, MOT, online |
| service type | EV_MAJOR | `5e000000-0000-4000-8000-000000000004` | 120 min, buffers 15/15, EV_HV_L3, EV_HV, online |
| service type | EV_CHECK | `5e000000-0000-4000-8000-000000000005` | 30 min, EV_HV_L3, GENERAL or EV_HV, online |
| service type | DIAGNOSTIC | `5e000000-0000-4000-8000-000000000006` | 50 min, GENERAL_SERVICE, GENERAL, staff only (`online_bookable` false) |
| service type | AIRCON | `5e000000-0000-4000-8000-000000000007` | 30 min, AIRCON_FGAS, GENERAL, online |
| customer / vehicle | Alice / YK21 ABC | `c0000000-0000-4000-8000-000000000001` / `f0000000-0000-4000-8000-000000000001` | Volkswagen ID.4 |
| customer / vehicle | Bob / BD70 XYZ | `c0000000-0000-4000-8000-000000000002` / `f0000000-0000-4000-8000-000000000002` | Ford Focus |
| customer / vehicle | Customer 01 to Customer 20 / TEST 01 to TEST 20 | `c0000000-0000-4000-8000-000000000101` to `...120` / `f0000000-0000-4000-8000-000000000101` to `...120` | one vehicle each |
| cells | every bay and technician of MAN01 | primary key | FREE; `CURRENT_DATE` to `CURRENT_DATE + 13`; 07:00Z to 16:45Z cell starts (40 per resource-day) |

## Validation summary (where each rule lives)

| Rule | Layer | Outcome |
|---|---|---|
| JSON shape, required fields, `startAt` with offset, `X-Channel` present | transport decoder | 400 `VALIDATION_ERROR` |
| `kind`, `X-Channel` value, text lengths | transport decoder (`scheduling.Parse*`) | 422 `INVALID_VALUE` |
| references exist, active, same tenant; vehicle owned by customer | service, through readers | 422 `INVALID_VALUE` |
| booking key owned | service, `Store.AppointmentByBookingKey` | 201 existing or 409 `DUPLICATE_BOOKING` |
| staff-only service on a customer channel | service | 422 `SERVICE_NOT_BOOKABLE_ONLINE` |
| start on the grid | `scheduling.Grid.Align` | 422 `OFF_GRID_START` |
| start after now and inside the horizon | service with `platform.Clock` | 422 `INVALID_VALUE` |
| runs inside working time | service, `Store.WorkingCells` | 422 `OUTSIDE_OPENING_HOURS` |
| a qualified technician exists | service, `WorkshopReader.Resources` | 422 `NO_QUALIFIED_TECHNICIAN` |
| every cell exists and is free | `Tx.ClaimPair` | next pair, recompute, or 409 |
