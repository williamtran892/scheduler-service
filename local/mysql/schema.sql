-- Scheduler schema for MySQL 8.4.
--
-- Executed by the MySQL container on every start (docker-compose.yml mounts it as
-- 01-schema.sql). This file is the only place tables are created; the application never
-- creates or alters them. Tables are added here by the feature that needs them.
--
-- Conventions (docs/design/IMPLEMENTATION_NOTES.md sections 0 and 5; target schema in docs/design/schema.sql):
--   * No stored procedures, no triggers.
--   * No ENUM columns: VARCHAR(20) NOT NULL, allowed values are Go constants (deviation D3).
--   * Every DATETIME is UTC; the server and every session run at time_zone '+00:00'.

SET NAMES utf8mb4;
