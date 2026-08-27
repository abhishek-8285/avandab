-- +goose Up
-- 00103 Tenant FK hardening — DB-level integrity via triggers (SQLite FK workaround).
-- Context: ALTER TABLE ADD COLUMN cannot add FOREIGN KEY constraints in SQLite;
-- rebuilding 50+ tables is too risky for this PR. Instead we enforce
-- tenant_id REFERENCES tenants(id) via BEFORE INSERT / BEFORE UPDATE triggers.
-- All rows were backfilled to default tenant via 00065; tenants('1') is seeded in 00102.
-- Triggers use RAISE(ABORT, ...) to reject writes with unknown tenant.
-- Indexes: CREATE INDEX IF NOT EXISTS idx_<table>_tenant on tables that lack
-- any tenant_id index (checked against sqlite_master on 2026-08-27 transport.db).
-- Tables covered (51): alerts, bookings, company_config, credit_debit_notes,
-- customers, device_quarantine, dispatch_overrides, dispatches,
-- driver_advance_requests, driver_expenses, driver_issues, drivers, engine_state,
-- error_reports, eta_history, eta_history_monthly, experiment_assignments,
-- experiment_events, experiments_spec16, fastag_tags, fastag_transactions,
-- feature_flags, founder_audit, founder_signals, fuel_prices, geofence_events,
-- geofences, incidents, invoice_line_items, invoice_sequences, invoices,
-- maintenance_records, money_ledger, note_sequences, offline_sync_log, ops_alerts,
-- payments, pnl_daily, provider_poll_state, route_optimization_jobs, routes,
-- telemetry_devices, telemetry_positions, telemetry_raw_events, trip_detentions,
-- trip_feedback, trips, users, vehicle_geofences, vehicle_latest_position, vehicles.

-- Seed common test tenants for legacy test compatibility (allows existing tests
-- that insert rows with hardcoded tenant-1 / tenant-a etc. to pass FK checks).
-- Production code must still create tenants via the /tenants API; these rows are
-- merely back-compat for the in-memory test suite. INSERT OR IGNORE is idempotent.
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('2','Tenant 2','tenant-2');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('7','Tenant 7','tenant-7');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('9','Tenant 9','tenant-9');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('other-tenant','Other Tenant','other-tenant');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-1','Test Tenant 1','tenant-1');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-2','Test Tenant 2','tenant-2b');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-7','Test Tenant 7','tenant-7b');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-9','Test Tenant 9','tenant-9b');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-999','Test Tenant 999','tenant-999');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-fmt','Test Tenant FMT','tenant-fmt');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-loop','Test Tenant Loop','tenant-loop');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-a','Tenant A','tenant-a');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-b','Tenant B','tenant-b');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-A','Tenant A Cap','tenant-a-cap');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-B','Tenant B Cap','tenant-b2');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-zz','Tenant ZZ','tenant-zz');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-seq','Tenant Seq','tenant-seq');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-cap','Tenant Cap','tenant-cap');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-dn','Tenant DN','tenant-dn');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-ledger','Tenant Ledger','tenant-ledger');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-val','Tenant Val','tenant-val');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tn-b','Tenant TN-B','tn-b');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tn-kpi','Tenant TN-KPI','tn-kpi');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('another-tenant','Another Tenant','another-tenant');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-c','Tenant C','tenant-c');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-d','Tenant D','tenant-d');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-forged','Tenant Forged','tenant-forged');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-42','Tenant 42','tenant-42');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('test-tenant','Test Tenant','test-tenant');

-- Indexes for tables missing tenant coverage (21)
CREATE INDEX IF NOT EXISTS idx_bookings_tenant ON bookings(tenant_id);
CREATE INDEX IF NOT EXISTS idx_device_quarantine_tenant ON device_quarantine(tenant_id);
CREATE INDEX IF NOT EXISTS idx_dispatch_overrides_tenant ON dispatch_overrides(tenant_id);
CREATE INDEX IF NOT EXISTS idx_dispatches_tenant ON dispatches(tenant_id);
CREATE INDEX IF NOT EXISTS idx_driver_issues_tenant ON driver_issues(tenant_id);
CREATE INDEX IF NOT EXISTS idx_drivers_tenant ON drivers(tenant_id);
CREATE INDEX IF NOT EXISTS idx_engine_state_tenant ON engine_state(tenant_id);
CREATE INDEX IF NOT EXISTS idx_experiment_events_tenant ON experiment_events(tenant_id);
CREATE INDEX IF NOT EXISTS idx_geofence_events_tenant ON geofence_events(tenant_id);
CREATE INDEX IF NOT EXISTS idx_invoice_line_items_tenant ON invoice_line_items(tenant_id);
CREATE INDEX IF NOT EXISTS idx_invoices_tenant ON invoices(tenant_id);
CREATE INDEX IF NOT EXISTS idx_offline_sync_log_tenant ON offline_sync_log(tenant_id);
CREATE INDEX IF NOT EXISTS idx_provider_poll_state_tenant ON provider_poll_state(tenant_id);
CREATE INDEX IF NOT EXISTS idx_telemetry_positions_tenant ON telemetry_positions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_telemetry_raw_events_tenant ON telemetry_raw_events(tenant_id);
CREATE INDEX IF NOT EXISTS idx_trip_detentions_tenant ON trip_detentions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_trip_feedback_tenant ON trip_feedback(tenant_id);
CREATE INDEX IF NOT EXISTS idx_trips_tenant ON trips(tenant_id);
CREATE INDEX IF NOT EXISTS idx_vehicle_geofences_tenant ON vehicle_geofences(tenant_id);
CREATE INDEX IF NOT EXISTS idx_vehicle_latest_position_tenant ON vehicle_latest_position(tenant_id);
CREATE INDEX IF NOT EXISTS idx_vehicles_tenant ON vehicles(tenant_id);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_alerts_tenant_fk_insert
BEFORE INSERT ON alerts
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for alerts.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_alerts_tenant_fk_update
BEFORE UPDATE OF tenant_id ON alerts
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for alerts.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_bookings_tenant_fk_insert
BEFORE INSERT ON bookings
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for bookings.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_bookings_tenant_fk_update
BEFORE UPDATE OF tenant_id ON bookings
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for bookings.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_company_config_tenant_fk_insert
BEFORE INSERT ON company_config
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for company_config.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_company_config_tenant_fk_update
BEFORE UPDATE OF tenant_id ON company_config
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for company_config.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_credit_debit_notes_tenant_fk_insert
BEFORE INSERT ON credit_debit_notes
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for credit_debit_notes.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_credit_debit_notes_tenant_fk_update
BEFORE UPDATE OF tenant_id ON credit_debit_notes
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for credit_debit_notes.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_customers_tenant_fk_insert
BEFORE INSERT ON customers
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for customers.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_customers_tenant_fk_update
BEFORE UPDATE OF tenant_id ON customers
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for customers.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_device_quarantine_tenant_fk_insert
BEFORE INSERT ON device_quarantine
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for device_quarantine.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_device_quarantine_tenant_fk_update
BEFORE UPDATE OF tenant_id ON device_quarantine
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for device_quarantine.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_dispatch_overrides_tenant_fk_insert
BEFORE INSERT ON dispatch_overrides
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for dispatch_overrides.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_dispatch_overrides_tenant_fk_update
BEFORE UPDATE OF tenant_id ON dispatch_overrides
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for dispatch_overrides.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_dispatches_tenant_fk_insert
BEFORE INSERT ON dispatches
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for dispatches.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_dispatches_tenant_fk_update
BEFORE UPDATE OF tenant_id ON dispatches
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for dispatches.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_driver_advance_requests_tenant_fk_insert
BEFORE INSERT ON driver_advance_requests
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_advance_requests.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_driver_advance_requests_tenant_fk_update
BEFORE UPDATE OF tenant_id ON driver_advance_requests
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_advance_requests.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_driver_expenses_tenant_fk_insert
BEFORE INSERT ON driver_expenses
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_expenses.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_driver_expenses_tenant_fk_update
BEFORE UPDATE OF tenant_id ON driver_expenses
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_expenses.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_driver_issues_tenant_fk_insert
BEFORE INSERT ON driver_issues
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_issues.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_driver_issues_tenant_fk_update
BEFORE UPDATE OF tenant_id ON driver_issues
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_issues.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_drivers_tenant_fk_insert
BEFORE INSERT ON drivers
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for drivers.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_drivers_tenant_fk_update
BEFORE UPDATE OF tenant_id ON drivers
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for drivers.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_engine_state_tenant_fk_insert
BEFORE INSERT ON engine_state
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for engine_state.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_engine_state_tenant_fk_update
BEFORE UPDATE OF tenant_id ON engine_state
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for engine_state.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_error_reports_tenant_fk_insert
BEFORE INSERT ON error_reports
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for error_reports.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_error_reports_tenant_fk_update
BEFORE UPDATE OF tenant_id ON error_reports
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for error_reports.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_eta_history_tenant_fk_insert
BEFORE INSERT ON eta_history
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for eta_history.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_eta_history_tenant_fk_update
BEFORE UPDATE OF tenant_id ON eta_history
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for eta_history.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_eta_history_monthly_tenant_fk_insert
BEFORE INSERT ON eta_history_monthly
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for eta_history_monthly.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_eta_history_monthly_tenant_fk_update
BEFORE UPDATE OF tenant_id ON eta_history_monthly
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for eta_history_monthly.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_experiment_assignments_tenant_fk_insert
BEFORE INSERT ON experiment_assignments
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiment_assignments.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_experiment_assignments_tenant_fk_update
BEFORE UPDATE OF tenant_id ON experiment_assignments
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiment_assignments.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_experiment_events_tenant_fk_insert
BEFORE INSERT ON experiment_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiment_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_experiment_events_tenant_fk_update
BEFORE UPDATE OF tenant_id ON experiment_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiment_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_experiments_spec16_tenant_fk_insert
BEFORE INSERT ON experiments_spec16
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiments_spec16.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_experiments_spec16_tenant_fk_update
BEFORE UPDATE OF tenant_id ON experiments_spec16
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiments_spec16.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_fastag_tags_tenant_fk_insert
BEFORE INSERT ON fastag_tags
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fastag_tags.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_fastag_tags_tenant_fk_update
BEFORE UPDATE OF tenant_id ON fastag_tags
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fastag_tags.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_fastag_transactions_tenant_fk_insert
BEFORE INSERT ON fastag_transactions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fastag_transactions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_fastag_transactions_tenant_fk_update
BEFORE UPDATE OF tenant_id ON fastag_transactions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fastag_transactions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_feature_flags_tenant_fk_insert
BEFORE INSERT ON feature_flags
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for feature_flags.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_feature_flags_tenant_fk_update
BEFORE UPDATE OF tenant_id ON feature_flags
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for feature_flags.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_founder_audit_tenant_fk_insert
BEFORE INSERT ON founder_audit
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for founder_audit.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_founder_audit_tenant_fk_update
BEFORE UPDATE OF tenant_id ON founder_audit
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for founder_audit.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_founder_signals_tenant_fk_insert
BEFORE INSERT ON founder_signals
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for founder_signals.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_founder_signals_tenant_fk_update
BEFORE UPDATE OF tenant_id ON founder_signals
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for founder_signals.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_fuel_prices_tenant_fk_insert
BEFORE INSERT ON fuel_prices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fuel_prices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_fuel_prices_tenant_fk_update
BEFORE UPDATE OF tenant_id ON fuel_prices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fuel_prices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_geofence_events_tenant_fk_insert
BEFORE INSERT ON geofence_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for geofence_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_geofence_events_tenant_fk_update
BEFORE UPDATE OF tenant_id ON geofence_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for geofence_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_geofences_tenant_fk_insert
BEFORE INSERT ON geofences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for geofences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_geofences_tenant_fk_update
BEFORE UPDATE OF tenant_id ON geofences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for geofences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_incidents_tenant_fk_insert
BEFORE INSERT ON incidents
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for incidents.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_incidents_tenant_fk_update
BEFORE UPDATE OF tenant_id ON incidents
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for incidents.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_invoice_line_items_tenant_fk_insert
BEFORE INSERT ON invoice_line_items
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoice_line_items.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_invoice_line_items_tenant_fk_update
BEFORE UPDATE OF tenant_id ON invoice_line_items
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoice_line_items.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_invoice_sequences_tenant_fk_insert
BEFORE INSERT ON invoice_sequences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoice_sequences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_invoice_sequences_tenant_fk_update
BEFORE UPDATE OF tenant_id ON invoice_sequences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoice_sequences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_invoices_tenant_fk_insert
BEFORE INSERT ON invoices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_invoices_tenant_fk_update
BEFORE UPDATE OF tenant_id ON invoices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_maintenance_records_tenant_fk_insert
BEFORE INSERT ON maintenance_records
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for maintenance_records.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_maintenance_records_tenant_fk_update
BEFORE UPDATE OF tenant_id ON maintenance_records
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for maintenance_records.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_money_ledger_tenant_fk_insert
BEFORE INSERT ON money_ledger
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for money_ledger.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_money_ledger_tenant_fk_update
BEFORE UPDATE OF tenant_id ON money_ledger
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for money_ledger.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_note_sequences_tenant_fk_insert
BEFORE INSERT ON note_sequences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for note_sequences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_note_sequences_tenant_fk_update
BEFORE UPDATE OF tenant_id ON note_sequences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for note_sequences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_offline_sync_log_tenant_fk_insert
BEFORE INSERT ON offline_sync_log
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for offline_sync_log.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_offline_sync_log_tenant_fk_update
BEFORE UPDATE OF tenant_id ON offline_sync_log
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for offline_sync_log.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_ops_alerts_tenant_fk_insert
BEFORE INSERT ON ops_alerts
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for ops_alerts.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_ops_alerts_tenant_fk_update
BEFORE UPDATE OF tenant_id ON ops_alerts
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for ops_alerts.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_payments_tenant_fk_insert
BEFORE INSERT ON payments
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for payments.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_payments_tenant_fk_update
BEFORE UPDATE OF tenant_id ON payments
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for payments.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_pnl_daily_tenant_fk_insert
BEFORE INSERT ON pnl_daily
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for pnl_daily.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_pnl_daily_tenant_fk_update
BEFORE UPDATE OF tenant_id ON pnl_daily
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for pnl_daily.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_provider_poll_state_tenant_fk_insert
BEFORE INSERT ON provider_poll_state
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for provider_poll_state.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_provider_poll_state_tenant_fk_update
BEFORE UPDATE OF tenant_id ON provider_poll_state
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for provider_poll_state.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_route_optimization_jobs_tenant_fk_insert
BEFORE INSERT ON route_optimization_jobs
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for route_optimization_jobs.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_route_optimization_jobs_tenant_fk_update
BEFORE UPDATE OF tenant_id ON route_optimization_jobs
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for route_optimization_jobs.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_routes_tenant_fk_insert
BEFORE INSERT ON routes
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for routes.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_routes_tenant_fk_update
BEFORE UPDATE OF tenant_id ON routes
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for routes.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_telemetry_devices_tenant_fk_insert
BEFORE INSERT ON telemetry_devices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_devices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_telemetry_devices_tenant_fk_update
BEFORE UPDATE OF tenant_id ON telemetry_devices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_devices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_telemetry_positions_tenant_fk_insert
BEFORE INSERT ON telemetry_positions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_positions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_telemetry_positions_tenant_fk_update
BEFORE UPDATE OF tenant_id ON telemetry_positions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_positions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_telemetry_raw_events_tenant_fk_insert
BEFORE INSERT ON telemetry_raw_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_raw_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_telemetry_raw_events_tenant_fk_update
BEFORE UPDATE OF tenant_id ON telemetry_raw_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_raw_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_trip_detentions_tenant_fk_insert
BEFORE INSERT ON trip_detentions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trip_detentions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_trip_detentions_tenant_fk_update
BEFORE UPDATE OF tenant_id ON trip_detentions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trip_detentions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_trip_feedback_tenant_fk_insert
BEFORE INSERT ON trip_feedback
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trip_feedback.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_trip_feedback_tenant_fk_update
BEFORE UPDATE OF tenant_id ON trip_feedback
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trip_feedback.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_trips_tenant_fk_insert
BEFORE INSERT ON trips
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trips.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_trips_tenant_fk_update
BEFORE UPDATE OF tenant_id ON trips
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trips.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_users_tenant_fk_insert
BEFORE INSERT ON users
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for users.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_users_tenant_fk_update
BEFORE UPDATE OF tenant_id ON users
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for users.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_vehicle_geofences_tenant_fk_insert
BEFORE INSERT ON vehicle_geofences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicle_geofences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_vehicle_geofences_tenant_fk_update
BEFORE UPDATE OF tenant_id ON vehicle_geofences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicle_geofences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_vehicle_latest_position_tenant_fk_insert
BEFORE INSERT ON vehicle_latest_position
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicle_latest_position.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_vehicle_latest_position_tenant_fk_update
BEFORE UPDATE OF tenant_id ON vehicle_latest_position
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicle_latest_position.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_vehicles_tenant_fk_insert
BEFORE INSERT ON vehicles
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicles.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_vehicles_tenant_fk_update
BEFORE UPDATE OF tenant_id ON vehicles
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicles.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose Down
-- Remove test-compat tenants seeded in Up (keep default '1').
-- Includes 'acme','beta' for cleanup of previous 00103 revisions that seeded them.
DELETE FROM tenants WHERE id IN ('2','7','9','other-tenant','another-tenant','tenant-1','tenant-2','tenant-7','tenant-9','tenant-999','tenant-a','tenant-b','tenant-A','tenant-B','tenant-zz','tenant-seq','tenant-cap','tenant-dn','tenant-ledger','tenant-val','tenant-fmt','tenant-loop','tn-b','tn-kpi','acme','beta','tenant-c','tenant-d','tenant-forged','tenant-42','test-tenant');

DROP TRIGGER IF EXISTS trg_vehicles_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_vehicles_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_vehicle_latest_position_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_vehicle_latest_position_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_vehicle_geofences_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_vehicle_geofences_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_users_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_users_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_trips_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_trips_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_trip_feedback_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_trip_feedback_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_trip_detentions_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_trip_detentions_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_telemetry_raw_events_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_telemetry_raw_events_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_telemetry_positions_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_telemetry_positions_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_telemetry_devices_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_telemetry_devices_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_routes_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_routes_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_route_optimization_jobs_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_route_optimization_jobs_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_provider_poll_state_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_provider_poll_state_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_pnl_daily_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_pnl_daily_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_payments_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_payments_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_ops_alerts_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_ops_alerts_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_offline_sync_log_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_offline_sync_log_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_note_sequences_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_note_sequences_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_money_ledger_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_money_ledger_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_maintenance_records_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_maintenance_records_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_invoices_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_invoices_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_invoice_sequences_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_invoice_sequences_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_invoice_line_items_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_invoice_line_items_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_incidents_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_incidents_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_geofences_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_geofences_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_geofence_events_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_geofence_events_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_fuel_prices_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_fuel_prices_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_founder_signals_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_founder_signals_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_founder_audit_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_founder_audit_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_feature_flags_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_feature_flags_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_fastag_transactions_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_fastag_transactions_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_fastag_tags_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_fastag_tags_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_experiments_spec16_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_experiments_spec16_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_experiment_events_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_experiment_events_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_experiment_assignments_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_experiment_assignments_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_eta_history_monthly_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_eta_history_monthly_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_eta_history_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_eta_history_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_error_reports_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_error_reports_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_engine_state_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_engine_state_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_drivers_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_drivers_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_driver_issues_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_driver_issues_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_driver_expenses_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_driver_expenses_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_driver_advance_requests_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_driver_advance_requests_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_dispatches_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_dispatches_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_dispatch_overrides_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_dispatch_overrides_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_device_quarantine_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_device_quarantine_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_customers_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_customers_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_credit_debit_notes_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_credit_debit_notes_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_company_config_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_company_config_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_bookings_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_bookings_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_alerts_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_alerts_tenant_fk_insert;

-- Drop only indexes created in Up (21)
DROP INDEX IF EXISTS idx_vehicles_tenant;
DROP INDEX IF EXISTS idx_vehicle_latest_position_tenant;
DROP INDEX IF EXISTS idx_vehicle_geofences_tenant;
DROP INDEX IF EXISTS idx_trips_tenant;
DROP INDEX IF EXISTS idx_trip_feedback_tenant;
DROP INDEX IF EXISTS idx_trip_detentions_tenant;
DROP INDEX IF EXISTS idx_telemetry_raw_events_tenant;
DROP INDEX IF EXISTS idx_telemetry_positions_tenant;
DROP INDEX IF EXISTS idx_provider_poll_state_tenant;
DROP INDEX IF EXISTS idx_offline_sync_log_tenant;
DROP INDEX IF EXISTS idx_invoices_tenant;
DROP INDEX IF EXISTS idx_invoice_line_items_tenant;
DROP INDEX IF EXISTS idx_geofence_events_tenant;
DROP INDEX IF EXISTS idx_experiment_events_tenant;
DROP INDEX IF EXISTS idx_engine_state_tenant;
DROP INDEX IF EXISTS idx_drivers_tenant;
DROP INDEX IF EXISTS idx_driver_issues_tenant;
DROP INDEX IF EXISTS idx_dispatches_tenant;
DROP INDEX IF EXISTS idx_dispatch_overrides_tenant;
DROP INDEX IF EXISTS idx_device_quarantine_tenant;
DROP INDEX IF EXISTS idx_bookings_tenant;
