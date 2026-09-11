-- PG port of 00149_identity_sequence_resync.sql
-- PG repair for identity-sequence desync (A7 tail proof): explicit-id seeds
-- in 00027 (driver=5) and 00064 (org_admin=6) never advance roles_id_seq,
-- so later generated ids collided — 00073's customer insert was silently
-- skipped by its bare ON CONFLICT DO NOTHING, and 00137 failed hard on
-- roles_pkey (arbiter was (name), collision was (id)). Insert-then-resync
-- order is deliberate: safe on fresh chains and on DBs with manual rows.
-- +goose Up
INSERT INTO roles (name, description) VALUES ('customer','Shipper portal customer') ON CONFLICT (name) DO NOTHING;
SELECT setval('roles_id_seq', (SELECT COALESCE(MAX(id), 0) FROM roles), true);
SELECT setval('permissions_id_seq', (SELECT COALESCE(MAX(id), 0) FROM permissions), true);

-- +goose Down
-- Documented no-op (00136/00141 convention): sequence position is never wound
-- back, and the backfilled customer row is left in place — Down only needs to
-- unwind schema, of which this migration has none.
SELECT 1;
