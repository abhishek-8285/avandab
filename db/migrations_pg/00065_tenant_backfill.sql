-- PG port of 00065_tenant_backfill.sql | status: PORTABLE | flags: none
-- +goose Up
-- 00065: Tenant backfill — normalize empty tenant_id to default '1' (idempotent)
-- Fixes legacy rows where tenant_id was NULL/'kept empty before multi-tenancy hardening.
-- Does NOT overwrite existing '1' rows (single-tenant default). Multi-tenant deployments
-- should replace '1' with real tenant after audit: SELECT tenant_id, COUNT(*) GROUP BY tenant_id.

UPDATE invoices SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE vehicles SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE drivers SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE trips SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE bookings SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE payments SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE routes SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE dispatches SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE fuel_prices SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE telemetry_devices SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE telemetry_raw_events SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE geofences SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE trip_detentions SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE invoice_line_items SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE pnl_daily SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE ops_alerts SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE founder_signals SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
UPDATE founder_audit SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';
-- +goose Down
-- No safe rollback — empty tenant_id cannot be distinguished from legitimate '1' after backfill.
SELECT 1;
