-- PG port of 00107_cleanup_test_tenants.sql | status: PORTABLE | flags: none
-- +goose Up
-- 00107 Prod cleanup — remove 29 test-only tenants seeded in 00103.
-- Tests must seed via test/helpers.go NewTestDB (already does) or per-test helper.
DELETE FROM tenants WHERE id IN ('7','9','other-tenant','another-tenant','tenant-1','tenant-2','tenant-7','tenant-9','tenant-999','tenant-a','tenant-b','tenant-A','tenant-B','tenant-zz','tenant-seq','tenant-cap','tenant-dn','tenant-ledger','tenant-val','tenant-fmt','tenant-loop','tn-b','tn-kpi','tenant-c','tenant-d','tenant-forged','tenant-42','test-tenant');
-- +goose Down
-- Re-seed test tenants for rollback.
INSERT INTO tenants (id, name, slug) VALUES ('2','2','2') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('7','7','7') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('9','9','9') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('other-tenant','other-tenant','other-tenant') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('another-tenant','another-tenant','another-tenant') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-1','tenant-1','tenant-1') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-2','tenant-2','tenant-2') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-7','tenant-7','tenant-7') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-9','tenant-9','tenant-9') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-999','tenant-999','tenant-999') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-a','tenant-a','tenant-a') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-b','tenant-b','tenant-b') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-A','tenant-A','tenant-A') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-B','tenant-B','tenant-B') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-zz','tenant-zz','tenant-zz') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-seq','tenant-seq','tenant-seq') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-cap','tenant-cap','tenant-cap') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-dn','tenant-dn','tenant-dn') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-ledger','tenant-ledger','tenant-ledger') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-val','tenant-val','tenant-val') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-fmt','tenant-fmt','tenant-fmt') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-loop','tenant-loop','tenant-loop') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tn-b','tn-b','tn-b') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tn-kpi','tn-kpi','tn-kpi') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-c','tenant-c','tenant-c') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-d','tenant-d','tenant-d') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-forged','tenant-forged','tenant-forged') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('tenant-42','tenant-42','tenant-42') ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, name, slug) VALUES ('test-tenant','test-tenant','test-tenant') ON CONFLICT DO NOTHING;
