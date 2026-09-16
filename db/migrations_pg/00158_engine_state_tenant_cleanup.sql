-- +goose Up
-- 00158: remove orphaned geofence engine state before strict tenant persistence
-- PG port of 00158_engine_state_tenant_cleanup.sql
-- Engine state is derived and rebuildable; assigning orphaned rows to a tenant
-- would risk cross-tenant state contamination.
DELETE FROM engine_state
WHERE tenant_id IS NULL
   OR tenant_id = ''
   OR NOT EXISTS (
       SELECT 1 FROM tenants
       WHERE tenants.id = engine_state.tenant_id
   );

-- +goose Down
-- No-op: deleted engine state is ephemeral and cannot be reconstructed safely.
