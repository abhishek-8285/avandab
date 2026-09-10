-- +goose Up
-- 00139: High-scale performance indexes for session auth, list KPI counts, and audit logs
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions(token_hash);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_vehicles_tenant_status ON vehicles(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_drivers_tenant_status ON drivers(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_invoices_tenant_payment_status ON invoices(tenant_id, payment_status);
CREATE INDEX IF NOT EXISTS idx_payments_tenant_date ON payments(tenant_id, payment_date);
CREATE INDEX IF NOT EXISTS idx_audit_logs_record ON audit_logs(table_name, record_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_audit_logs_created_at;
DROP INDEX IF EXISTS idx_audit_logs_record;
DROP INDEX IF EXISTS idx_payments_tenant_date;
DROP INDEX IF EXISTS idx_invoices_tenant_payment_status;
DROP INDEX IF EXISTS idx_drivers_tenant_status;
DROP INDEX IF EXISTS idx_vehicles_tenant_status;
DROP INDEX IF EXISTS idx_sessions_expires_at;
DROP INDEX IF EXISTS idx_sessions_token_hash;
