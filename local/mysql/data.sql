-- Fixture data, loaded after schema.sql whenever the MySQL container starts on an empty
-- database (02-data.sql). Every `make test-integration` run starts from it. Rows use fixed ids
-- so scenarios can name them; test/acceptance/fixture.go mirrors them. Rows are added here by
-- the feature that needs them.
--
-- Dates are relative to CURRENT_DATE (UTC, the server's time zone) so the fixture never goes
-- stale and no fake clock is needed. Ledger cells cover 07:00Z to 17:00Z, which is 08:00 to
-- 17:00 Europe/London in both summer and winter time.

SET NAMES utf8mb4;
SET time_zone = '+00:00';

-- Tenancy and dealerships ---------------------------------------------------------------
INSERT INTO tenant (id, name) VALUES
  ('a0000000-0000-4000-8000-000000000001', 'Northern Motor Group');

INSERT INTO dealership (id, tenant_id, code, name, timezone, slot_minutes, booking_horizon_days, materialise_horizon_days) VALUES
  ('d0000000-0000-4000-8000-000000000001', 'a0000000-0000-4000-8000-000000000001', 'MAN01', 'Manchester Central', 'Europe/London', 15, 28, 60),
  -- LDS01 has no resources and no cells: it exists so a booking key can be submitted at a
  -- second dealership (idempotency.feature).
  ('d0000000-0000-4000-8000-000000000002', 'a0000000-0000-4000-8000-000000000001', 'LDS01', 'Leeds North', 'Europe/London', 15, 28, 60);

-- Resources -------------------------------------------------------------------------------
INSERT INTO service_bay (id, tenant_id, dealership_id, name, type, status) VALUES
  ('b0000000-0000-4000-8000-000000000001', 'a0000000-0000-4000-8000-000000000001', 'd0000000-0000-4000-8000-000000000001', 'Bay 1', 'GENERAL', 'ACTIVE'),
  ('b0000000-0000-4000-8000-000000000002', 'a0000000-0000-4000-8000-000000000001', 'd0000000-0000-4000-8000-000000000001', 'Bay 2', 'MOT',     'ACTIVE'),
  ('b0000000-0000-4000-8000-000000000003', 'a0000000-0000-4000-8000-000000000001', 'd0000000-0000-4000-8000-000000000001', 'Bay 3', 'EV_HV',   'ACTIVE');

INSERT INTO technician (id, tenant_id, dealership_id, display_name, status) VALUES
  ('e0000000-0000-4000-8000-000000000001', 'a0000000-0000-4000-8000-000000000001', 'd0000000-0000-4000-8000-000000000001', 'Sam',   'ACTIVE'),
  ('e0000000-0000-4000-8000-000000000002', 'a0000000-0000-4000-8000-000000000001', 'd0000000-0000-4000-8000-000000000001', 'Priya', 'ACTIVE'),
  ('e0000000-0000-4000-8000-000000000003', 'a0000000-0000-4000-8000-000000000001', 'd0000000-0000-4000-8000-000000000001', 'Lee',   'ACTIVE');

INSERT INTO skill (id, tenant_id, code, name) VALUES
  ('5c000000-0000-4000-8000-000000000001', 'a0000000-0000-4000-8000-000000000001', 'GENERAL_SERVICE', 'General servicing'),
  ('5c000000-0000-4000-8000-000000000002', 'a0000000-0000-4000-8000-000000000001', 'MOT_TESTER',      'MOT tester'),
  ('5c000000-0000-4000-8000-000000000003', 'a0000000-0000-4000-8000-000000000001', 'EV_HV_L3',        'EV high-voltage level 3'),
  ('5c000000-0000-4000-8000-000000000004', 'a0000000-0000-4000-8000-000000000001', 'AIRCON_FGAS',     'Air conditioning F-gas');

INSERT INTO technician_skill (technician_id, skill_id, valid_from, valid_to) VALUES
  ('e0000000-0000-4000-8000-000000000001', '5c000000-0000-4000-8000-000000000001', CURRENT_DATE - INTERVAL 1 YEAR, NULL),  -- Sam: GENERAL_SERVICE
  ('e0000000-0000-4000-8000-000000000001', '5c000000-0000-4000-8000-000000000003', CURRENT_DATE - INTERVAL 1 YEAR, NULL),  -- Sam: EV_HV_L3
  ('e0000000-0000-4000-8000-000000000002', '5c000000-0000-4000-8000-000000000001', CURRENT_DATE - INTERVAL 1 YEAR, NULL),  -- Priya: GENERAL_SERVICE
  ('e0000000-0000-4000-8000-000000000002', '5c000000-0000-4000-8000-000000000002', CURRENT_DATE - INTERVAL 1 YEAR, NULL),  -- Priya: MOT_TESTER
  ('e0000000-0000-4000-8000-000000000003', '5c000000-0000-4000-8000-000000000001', CURRENT_DATE - INTERVAL 1 YEAR, NULL),  -- Lee: GENERAL_SERVICE
  -- Lee's F-gas certificate expired yesterday, so nobody qualifies for AIRCON.
  ('e0000000-0000-4000-8000-000000000003', '5c000000-0000-4000-8000-000000000004', CURRENT_DATE - INTERVAL 2 YEAR, CURRENT_DATE - INTERVAL 1 DAY);

-- Service catalogue -----------------------------------------------------------------------
INSERT INTO service_type (id, tenant_id, code, name, duration_minutes, buffer_before_minutes, buffer_after_minutes, online_bookable, active) VALUES
  ('5e000000-0000-4000-8000-000000000001', 'a0000000-0000-4000-8000-000000000001', 'INTERIM',    'Interim Service',         45,  0,  0, TRUE,  TRUE),
  ('5e000000-0000-4000-8000-000000000002', 'a0000000-0000-4000-8000-000000000001', 'MAJOR',      'Major Service',           90, 15, 15, TRUE,  TRUE),
  ('5e000000-0000-4000-8000-000000000003', 'a0000000-0000-4000-8000-000000000001', 'MOT',        'MOT Test',                45,  0,  0, TRUE,  TRUE),
  ('5e000000-0000-4000-8000-000000000004', 'a0000000-0000-4000-8000-000000000001', 'EV_MAJOR',   'EV Major Service',       120, 15, 15, TRUE,  TRUE),
  ('5e000000-0000-4000-8000-000000000005', 'a0000000-0000-4000-8000-000000000001', 'EV_CHECK',   'EV Battery Health Check', 30,  0,  0, TRUE,  TRUE),
  ('5e000000-0000-4000-8000-000000000006', 'a0000000-0000-4000-8000-000000000001', 'DIAGNOSTIC', 'Diagnostic Investigation', 50, 0,  0, FALSE, TRUE),
  ('5e000000-0000-4000-8000-000000000007', 'a0000000-0000-4000-8000-000000000001', 'AIRCON',     'Air Conditioning Service', 30, 0,  0, TRUE,  TRUE);

INSERT INTO service_type_bay_type (service_type_id, bay_type) VALUES
  ('5e000000-0000-4000-8000-000000000001', 'GENERAL'),  -- INTERIM
  ('5e000000-0000-4000-8000-000000000001', 'MOT'),
  ('5e000000-0000-4000-8000-000000000002', 'GENERAL'),  -- MAJOR
  ('5e000000-0000-4000-8000-000000000003', 'MOT'),      -- MOT
  ('5e000000-0000-4000-8000-000000000004', 'EV_HV'),    -- EV_MAJOR
  ('5e000000-0000-4000-8000-000000000005', 'GENERAL'),  -- EV_CHECK
  ('5e000000-0000-4000-8000-000000000005', 'EV_HV'),
  ('5e000000-0000-4000-8000-000000000006', 'GENERAL'),  -- DIAGNOSTIC
  ('5e000000-0000-4000-8000-000000000007', 'GENERAL');  -- AIRCON

INSERT INTO service_type_skill (service_type_id, skill_id) VALUES
  ('5e000000-0000-4000-8000-000000000001', '5c000000-0000-4000-8000-000000000001'),  -- INTERIM: GENERAL_SERVICE
  ('5e000000-0000-4000-8000-000000000002', '5c000000-0000-4000-8000-000000000001'),  -- MAJOR: GENERAL_SERVICE
  ('5e000000-0000-4000-8000-000000000003', '5c000000-0000-4000-8000-000000000002'),  -- MOT: MOT_TESTER
  ('5e000000-0000-4000-8000-000000000004', '5c000000-0000-4000-8000-000000000003'),  -- EV_MAJOR: EV_HV_L3
  ('5e000000-0000-4000-8000-000000000005', '5c000000-0000-4000-8000-000000000003'),  -- EV_CHECK: EV_HV_L3
  ('5e000000-0000-4000-8000-000000000006', '5c000000-0000-4000-8000-000000000001'),  -- DIAGNOSTIC: GENERAL_SERVICE
  ('5e000000-0000-4000-8000-000000000007', '5c000000-0000-4000-8000-000000000004');  -- AIRCON: AIRCON_FGAS

-- Customers and vehicles ------------------------------------------------------------------
INSERT INTO customer (id, tenant_id, display_name, email) VALUES
  ('c0000000-0000-4000-8000-000000000001', 'a0000000-0000-4000-8000-000000000001', 'Alice', 'alice@example.com'),
  ('c0000000-0000-4000-8000-000000000002', 'a0000000-0000-4000-8000-000000000001', 'Bob',   'bob@example.com');

INSERT INTO vehicle (id, tenant_id, customer_id, registration, make, model) VALUES
  ('f0000000-0000-4000-8000-000000000001', 'a0000000-0000-4000-8000-000000000001', 'c0000000-0000-4000-8000-000000000001', 'YK21 ABC', 'Volkswagen', 'ID.4'),
  ('f0000000-0000-4000-8000-000000000002', 'a0000000-0000-4000-8000-000000000001', 'c0000000-0000-4000-8000-000000000002', 'BD70 XYZ', 'Ford',       'Focus');

-- Customer 01 to Customer 20, each with vehicle TEST 01 to TEST 20, for the race scenarios.
-- Ids end in 101 to 120.
INSERT INTO customer (id, tenant_id, display_name)
WITH RECURSIVE n (i) AS (SELECT 101 UNION ALL SELECT i + 1 FROM n WHERE i < 120)
SELECT CONCAT('c0000000-0000-4000-8000-000000000', i), 'a0000000-0000-4000-8000-000000000001',
       CONCAT('Customer ', LPAD(i - 100, 2, '0'))
  FROM n;

INSERT INTO vehicle (id, tenant_id, customer_id, registration, make, model)
WITH RECURSIVE n (i) AS (SELECT 101 UNION ALL SELECT i + 1 FROM n WHERE i < 120)
SELECT CONCAT('f0000000-0000-4000-8000-000000000', i), 'a0000000-0000-4000-8000-000000000001',
       CONCAT('c0000000-0000-4000-8000-000000000', i), CONCAT('TEST ', LPAD(i - 100, 2, '0')),
       'Toyota', 'Yaris'
  FROM n;

-- The ledger --------------------------------------------------------------------------------
-- FREE cells for every bay and technician of MAN01, for CURRENT_DATE to CURRENT_DATE + 13,
-- cell starts 07:00Z to 16:45Z on the 15-minute grid: 6 resources x 14 days x 40 cells = 3 360.
-- This stands in for the materialiser, which is not part of this release.
INSERT INTO time_slot (dealership_id, resource_kind, resource_id, slot_start, status)
WITH RECURSIVE
  days (d) AS (SELECT 0 UNION ALL SELECT d + 1 FROM days WHERE d < 13),
  cells (k) AS (SELECT 0 UNION ALL SELECT k + 1 FROM cells WHERE k < 39),
  resources (kind, id) AS (
    SELECT 'BAY', id FROM service_bay WHERE dealership_id = 'd0000000-0000-4000-8000-000000000001'
    UNION ALL
    SELECT 'TECHNICIAN', id FROM technician WHERE dealership_id = 'd0000000-0000-4000-8000-000000000001')
SELECT 'd0000000-0000-4000-8000-000000000001', r.kind, r.id,
       TIMESTAMP(CURRENT_DATE) + INTERVAL d.d DAY + INTERVAL 7 HOUR + INTERVAL 15 * c.k MINUTE,
       'FREE'
  FROM resources r CROSS JOIN days d CROSS JOIN cells c;
