-- =============================================================================
-- Unified Service Scheduler – MySQL schema (v3, time-grid capacity ledger)
-- v3 (2026-10-12): no stored routines or triggers, enumerations as VARCHAR,
-- no idempotency_key table (a booking is idempotent on its natural key:
-- vehicle, customer, start, kind). See IMPLEMENTATION_NOTES.md section 0 (D2, D3, D4).
-- Target: MySQL 8.0.16+ / 8.4 LTS, InnoDB, utf8mb4
--
-- Capacity model: every bay and every technician has one row per grid cell
-- (dealership.slot_minutes, default 15) for every cell inside dealership
-- opening hours. A booking claims a run of consecutive cells on one bay and
-- on one technician. A cell row that does not exist is time the resource is
-- not available. The customer-facing appointment keeps exact minute times;
-- only the capacity ledger is quantised.
--
-- SCOPE (v1): every cell that exists is bookable by anyone, and a booking is
-- confirmed in one transaction (no time-boxed holds). Out of scope: holds,
-- shift patterns, per-date rota overrides and HR import (a weekly rota per
-- resource is in; a resource with no rota works the opening hours),
-- technician absences, capacity blocks, channel reservations, per-service
-- daily caps, and reallocation of bookings when a technician drops out.
-- Each of those later changes only which cells exist or which are claimable;
-- none touches the booking flows.
--
-- Conventions:
--   * All instants are DATETIME(6) in UTC; cells are DATETIME on the grid.
--     The application sets time_zone = '+00:00' on every connection.
--   * Rows on the booking path are never hard-deleted; status changes free
--     capacity and preserve the audit trail.
--   * Every aggregate-root table (dealership, bay, technician, skill,
--     service type, customer, vehicle, appointment) carries tenant_id and the
--     repository filters on it (MySQL has no row-level security). Child and
--     ledger tables (opening hours, technician_skill, time_slot, events) are
--     reached only through their parent and carry the parent key instead.
--   * IDs are CHAR(36) UUIDs for readability; BINARY(16) in production.
--   * Double-booking prevention: the application locks the cells of a run
--     with SELECT ... FOR UPDATE, checks in code that every cell exists and
--     is FREE (or owned by the appointment being rescheduled), then UPDATEs
--     them to BOOKED, all in one transaction. A locking read returns current
--     data and takes an exclusive lock per row, so this is correct at any
--     isolation level. store.Tx.ClaimPair is the single sanctioned claim path
--     and store.Tx.ReleaseCells the single release path; see the comment
--     block after time_slot. MySQL error 1213 (deadlock victim, possible when
--     a reschedule races a booking on the same cells) is retried once.
--   * No stored procedures, functions or triggers. All logic lives in the
--     application so it is versioned, tested and reviewed as code.
--   * Enumerated values (bay type, statuses, kind, channel, resource kind)
--     are VARCHAR(20) columns. The allowed values are defined once in the
--     application (internal/domain/enums.go) and validated there, both on
--     input and when rows are read. Only cross-column invariants are CHECK
--     constraints here.
--   * The grid is anchored to the UTC clock: every cell starts at a whole
--     multiple of slot_minutes from the top of the hour. The materialiser and
--     the claim both reject a start that is not (OFF_GRID_START), in code.
-- =============================================================================
-- NOTE: opening_hours, opening_exception and rota_assignment are not part of this release.
-- They feed the materialiser, which is not built yet; cells come from the fixture in
-- local/mysql/data.sql.

SET NAMES utf8mb4;
SET time_zone = '+00:00';

CREATE DATABASE IF NOT EXISTS scheduler
  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
USE scheduler;

-- -----------------------------------------------------------------------------
-- Tenancy & dealerships
-- -----------------------------------------------------------------------------
CREATE TABLE tenant (
    id          CHAR(36)     NOT NULL DEFAULT (UUID()),
    name        VARCHAR(200) NOT NULL,
    created_at  DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id)
) ENGINE=InnoDB;

CREATE TABLE dealership (
    id                     CHAR(36)     NOT NULL DEFAULT (UUID()),
    tenant_id              CHAR(36)     NOT NULL,
    code                   VARCHAR(20)  NOT NULL,              -- 'MAN01'
    name                   VARCHAR(200) NOT NULL,
    timezone               VARCHAR(64)  NOT NULL,              -- IANA, 'Europe/London'
    slot_minutes           TINYINT UNSIGNED NOT NULL DEFAULT 15,   -- grid cell; set at deployment, do not change
    booking_horizon_days   SMALLINT UNSIGNED NOT NULL DEFAULT 28,  -- how far ahead customers may book
    materialise_horizon_days SMALLINT UNSIGNED NOT NULL DEFAULT 60, -- how far ahead cells exist
    created_at             DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_dealership_code (tenant_id, code),
    CONSTRAINT fk_dealership_tenant FOREIGN KEY (tenant_id) REFERENCES tenant(id),
    CONSTRAINT ck_dealership_slot CHECK (slot_minutes IN (10, 15, 20, 30)),
    CONSTRAINT ck_dealership_horizons CHECK (booking_horizon_days <= materialise_horizon_days)
) ENGINE=InnoDB;

-- -----------------------------------------------------------------------------
-- Resources: bays, technicians, skills
-- -----------------------------------------------------------------------------
CREATE TABLE service_bay (
    id             CHAR(36)     NOT NULL DEFAULT (UUID()),
    tenant_id      CHAR(36)     NOT NULL,
    dealership_id  CHAR(36)     NOT NULL,
    name           VARCHAR(100) NOT NULL,
    type           VARCHAR(20)  NOT NULL DEFAULT 'GENERAL',   -- domain.BayType: GENERAL|MOT|EV_HV|ALIGNMENT|BODYSHOP|EXPRESS
    status         VARCHAR(20)  NOT NULL DEFAULT 'ACTIVE',    -- domain.BayStatus: ACTIVE|OUT_OF_SERVICE|RETIRED
    created_at     DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_bay_name (dealership_id, name),
    CONSTRAINT fk_bay_tenant     FOREIGN KEY (tenant_id)     REFERENCES tenant(id),
    CONSTRAINT fk_bay_dealership FOREIGN KEY (dealership_id) REFERENCES dealership(id)
) ENGINE=InnoDB;

CREATE TABLE technician (
    id                 CHAR(36)     NOT NULL DEFAULT (UUID()),
    tenant_id          CHAR(36)     NOT NULL,
    dealership_id      CHAR(36)     NOT NULL,
    display_name       VARCHAR(200) NOT NULL,
    status             VARCHAR(20)  NOT NULL DEFAULT 'ACTIVE', -- domain.TechnicianStatus: ACTIVE|INACTIVE
    created_at         DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY ix_technician_dealership (dealership_id, status),
    CONSTRAINT fk_tech_tenant     FOREIGN KEY (tenant_id)     REFERENCES tenant(id),
    CONSTRAINT fk_tech_dealership FOREIGN KEY (dealership_id) REFERENCES dealership(id)
) ENGINE=InnoDB;

CREATE TABLE skill (
    id         CHAR(36)     NOT NULL DEFAULT (UUID()),
    tenant_id  CHAR(36)     NOT NULL,
    code       VARCHAR(50)  NOT NULL,                            -- 'MOT_TESTER', 'EV_HV_L3'
    name       VARCHAR(200) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_skill_code (tenant_id, code),
    CONSTRAINT fk_skill_tenant FOREIGN KEY (tenant_id) REFERENCES tenant(id)
) ENGINE=InnoDB;

CREATE TABLE technician_skill (
    technician_id  CHAR(36) NOT NULL,
    skill_id       CHAR(36) NOT NULL,
    valid_from     DATE     NOT NULL DEFAULT (CURRENT_DATE),
    valid_to       DATE     NULL,
    PRIMARY KEY (technician_id, skill_id),
    KEY ix_tech_skill_skill (skill_id, technician_id),
    CONSTRAINT fk_ts_technician FOREIGN KEY (technician_id) REFERENCES technician(id) ON DELETE CASCADE,
    CONSTRAINT fk_ts_skill      FOREIGN KEY (skill_id)      REFERENCES skill(id),
    CONSTRAINT ck_ts_validity   CHECK (valid_to IS NULL OR valid_to >= valid_from)
) ENGINE=InnoDB;

-- -----------------------------------------------------------------------------
-- Service catalogue
-- -----------------------------------------------------------------------------
CREATE TABLE service_type (
    id                     CHAR(36)     NOT NULL DEFAULT (UUID()),
    tenant_id              CHAR(36)     NOT NULL,
    code                   VARCHAR(50)  NOT NULL,              -- opcode: 'MOT', 'INTERIM', 'EV_MAJOR'
    name                   VARCHAR(200) NOT NULL,
    duration_minutes       SMALLINT UNSIGNED NOT NULL,         -- flat-rate labour time
    buffer_before_minutes  SMALLINT UNSIGNED NOT NULL DEFAULT 0,
    buffer_after_minutes   SMALLINT UNSIGNED NOT NULL DEFAULT 0,
    online_bookable        BOOLEAN      NOT NULL DEFAULT TRUE,
    active                 BOOLEAN      NOT NULL DEFAULT TRUE,
    PRIMARY KEY (id),
    UNIQUE KEY uq_service_code (tenant_id, code),
    CONSTRAINT fk_service_tenant FOREIGN KEY (tenant_id) REFERENCES tenant(id),
    CONSTRAINT ck_service_duration CHECK (duration_minutes > 0)
) ENGINE=InnoDB;

CREATE TABLE service_type_bay_type (                            -- any one of these (OR)
    service_type_id  CHAR(36) NOT NULL,
    bay_type         VARCHAR(20) NOT NULL,                      -- domain.BayType
    PRIMARY KEY (service_type_id, bay_type),
    CONSTRAINT fk_stbt_service FOREIGN KEY (service_type_id) REFERENCES service_type(id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE service_type_skill (                               -- all of these (AND)
    service_type_id  CHAR(36) NOT NULL,
    skill_id         CHAR(36) NOT NULL,
    PRIMARY KEY (service_type_id, skill_id),
    CONSTRAINT fk_sts_service FOREIGN KEY (service_type_id) REFERENCES service_type(id) ON DELETE CASCADE,
    CONSTRAINT fk_sts_skill   FOREIGN KEY (skill_id)        REFERENCES skill(id)
) ENGINE=InnoDB;

-- -----------------------------------------------------------------------------
-- Customers & vehicles (snapshots; the DMS is master)
-- -----------------------------------------------------------------------------
CREATE TABLE customer (
    id            CHAR(36)     NOT NULL DEFAULT (UUID()),
    tenant_id     CHAR(36)     NOT NULL,
    dms_ref       VARCHAR(100) NULL,
    display_name  VARCHAR(200) NOT NULL,
    email         VARCHAR(320) NULL,
    phone         VARCHAR(30)  NULL,
    created_at    DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_customer_dms (tenant_id, dms_ref),
    KEY ix_customer_phone (tenant_id, phone),
    CONSTRAINT fk_customer_tenant FOREIGN KEY (tenant_id) REFERENCES tenant(id)
) ENGINE=InnoDB;

CREATE TABLE vehicle (
    id            CHAR(36)     NOT NULL DEFAULT (UUID()),
    tenant_id     CHAR(36)     NOT NULL,
    customer_id   CHAR(36)     NOT NULL,
    dms_ref       VARCHAR(100) NULL,
    vin           CHAR(17)     NULL,
    registration  VARCHAR(15)  NOT NULL,
    make          VARCHAR(50)  NULL,
    model         VARCHAR(50)  NULL,
    created_at    DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_vehicle_dms (tenant_id, dms_ref),
    KEY ix_vehicle_registration (tenant_id, registration),
    KEY ix_vehicle_vin (tenant_id, vin),
    KEY ix_vehicle_customer (customer_id),
    CONSTRAINT fk_vehicle_tenant   FOREIGN KEY (tenant_id)   REFERENCES tenant(id),
    CONSTRAINT fk_vehicle_customer FOREIGN KEY (customer_id) REFERENCES customer(id)
) ENGINE=InnoDB;

-- -----------------------------------------------------------------------------
-- Booking: appointments and the time-grid ledger
-- -----------------------------------------------------------------------------

-- appointment.id is generated by the Booking Service and doubles as the
-- customer-facing reference; there is no separate reference column.
--
-- Idempotency is the natural booking key: a vehicle, a customer, a start time
-- and a kind identify one appointment, enforced by uq_appointment_booking. Its
-- purpose is that two identical requests (a retry after a lost response, or a
-- concurrent double submit) produce one appointment. The INSERT is the first
-- statement of the booking transaction, so a concurrent retry blocks on that
-- unique index until the first attempt commits (duplicate key -> return the
-- existing appointment, or 409 DUPLICATE_BOOKING if the rest of the request
-- differs) or rolls back (the retry proceeds). A retry can therefore never
-- create a second appointment, and no idempotency header is needed.
--
-- The key covers every status, so a cancelled or no-show appointment also
-- blocks a new booking of the exact same minute for the same vehicle, customer
-- and kind; the client receives 409 DUPLICATE_BOOKING with that appointment and
-- picks another time. Reinstating a cancelled appointment is a later feature.
CREATE TABLE appointment (
    id                    CHAR(36)    NOT NULL DEFAULT (UUID()), -- generated by the service (so it can be referenced in the same tx); also the reference
    tenant_id             CHAR(36)    NOT NULL,
    dealership_id         CHAR(36)    NOT NULL,
    customer_id           CHAR(36)    NOT NULL,
    vehicle_id            CHAR(36)    NOT NULL,
    service_type_id       CHAR(36)    NOT NULL,
    kind                  VARCHAR(20) NOT NULL DEFAULT 'DROP_OFF',   -- domain.AppointmentKind: WAITER|DROP_OFF|LOANER
    start_at              DATETIME(6) NOT NULL,                 -- exact arrival / work start promised to customer
    end_at                DATETIME(6) NOT NULL,                 -- exact promised ready time (no buffers)
    service_bay_id        CHAR(36)    NOT NULL,                 -- denormalised from claimed cells (R3)
    technician_id         CHAR(36)    NOT NULL,
    status                VARCHAR(20) NOT NULL DEFAULT 'CONFIRMED',  -- domain.AppointmentStatus: CONFIRMED|CHECKED_IN|IN_PROGRESS|COMPLETED|CANCELLED|NO_SHOW
    customer_requests     JSON        NOT NULL DEFAULT (JSON_OBJECT()),
    notes                 TEXT        NULL,
    channel               VARCHAR(20) NOT NULL,                      -- domain.Channel: CUSTOMER_WEB|CUSTOMER_APP|ADVISOR_CONSOLE|CALL_CENTRE|INTEGRATION
    version               INT UNSIGNED NOT NULL DEFAULT 1,           -- optimistic lock; the application writes version = version + 1 WHERE version = ?
    created_at            DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at            DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_appointment_booking (vehicle_id, customer_id, start_at, kind),  -- natural idempotency key, see comment above
    KEY ix_appointment_day      (dealership_id, start_at),
    KEY ix_appointment_customer (customer_id, start_at),
    KEY ix_appointment_vehicle  (vehicle_id, start_at),
    CONSTRAINT fk_appt_tenant     FOREIGN KEY (tenant_id)       REFERENCES tenant(id),
    CONSTRAINT fk_appt_dealership FOREIGN KEY (dealership_id)   REFERENCES dealership(id),
    CONSTRAINT fk_appt_customer   FOREIGN KEY (customer_id)     REFERENCES customer(id),
    CONSTRAINT fk_appt_vehicle    FOREIGN KEY (vehicle_id)      REFERENCES vehicle(id),
    CONSTRAINT fk_appt_service    FOREIGN KEY (service_type_id) REFERENCES service_type(id),
    CONSTRAINT fk_appt_bay        FOREIGN KEY (service_bay_id)  REFERENCES service_bay(id),
    CONSTRAINT fk_appt_technician FOREIGN KEY (technician_id)   REFERENCES technician(id),
    CONSTRAINT ck_appt_order CHECK (start_at < end_at)
) ENGINE=InnoDB;

-- The capacity ledger. One row per resource per grid cell inside working time.
-- Generated ahead by the materialiser; a missing row = resource not available.
CREATE TABLE time_slot (
    dealership_id      CHAR(36)    NOT NULL,
    resource_kind      VARCHAR(20) NOT NULL,                       -- domain.ResourceKind: BAY|TECHNICIAN
    resource_id        CHAR(36)    NOT NULL,
    slot_start         DATETIME    NOT NULL,                    -- on the grid, UTC
    status             VARCHAR(20) NOT NULL DEFAULT 'FREE',        -- domain.CellStatus: FREE|BOOKED (phase 2: BLOCKED, HELD)
    appointment_id     CHAR(36)    NULL,                        -- while BOOKED
    version            INT UNSIGNED NOT NULL DEFAULT 0,         -- flip counter for debugging; bumped on every claim and release
    PRIMARY KEY (resource_kind, resource_id, slot_start),       -- uniqueness guarantee; lock order within a statement
    KEY ix_slot_day  (dealership_id, slot_start, status),       -- availability reads
    KEY ix_slot_appt (appointment_id),
    CONSTRAINT fk_slot_dealership  FOREIGN KEY (dealership_id)  REFERENCES dealership(id),
    CONSTRAINT fk_slot_appointment FOREIGN KEY (appointment_id) REFERENCES appointment(id),
    CONSTRAINT ck_slot_owner CHECK (
        (status = 'FREE'   AND appointment_id IS NULL) OR
        (status = 'BOOKED' AND appointment_id IS NOT NULL))
) ENGINE=InnoDB;

-- -----------------------------------------------------------------------------
-- How capacity is claimed and released (application code, store/ledger.go)
--
-- CLAIM, per resource, bay first then technician, inside the booking tx:
--   SELECT slot_start, status, appointment_id FROM time_slot
--    WHERE resource_kind = ? AND resource_id = ? AND slot_start BETWEEN ? AND ?
--    ORDER BY slot_start FOR UPDATE;
--   -- in code: fewer rows than cells needed -> BAY_NOT_OPEN / TECHNICIAN_NOT_WORKING
--   --          any row not FREE and not owned by the appointment being
--   --          rescheduled                 -> SLOT_TAKEN (with the cells)
--   UPDATE time_slot SET status = 'BOOKED', appointment_id = ?, version = version + 1
--    WHERE resource_kind = ? AND resource_id = ? AND slot_start BETWEEN ? AND ?;
--
-- Why this is race-free without explicit locks or an isolation-level
-- requirement: SELECT ... FOR UPDATE is a current read. It takes an exclusive
-- lock on each matching row and returns the latest committed version, not the
-- transaction's snapshot. Two claimers for overlapping cells serialise on the
-- first shared row; the loser resumes after the winner commits, sees BOOKED,
-- and stops before writing. A cell row that does not exist cannot be claimed,
-- so working time is enforced by row existence.
--
-- Lock order: within one statement InnoDB locks rows in primary-key order
-- (resource_kind, resource_id, slot_start). Every caller claims the bay
-- before the technician. Plain claims therefore never deadlock each other.
--
-- Known deadlock: a reschedule locks its NEW cells, then frees its OLD cells
-- through ix_slot_appt. A booking that overlaps both runs can hold the old
-- cells while waiting for the new ones, a cycle InnoDB breaks with error
-- 1213. The application retries the victim once. The ledger is never left
-- inconsistent because both sides are single transactions.
--
-- RELEASE (cancel, no-show, complete, reschedule):
--   SELECT ... FROM time_slot WHERE appointment_id = ? FOR UPDATE;
--   -- in code: keep cells inside the new runs (reschedule) or starting before
--   --          keep_before (complete); everything else is released
--   UPDATE time_slot SET status = 'FREE', appointment_id = NULL, version = version + 1
--    WHERE appointment_id = ? AND slot_start IN (...);
--
-- MATERIALISE: for each resource and local date the application resolves the
-- working intervals (rota_assignment rows valid on that date for its weekday,
-- else the opening window), intersects them with the dealership opening window, converts to UTC in code, generates
-- the cell starts and INSERT IGNOREs them in one batch per resource-day.
-- Re-running is harmless; FREE cells outside the intervals are deleted.
-- -----------------------------------------------------------------------------

-- -----------------------------------------------------------------------------
-- Audit and outbox
-- -----------------------------------------------------------------------------
CREATE TABLE appointment_event (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    appointment_id  CHAR(36)     NOT NULL,
    event_type      VARCHAR(50)  NOT NULL,
    payload         JSON         NOT NULL DEFAULT (JSON_OBJECT()),
    actor           VARCHAR(100) NOT NULL,
    channel         VARCHAR(20)  NULL,                          -- domain.Channel
    occurred_at     DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY ix_event_appointment (appointment_id, occurred_at),
    CONSTRAINT fk_event_appointment FOREIGN KEY (appointment_id) REFERENCES appointment(id)
) ENGINE=InnoDB;

CREATE TABLE outbox (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    event_id        CHAR(36)     NOT NULL DEFAULT (UUID()),
    aggregate_type  VARCHAR(50)  NOT NULL,
    aggregate_id    CHAR(36)     NOT NULL,
    event_type      VARCHAR(50)  NOT NULL,
    payload         JSON         NOT NULL,
    created_at      DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    published_at    DATETIME(6)  NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_outbox_event (event_id),
    KEY ix_outbox_unpublished (published_at, id)
) ENGINE=InnoDB;
