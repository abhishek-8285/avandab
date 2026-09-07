-- Cutover data cleanup: run against the SQLITE file BEFORE sqlite2pg.
-- Safe: only touches provably-broken rows (verified on 2026-09-07:
-- 9 mapped + 52 orphans deleted, 0 violations after, counts intact).
-- Usage: sqlite3 transport.db < scripts/cutover-data-cleanup.sql
-- Always back up first (pg-cutover.sh does this automatically).

-- 1. Dirty role names in INTEGER column -> canonical role ids.
-- Trigger sync_user_role_on_update rebuilds user_roles automatically.
UPDATE users SET role_id = 2 WHERE role_id = 'dispatcher';
UPDATE users SET role_id = 5 WHERE role_id = 'driver';
UPDATE users SET role_id = 6 WHERE role_id = 'org_admin';

-- 2. Orphan rows (parents already gone; FK-off deletes left them behind).
DELETE FROM sessions            WHERE user_id NOT IN (SELECT id FROM users);
DELETE FROM audit_logs          WHERE user_id NOT IN (SELECT id FROM users);
DELETE FROM telemetry_snapshots WHERE trip_id IS NOT NULL AND trip_id NOT IN (SELECT id FROM trips);
DELETE FROM engine_state        WHERE vehicle_id NOT IN (SELECT id FROM vehicles);

-- 3. Verify: must print zero rows.
PRAGMA foreign_key_check;
