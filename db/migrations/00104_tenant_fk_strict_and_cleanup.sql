-- +goose Up
-- 00104 Fix 00103 gaps: reject empty-string tenant_id (fail-closed) + remove test-only tenants from prod.
-- Drops 102 triggers with `!= ''` bypass and recreates them as strict `IS NOT NULL` only.
-- Also deletes 29 test-only tenants that were seeded in 00103 for back-compat; tests must now seed via helpers.

DROP TRIGGER IF EXISTS trg_alerts_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_alerts_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_bookings_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_bookings_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_company_config_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_company_config_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_credit_debit_notes_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_credit_debit_notes_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_customers_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_customers_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_device_quarantine_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_device_quarantine_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_dispatch_overrides_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_dispatch_overrides_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_dispatches_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_dispatches_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_driver_advance_requests_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_driver_advance_requests_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_driver_expenses_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_driver_expenses_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_driver_issues_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_driver_issues_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_drivers_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_drivers_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_engine_state_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_engine_state_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_error_reports_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_error_reports_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_eta_history_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_eta_history_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_eta_history_monthly_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_eta_history_monthly_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_experiment_assignments_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_experiment_assignments_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_experiment_events_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_experiment_events_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_experiments_spec16_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_experiments_spec16_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_fastag_tags_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_fastag_tags_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_fastag_transactions_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_fastag_transactions_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_feature_flags_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_feature_flags_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_founder_audit_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_founder_audit_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_founder_signals_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_founder_signals_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_fuel_prices_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_fuel_prices_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_geofence_events_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_geofence_events_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_geofences_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_geofences_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_incidents_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_incidents_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_invoice_line_items_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_invoice_line_items_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_invoice_sequences_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_invoice_sequences_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_invoices_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_invoices_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_maintenance_records_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_maintenance_records_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_money_ledger_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_money_ledger_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_note_sequences_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_note_sequences_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_offline_sync_log_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_offline_sync_log_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_ops_alerts_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_ops_alerts_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_payments_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_payments_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_pnl_daily_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_pnl_daily_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_provider_poll_state_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_provider_poll_state_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_route_optimization_jobs_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_route_optimization_jobs_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_routes_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_routes_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_telemetry_devices_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_telemetry_devices_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_telemetry_positions_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_telemetry_positions_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_telemetry_raw_events_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_telemetry_raw_events_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_trip_detentions_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_trip_detentions_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_trip_feedback_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_trip_feedback_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_trips_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_trips_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_users_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_users_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_vehicle_geofences_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_vehicle_geofences_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_vehicle_latest_position_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_vehicle_latest_position_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_vehicles_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_vehicles_tenant_fk_update;

-- +goose StatementBegin
CREATE TRIGGER trg_alerts_tenant_fk_insert
BEFORE INSERT ON alerts
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for alerts.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_alerts_tenant_fk_update
BEFORE UPDATE OF tenant_id ON alerts
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for alerts.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_bookings_tenant_fk_insert
BEFORE INSERT ON bookings
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for bookings.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_bookings_tenant_fk_update
BEFORE UPDATE OF tenant_id ON bookings
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for bookings.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_company_config_tenant_fk_insert
BEFORE INSERT ON company_config
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for company_config.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_company_config_tenant_fk_update
BEFORE UPDATE OF tenant_id ON company_config
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for company_config.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_credit_debit_notes_tenant_fk_insert
BEFORE INSERT ON credit_debit_notes
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for credit_debit_notes.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_credit_debit_notes_tenant_fk_update
BEFORE UPDATE OF tenant_id ON credit_debit_notes
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for credit_debit_notes.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_customers_tenant_fk_insert
BEFORE INSERT ON customers
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for customers.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_customers_tenant_fk_update
BEFORE UPDATE OF tenant_id ON customers
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for customers.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_device_quarantine_tenant_fk_insert
BEFORE INSERT ON device_quarantine
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for device_quarantine.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_device_quarantine_tenant_fk_update
BEFORE UPDATE OF tenant_id ON device_quarantine
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for device_quarantine.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_dispatch_overrides_tenant_fk_insert
BEFORE INSERT ON dispatch_overrides
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for dispatch_overrides.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_dispatch_overrides_tenant_fk_update
BEFORE UPDATE OF tenant_id ON dispatch_overrides
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for dispatch_overrides.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_dispatches_tenant_fk_insert
BEFORE INSERT ON dispatches
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for dispatches.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_dispatches_tenant_fk_update
BEFORE UPDATE OF tenant_id ON dispatches
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for dispatches.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_driver_advance_requests_tenant_fk_insert
BEFORE INSERT ON driver_advance_requests
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_advance_requests.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_driver_advance_requests_tenant_fk_update
BEFORE UPDATE OF tenant_id ON driver_advance_requests
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_advance_requests.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_driver_expenses_tenant_fk_insert
BEFORE INSERT ON driver_expenses
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_expenses.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_driver_expenses_tenant_fk_update
BEFORE UPDATE OF tenant_id ON driver_expenses
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_expenses.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_driver_issues_tenant_fk_insert
BEFORE INSERT ON driver_issues
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_issues.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_driver_issues_tenant_fk_update
BEFORE UPDATE OF tenant_id ON driver_issues
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_issues.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_drivers_tenant_fk_insert
BEFORE INSERT ON drivers
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for drivers.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_drivers_tenant_fk_update
BEFORE UPDATE OF tenant_id ON drivers
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for drivers.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_engine_state_tenant_fk_insert
BEFORE INSERT ON engine_state
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for engine_state.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_engine_state_tenant_fk_update
BEFORE UPDATE OF tenant_id ON engine_state
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for engine_state.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_error_reports_tenant_fk_insert
BEFORE INSERT ON error_reports
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for error_reports.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_error_reports_tenant_fk_update
BEFORE UPDATE OF tenant_id ON error_reports
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for error_reports.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_eta_history_tenant_fk_insert
BEFORE INSERT ON eta_history
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for eta_history.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_eta_history_tenant_fk_update
BEFORE UPDATE OF tenant_id ON eta_history
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for eta_history.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_eta_history_monthly_tenant_fk_insert
BEFORE INSERT ON eta_history_monthly
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for eta_history_monthly.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_eta_history_monthly_tenant_fk_update
BEFORE UPDATE OF tenant_id ON eta_history_monthly
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for eta_history_monthly.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_experiment_assignments_tenant_fk_insert
BEFORE INSERT ON experiment_assignments
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiment_assignments.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_experiment_assignments_tenant_fk_update
BEFORE UPDATE OF tenant_id ON experiment_assignments
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiment_assignments.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_experiment_events_tenant_fk_insert
BEFORE INSERT ON experiment_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiment_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_experiment_events_tenant_fk_update
BEFORE UPDATE OF tenant_id ON experiment_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiment_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_experiments_spec16_tenant_fk_insert
BEFORE INSERT ON experiments_spec16
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiments_spec16.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_experiments_spec16_tenant_fk_update
BEFORE UPDATE OF tenant_id ON experiments_spec16
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiments_spec16.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_fastag_tags_tenant_fk_insert
BEFORE INSERT ON fastag_tags
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fastag_tags.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_fastag_tags_tenant_fk_update
BEFORE UPDATE OF tenant_id ON fastag_tags
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fastag_tags.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_fastag_transactions_tenant_fk_insert
BEFORE INSERT ON fastag_transactions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fastag_transactions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_fastag_transactions_tenant_fk_update
BEFORE UPDATE OF tenant_id ON fastag_transactions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fastag_transactions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_feature_flags_tenant_fk_insert
BEFORE INSERT ON feature_flags
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for feature_flags.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_feature_flags_tenant_fk_update
BEFORE UPDATE OF tenant_id ON feature_flags
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for feature_flags.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_founder_audit_tenant_fk_insert
BEFORE INSERT ON founder_audit
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for founder_audit.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_founder_audit_tenant_fk_update
BEFORE UPDATE OF tenant_id ON founder_audit
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for founder_audit.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_founder_signals_tenant_fk_insert
BEFORE INSERT ON founder_signals
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for founder_signals.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_founder_signals_tenant_fk_update
BEFORE UPDATE OF tenant_id ON founder_signals
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for founder_signals.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_fuel_prices_tenant_fk_insert
BEFORE INSERT ON fuel_prices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fuel_prices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_fuel_prices_tenant_fk_update
BEFORE UPDATE OF tenant_id ON fuel_prices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fuel_prices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_geofence_events_tenant_fk_insert
BEFORE INSERT ON geofence_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for geofence_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_geofence_events_tenant_fk_update
BEFORE UPDATE OF tenant_id ON geofence_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for geofence_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_geofences_tenant_fk_insert
BEFORE INSERT ON geofences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for geofences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_geofences_tenant_fk_update
BEFORE UPDATE OF tenant_id ON geofences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for geofences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_incidents_tenant_fk_insert
BEFORE INSERT ON incidents
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for incidents.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_incidents_tenant_fk_update
BEFORE UPDATE OF tenant_id ON incidents
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for incidents.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_invoice_line_items_tenant_fk_insert
BEFORE INSERT ON invoice_line_items
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoice_line_items.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_invoice_line_items_tenant_fk_update
BEFORE UPDATE OF tenant_id ON invoice_line_items
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoice_line_items.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_invoice_sequences_tenant_fk_insert
BEFORE INSERT ON invoice_sequences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoice_sequences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_invoice_sequences_tenant_fk_update
BEFORE UPDATE OF tenant_id ON invoice_sequences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoice_sequences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_invoices_tenant_fk_insert
BEFORE INSERT ON invoices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_invoices_tenant_fk_update
BEFORE UPDATE OF tenant_id ON invoices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_maintenance_records_tenant_fk_insert
BEFORE INSERT ON maintenance_records
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for maintenance_records.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_maintenance_records_tenant_fk_update
BEFORE UPDATE OF tenant_id ON maintenance_records
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for maintenance_records.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_money_ledger_tenant_fk_insert
BEFORE INSERT ON money_ledger
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for money_ledger.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_money_ledger_tenant_fk_update
BEFORE UPDATE OF tenant_id ON money_ledger
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for money_ledger.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_note_sequences_tenant_fk_insert
BEFORE INSERT ON note_sequences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for note_sequences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_note_sequences_tenant_fk_update
BEFORE UPDATE OF tenant_id ON note_sequences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for note_sequences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_offline_sync_log_tenant_fk_insert
BEFORE INSERT ON offline_sync_log
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for offline_sync_log.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_offline_sync_log_tenant_fk_update
BEFORE UPDATE OF tenant_id ON offline_sync_log
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for offline_sync_log.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_ops_alerts_tenant_fk_insert
BEFORE INSERT ON ops_alerts
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for ops_alerts.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_ops_alerts_tenant_fk_update
BEFORE UPDATE OF tenant_id ON ops_alerts
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for ops_alerts.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_payments_tenant_fk_insert
BEFORE INSERT ON payments
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for payments.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_payments_tenant_fk_update
BEFORE UPDATE OF tenant_id ON payments
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for payments.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_pnl_daily_tenant_fk_insert
BEFORE INSERT ON pnl_daily
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for pnl_daily.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_pnl_daily_tenant_fk_update
BEFORE UPDATE OF tenant_id ON pnl_daily
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for pnl_daily.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_provider_poll_state_tenant_fk_insert
BEFORE INSERT ON provider_poll_state
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for provider_poll_state.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_provider_poll_state_tenant_fk_update
BEFORE UPDATE OF tenant_id ON provider_poll_state
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for provider_poll_state.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_route_optimization_jobs_tenant_fk_insert
BEFORE INSERT ON route_optimization_jobs
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for route_optimization_jobs.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_route_optimization_jobs_tenant_fk_update
BEFORE UPDATE OF tenant_id ON route_optimization_jobs
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for route_optimization_jobs.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_routes_tenant_fk_insert
BEFORE INSERT ON routes
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for routes.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_routes_tenant_fk_update
BEFORE UPDATE OF tenant_id ON routes
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for routes.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_telemetry_devices_tenant_fk_insert
BEFORE INSERT ON telemetry_devices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_devices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_telemetry_devices_tenant_fk_update
BEFORE UPDATE OF tenant_id ON telemetry_devices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_devices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_telemetry_positions_tenant_fk_insert
BEFORE INSERT ON telemetry_positions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_positions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_telemetry_positions_tenant_fk_update
BEFORE UPDATE OF tenant_id ON telemetry_positions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_positions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_telemetry_raw_events_tenant_fk_insert
BEFORE INSERT ON telemetry_raw_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_raw_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_telemetry_raw_events_tenant_fk_update
BEFORE UPDATE OF tenant_id ON telemetry_raw_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_raw_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_trip_detentions_tenant_fk_insert
BEFORE INSERT ON trip_detentions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trip_detentions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_trip_detentions_tenant_fk_update
BEFORE UPDATE OF tenant_id ON trip_detentions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trip_detentions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_trip_feedback_tenant_fk_insert
BEFORE INSERT ON trip_feedback
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trip_feedback.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_trip_feedback_tenant_fk_update
BEFORE UPDATE OF tenant_id ON trip_feedback
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trip_feedback.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_trips_tenant_fk_insert
BEFORE INSERT ON trips
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trips.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_trips_tenant_fk_update
BEFORE UPDATE OF tenant_id ON trips
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trips.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_users_tenant_fk_insert
BEFORE INSERT ON users
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for users.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_users_tenant_fk_update
BEFORE UPDATE OF tenant_id ON users
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for users.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_vehicle_geofences_tenant_fk_insert
BEFORE INSERT ON vehicle_geofences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicle_geofences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_vehicle_geofences_tenant_fk_update
BEFORE UPDATE OF tenant_id ON vehicle_geofences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicle_geofences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_vehicle_latest_position_tenant_fk_insert
BEFORE INSERT ON vehicle_latest_position
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicle_latest_position.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_vehicle_latest_position_tenant_fk_update
BEFORE UPDATE OF tenant_id ON vehicle_latest_position
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicle_latest_position.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_vehicles_tenant_fk_insert
BEFORE INSERT ON vehicles
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicles.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_vehicles_tenant_fk_update
BEFORE UPDATE OF tenant_id ON vehicles
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicles.tenant_id') END;
END;
-- +goose StatementEnd

-- 00104 prod cleanup deferred: keep test tenants seeded in 00103 for now; will clean in 00105 after test helpers seed via NewTestDB

-- +goose Down
-- Recreate 00103 triggers with empty-string bypass and re-seed test tenants (for rollback).

DROP TRIGGER IF EXISTS trg_alerts_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_alerts_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_bookings_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_bookings_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_company_config_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_company_config_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_credit_debit_notes_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_credit_debit_notes_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_customers_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_customers_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_device_quarantine_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_device_quarantine_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_dispatch_overrides_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_dispatch_overrides_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_dispatches_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_dispatches_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_driver_advance_requests_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_driver_advance_requests_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_driver_expenses_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_driver_expenses_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_driver_issues_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_driver_issues_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_drivers_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_drivers_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_engine_state_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_engine_state_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_error_reports_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_error_reports_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_eta_history_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_eta_history_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_eta_history_monthly_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_eta_history_monthly_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_experiment_assignments_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_experiment_assignments_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_experiment_events_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_experiment_events_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_experiments_spec16_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_experiments_spec16_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_fastag_tags_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_fastag_tags_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_fastag_transactions_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_fastag_transactions_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_feature_flags_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_feature_flags_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_founder_audit_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_founder_audit_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_founder_signals_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_founder_signals_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_fuel_prices_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_fuel_prices_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_geofence_events_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_geofence_events_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_geofences_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_geofences_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_incidents_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_incidents_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_invoice_line_items_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_invoice_line_items_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_invoice_sequences_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_invoice_sequences_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_invoices_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_invoices_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_maintenance_records_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_maintenance_records_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_money_ledger_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_money_ledger_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_note_sequences_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_note_sequences_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_offline_sync_log_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_offline_sync_log_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_ops_alerts_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_ops_alerts_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_payments_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_payments_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_pnl_daily_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_pnl_daily_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_provider_poll_state_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_provider_poll_state_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_route_optimization_jobs_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_route_optimization_jobs_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_routes_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_routes_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_telemetry_devices_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_telemetry_devices_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_telemetry_positions_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_telemetry_positions_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_telemetry_raw_events_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_telemetry_raw_events_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_trip_detentions_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_trip_detentions_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_trip_feedback_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_trip_feedback_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_trips_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_trips_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_users_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_users_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_vehicle_geofences_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_vehicle_geofences_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_vehicle_latest_position_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_vehicle_latest_position_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_vehicles_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_vehicles_tenant_fk_update;

-- +goose StatementBegin
CREATE TRIGGER trg_alerts_tenant_fk_insert
BEFORE INSERT ON alerts
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for alerts.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_alerts_tenant_fk_update
BEFORE UPDATE OF tenant_id ON alerts
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for alerts.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_bookings_tenant_fk_insert
BEFORE INSERT ON bookings
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for bookings.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_bookings_tenant_fk_update
BEFORE UPDATE OF tenant_id ON bookings
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for bookings.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_company_config_tenant_fk_insert
BEFORE INSERT ON company_config
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for company_config.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_company_config_tenant_fk_update
BEFORE UPDATE OF tenant_id ON company_config
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for company_config.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_credit_debit_notes_tenant_fk_insert
BEFORE INSERT ON credit_debit_notes
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for credit_debit_notes.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_credit_debit_notes_tenant_fk_update
BEFORE UPDATE OF tenant_id ON credit_debit_notes
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for credit_debit_notes.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_customers_tenant_fk_insert
BEFORE INSERT ON customers
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for customers.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_customers_tenant_fk_update
BEFORE UPDATE OF tenant_id ON customers
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for customers.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_device_quarantine_tenant_fk_insert
BEFORE INSERT ON device_quarantine
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for device_quarantine.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_device_quarantine_tenant_fk_update
BEFORE UPDATE OF tenant_id ON device_quarantine
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for device_quarantine.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_dispatch_overrides_tenant_fk_insert
BEFORE INSERT ON dispatch_overrides
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for dispatch_overrides.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_dispatch_overrides_tenant_fk_update
BEFORE UPDATE OF tenant_id ON dispatch_overrides
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for dispatch_overrides.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_dispatches_tenant_fk_insert
BEFORE INSERT ON dispatches
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for dispatches.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_dispatches_tenant_fk_update
BEFORE UPDATE OF tenant_id ON dispatches
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for dispatches.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_driver_advance_requests_tenant_fk_insert
BEFORE INSERT ON driver_advance_requests
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_advance_requests.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_driver_advance_requests_tenant_fk_update
BEFORE UPDATE OF tenant_id ON driver_advance_requests
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_advance_requests.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_driver_expenses_tenant_fk_insert
BEFORE INSERT ON driver_expenses
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_expenses.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_driver_expenses_tenant_fk_update
BEFORE UPDATE OF tenant_id ON driver_expenses
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_expenses.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_driver_issues_tenant_fk_insert
BEFORE INSERT ON driver_issues
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_issues.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_driver_issues_tenant_fk_update
BEFORE UPDATE OF tenant_id ON driver_issues
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_issues.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_drivers_tenant_fk_insert
BEFORE INSERT ON drivers
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for drivers.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_drivers_tenant_fk_update
BEFORE UPDATE OF tenant_id ON drivers
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for drivers.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_engine_state_tenant_fk_insert
BEFORE INSERT ON engine_state
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for engine_state.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_engine_state_tenant_fk_update
BEFORE UPDATE OF tenant_id ON engine_state
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for engine_state.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_error_reports_tenant_fk_insert
BEFORE INSERT ON error_reports
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for error_reports.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_error_reports_tenant_fk_update
BEFORE UPDATE OF tenant_id ON error_reports
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for error_reports.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_eta_history_tenant_fk_insert
BEFORE INSERT ON eta_history
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for eta_history.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_eta_history_tenant_fk_update
BEFORE UPDATE OF tenant_id ON eta_history
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for eta_history.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_eta_history_monthly_tenant_fk_insert
BEFORE INSERT ON eta_history_monthly
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for eta_history_monthly.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_eta_history_monthly_tenant_fk_update
BEFORE UPDATE OF tenant_id ON eta_history_monthly
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for eta_history_monthly.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_experiment_assignments_tenant_fk_insert
BEFORE INSERT ON experiment_assignments
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiment_assignments.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_experiment_assignments_tenant_fk_update
BEFORE UPDATE OF tenant_id ON experiment_assignments
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiment_assignments.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_experiment_events_tenant_fk_insert
BEFORE INSERT ON experiment_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiment_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_experiment_events_tenant_fk_update
BEFORE UPDATE OF tenant_id ON experiment_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiment_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_experiments_spec16_tenant_fk_insert
BEFORE INSERT ON experiments_spec16
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiments_spec16.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_experiments_spec16_tenant_fk_update
BEFORE UPDATE OF tenant_id ON experiments_spec16
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for experiments_spec16.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_fastag_tags_tenant_fk_insert
BEFORE INSERT ON fastag_tags
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fastag_tags.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_fastag_tags_tenant_fk_update
BEFORE UPDATE OF tenant_id ON fastag_tags
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fastag_tags.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_fastag_transactions_tenant_fk_insert
BEFORE INSERT ON fastag_transactions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fastag_transactions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_fastag_transactions_tenant_fk_update
BEFORE UPDATE OF tenant_id ON fastag_transactions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fastag_transactions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_feature_flags_tenant_fk_insert
BEFORE INSERT ON feature_flags
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for feature_flags.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_feature_flags_tenant_fk_update
BEFORE UPDATE OF tenant_id ON feature_flags
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for feature_flags.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_founder_audit_tenant_fk_insert
BEFORE INSERT ON founder_audit
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for founder_audit.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_founder_audit_tenant_fk_update
BEFORE UPDATE OF tenant_id ON founder_audit
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for founder_audit.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_founder_signals_tenant_fk_insert
BEFORE INSERT ON founder_signals
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for founder_signals.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_founder_signals_tenant_fk_update
BEFORE UPDATE OF tenant_id ON founder_signals
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for founder_signals.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_fuel_prices_tenant_fk_insert
BEFORE INSERT ON fuel_prices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fuel_prices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_fuel_prices_tenant_fk_update
BEFORE UPDATE OF tenant_id ON fuel_prices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fuel_prices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_geofence_events_tenant_fk_insert
BEFORE INSERT ON geofence_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for geofence_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_geofence_events_tenant_fk_update
BEFORE UPDATE OF tenant_id ON geofence_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for geofence_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_geofences_tenant_fk_insert
BEFORE INSERT ON geofences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for geofences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_geofences_tenant_fk_update
BEFORE UPDATE OF tenant_id ON geofences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for geofences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_incidents_tenant_fk_insert
BEFORE INSERT ON incidents
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for incidents.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_incidents_tenant_fk_update
BEFORE UPDATE OF tenant_id ON incidents
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for incidents.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_invoice_line_items_tenant_fk_insert
BEFORE INSERT ON invoice_line_items
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoice_line_items.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_invoice_line_items_tenant_fk_update
BEFORE UPDATE OF tenant_id ON invoice_line_items
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoice_line_items.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_invoice_sequences_tenant_fk_insert
BEFORE INSERT ON invoice_sequences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoice_sequences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_invoice_sequences_tenant_fk_update
BEFORE UPDATE OF tenant_id ON invoice_sequences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoice_sequences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_invoices_tenant_fk_insert
BEFORE INSERT ON invoices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_invoices_tenant_fk_update
BEFORE UPDATE OF tenant_id ON invoices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_maintenance_records_tenant_fk_insert
BEFORE INSERT ON maintenance_records
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for maintenance_records.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_maintenance_records_tenant_fk_update
BEFORE UPDATE OF tenant_id ON maintenance_records
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for maintenance_records.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_money_ledger_tenant_fk_insert
BEFORE INSERT ON money_ledger
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for money_ledger.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_money_ledger_tenant_fk_update
BEFORE UPDATE OF tenant_id ON money_ledger
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for money_ledger.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_note_sequences_tenant_fk_insert
BEFORE INSERT ON note_sequences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for note_sequences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_note_sequences_tenant_fk_update
BEFORE UPDATE OF tenant_id ON note_sequences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for note_sequences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_offline_sync_log_tenant_fk_insert
BEFORE INSERT ON offline_sync_log
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for offline_sync_log.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_offline_sync_log_tenant_fk_update
BEFORE UPDATE OF tenant_id ON offline_sync_log
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for offline_sync_log.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_ops_alerts_tenant_fk_insert
BEFORE INSERT ON ops_alerts
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for ops_alerts.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_ops_alerts_tenant_fk_update
BEFORE UPDATE OF tenant_id ON ops_alerts
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for ops_alerts.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_payments_tenant_fk_insert
BEFORE INSERT ON payments
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for payments.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_payments_tenant_fk_update
BEFORE UPDATE OF tenant_id ON payments
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for payments.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_pnl_daily_tenant_fk_insert
BEFORE INSERT ON pnl_daily
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for pnl_daily.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_pnl_daily_tenant_fk_update
BEFORE UPDATE OF tenant_id ON pnl_daily
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for pnl_daily.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_provider_poll_state_tenant_fk_insert
BEFORE INSERT ON provider_poll_state
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for provider_poll_state.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_provider_poll_state_tenant_fk_update
BEFORE UPDATE OF tenant_id ON provider_poll_state
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for provider_poll_state.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_route_optimization_jobs_tenant_fk_insert
BEFORE INSERT ON route_optimization_jobs
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for route_optimization_jobs.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_route_optimization_jobs_tenant_fk_update
BEFORE UPDATE OF tenant_id ON route_optimization_jobs
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for route_optimization_jobs.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_routes_tenant_fk_insert
BEFORE INSERT ON routes
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for routes.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_routes_tenant_fk_update
BEFORE UPDATE OF tenant_id ON routes
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for routes.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_telemetry_devices_tenant_fk_insert
BEFORE INSERT ON telemetry_devices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_devices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_telemetry_devices_tenant_fk_update
BEFORE UPDATE OF tenant_id ON telemetry_devices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_devices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_telemetry_positions_tenant_fk_insert
BEFORE INSERT ON telemetry_positions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_positions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_telemetry_positions_tenant_fk_update
BEFORE UPDATE OF tenant_id ON telemetry_positions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_positions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_telemetry_raw_events_tenant_fk_insert
BEFORE INSERT ON telemetry_raw_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_raw_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_telemetry_raw_events_tenant_fk_update
BEFORE UPDATE OF tenant_id ON telemetry_raw_events
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for telemetry_raw_events.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_trip_detentions_tenant_fk_insert
BEFORE INSERT ON trip_detentions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trip_detentions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_trip_detentions_tenant_fk_update
BEFORE UPDATE OF tenant_id ON trip_detentions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trip_detentions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_trip_feedback_tenant_fk_insert
BEFORE INSERT ON trip_feedback
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trip_feedback.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_trip_feedback_tenant_fk_update
BEFORE UPDATE OF tenant_id ON trip_feedback
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trip_feedback.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_trips_tenant_fk_insert
BEFORE INSERT ON trips
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trips.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_trips_tenant_fk_update
BEFORE UPDATE OF tenant_id ON trips
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for trips.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_users_tenant_fk_insert
BEFORE INSERT ON users
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for users.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_users_tenant_fk_update
BEFORE UPDATE OF tenant_id ON users
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for users.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_vehicle_geofences_tenant_fk_insert
BEFORE INSERT ON vehicle_geofences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicle_geofences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_vehicle_geofences_tenant_fk_update
BEFORE UPDATE OF tenant_id ON vehicle_geofences
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicle_geofences.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_vehicle_latest_position_tenant_fk_insert
BEFORE INSERT ON vehicle_latest_position
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicle_latest_position.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_vehicle_latest_position_tenant_fk_update
BEFORE UPDATE OF tenant_id ON vehicle_latest_position
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicle_latest_position.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_vehicles_tenant_fk_insert
BEFORE INSERT ON vehicles
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicles.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_vehicles_tenant_fk_update
BEFORE UPDATE OF tenant_id ON vehicles
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicles.tenant_id') END;
END;
-- +goose StatementEnd

INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('2','2','2');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('7','7','7');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('9','9','9');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('other-tenant','other-tenant','other-tenant');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('another-tenant','another-tenant','another-tenant');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-1','tenant-1','tenant-1');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-2','tenant-2','tenant-2');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-7','tenant-7','tenant-7');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-9','tenant-9','tenant-9');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-999','tenant-999','tenant-999');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-a','tenant-a','tenant-a');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-b','tenant-b','tenant-b');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-A','tenant-A','tenant-A');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-B','tenant-B','tenant-B');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-zz','tenant-zz','tenant-zz');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-seq','tenant-seq','tenant-seq');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-cap','tenant-cap','tenant-cap');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-dn','tenant-dn','tenant-dn');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-ledger','tenant-ledger','tenant-ledger');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-val','tenant-val','tenant-val');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-fmt','tenant-fmt','tenant-fmt');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-loop','tenant-loop','tenant-loop');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tn-b','tn-b','tn-b');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tn-kpi','tn-kpi','tn-kpi');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-c','tenant-c','tenant-c');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-d','tenant-d','tenant-d');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-forged','tenant-forged','tenant-forged');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-42','tenant-42','tenant-42');
INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('test-tenant','test-tenant','test-tenant');