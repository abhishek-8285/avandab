-- PG port of 00105_tenant_strict_and_prod_cleanup.sql | status: MANUAL | flags: MANUAL-TRIGGER-GEN | reviewed: YES
-- +goose Up
-- Shared guard: one function for all tenant-FK triggers. TG_TABLE_NAME
-- reproduces the sqlite per-table error text.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION tenant_fk_guard_fn()
RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id) THEN
        RAISE EXCEPTION 'FK violation: tenants(id) missing for %.tenant_id', TG_TABLE_NAME;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
-- 00105 Tenant strict + prod cleanup — make empty-string fail-closed and remove test-only tenants.
-- Drops 00104 triggers (with != '' bypass) and recreates strict IS NOT NULL only.
-- Deletes 29 test tenants seeded in 00103; tests must seed via test/helpers.go NewTestDB.

DROP TRIGGER IF EXISTS trg_alerts_tenant_fk_insert ON alerts;
DROP TRIGGER IF EXISTS trg_alerts_tenant_fk_update ON alerts;
DROP TRIGGER IF EXISTS trg_bookings_tenant_fk_insert ON bookings;
DROP TRIGGER IF EXISTS trg_bookings_tenant_fk_update ON bookings;
DROP TRIGGER IF EXISTS trg_company_config_tenant_fk_insert ON company_config;
DROP TRIGGER IF EXISTS trg_company_config_tenant_fk_update ON company_config;
DROP TRIGGER IF EXISTS trg_credit_debit_notes_tenant_fk_insert ON credit_debit_notes;
DROP TRIGGER IF EXISTS trg_credit_debit_notes_tenant_fk_update ON credit_debit_notes;
DROP TRIGGER IF EXISTS trg_customers_tenant_fk_insert ON customers;
DROP TRIGGER IF EXISTS trg_customers_tenant_fk_update ON customers;
DROP TRIGGER IF EXISTS trg_device_quarantine_tenant_fk_insert ON device_quarantine;
DROP TRIGGER IF EXISTS trg_device_quarantine_tenant_fk_update ON device_quarantine;
DROP TRIGGER IF EXISTS trg_dispatch_overrides_tenant_fk_insert ON dispatch_overrides;
DROP TRIGGER IF EXISTS trg_dispatch_overrides_tenant_fk_update ON dispatch_overrides;
DROP TRIGGER IF EXISTS trg_dispatches_tenant_fk_insert ON dispatches;
DROP TRIGGER IF EXISTS trg_dispatches_tenant_fk_update ON dispatches;
DROP TRIGGER IF EXISTS trg_driver_advance_requests_tenant_fk_insert ON driver_advance_requests;
DROP TRIGGER IF EXISTS trg_driver_advance_requests_tenant_fk_update ON driver_advance_requests;
DROP TRIGGER IF EXISTS trg_driver_expenses_tenant_fk_insert ON driver_expenses;
DROP TRIGGER IF EXISTS trg_driver_expenses_tenant_fk_update ON driver_expenses;
DROP TRIGGER IF EXISTS trg_driver_issues_tenant_fk_insert ON driver_issues;
DROP TRIGGER IF EXISTS trg_driver_issues_tenant_fk_update ON driver_issues;
DROP TRIGGER IF EXISTS trg_drivers_tenant_fk_insert ON drivers;
DROP TRIGGER IF EXISTS trg_drivers_tenant_fk_update ON drivers;
DROP TRIGGER IF EXISTS trg_engine_state_tenant_fk_insert ON engine_state;
DROP TRIGGER IF EXISTS trg_engine_state_tenant_fk_update ON engine_state;
DROP TRIGGER IF EXISTS trg_error_reports_tenant_fk_insert ON error_reports;
DROP TRIGGER IF EXISTS trg_error_reports_tenant_fk_update ON error_reports;
DROP TRIGGER IF EXISTS trg_eta_history_tenant_fk_insert ON eta_history;
DROP TRIGGER IF EXISTS trg_eta_history_tenant_fk_update ON eta_history;
DROP TRIGGER IF EXISTS trg_eta_history_monthly_tenant_fk_insert ON eta_history_monthly;
DROP TRIGGER IF EXISTS trg_eta_history_monthly_tenant_fk_update ON eta_history_monthly;
DROP TRIGGER IF EXISTS trg_experiment_assignments_tenant_fk_insert ON experiment_assignments;
DROP TRIGGER IF EXISTS trg_experiment_assignments_tenant_fk_update ON experiment_assignments;
DROP TRIGGER IF EXISTS trg_experiment_events_tenant_fk_insert ON experiment_events;
DROP TRIGGER IF EXISTS trg_experiment_events_tenant_fk_update ON experiment_events;
DROP TRIGGER IF EXISTS trg_experiments_spec16_tenant_fk_insert ON experiments_spec16;
DROP TRIGGER IF EXISTS trg_experiments_spec16_tenant_fk_update ON experiments_spec16;
DROP TRIGGER IF EXISTS trg_fastag_tags_tenant_fk_insert ON fastag_tags;
DROP TRIGGER IF EXISTS trg_fastag_tags_tenant_fk_update ON fastag_tags;
DROP TRIGGER IF EXISTS trg_fastag_transactions_tenant_fk_insert ON fastag_transactions;
DROP TRIGGER IF EXISTS trg_fastag_transactions_tenant_fk_update ON fastag_transactions;
DROP TRIGGER IF EXISTS trg_feature_flags_tenant_fk_insert ON feature_flags;
DROP TRIGGER IF EXISTS trg_feature_flags_tenant_fk_update ON feature_flags;
DROP TRIGGER IF EXISTS trg_founder_audit_tenant_fk_insert ON founder_audit;
DROP TRIGGER IF EXISTS trg_founder_audit_tenant_fk_update ON founder_audit;
DROP TRIGGER IF EXISTS trg_founder_signals_tenant_fk_insert ON founder_signals;
DROP TRIGGER IF EXISTS trg_founder_signals_tenant_fk_update ON founder_signals;
DROP TRIGGER IF EXISTS trg_fuel_prices_tenant_fk_insert ON fuel_prices;
DROP TRIGGER IF EXISTS trg_fuel_prices_tenant_fk_update ON fuel_prices;
DROP TRIGGER IF EXISTS trg_geofence_events_tenant_fk_insert ON geofence_events;
DROP TRIGGER IF EXISTS trg_geofence_events_tenant_fk_update ON geofence_events;
DROP TRIGGER IF EXISTS trg_geofences_tenant_fk_insert ON geofences;
DROP TRIGGER IF EXISTS trg_geofences_tenant_fk_update ON geofences;
DROP TRIGGER IF EXISTS trg_incidents_tenant_fk_insert ON incidents;
DROP TRIGGER IF EXISTS trg_incidents_tenant_fk_update ON incidents;
DROP TRIGGER IF EXISTS trg_invoice_line_items_tenant_fk_insert ON invoice_line_items;
DROP TRIGGER IF EXISTS trg_invoice_line_items_tenant_fk_update ON invoice_line_items;
DROP TRIGGER IF EXISTS trg_invoice_sequences_tenant_fk_insert ON invoice_sequences;
DROP TRIGGER IF EXISTS trg_invoice_sequences_tenant_fk_update ON invoice_sequences;
DROP TRIGGER IF EXISTS trg_invoices_tenant_fk_insert ON invoices;
DROP TRIGGER IF EXISTS trg_invoices_tenant_fk_update ON invoices;
DROP TRIGGER IF EXISTS trg_maintenance_records_tenant_fk_insert ON maintenance_records;
DROP TRIGGER IF EXISTS trg_maintenance_records_tenant_fk_update ON maintenance_records;
DROP TRIGGER IF EXISTS trg_money_ledger_tenant_fk_insert ON money_ledger;
DROP TRIGGER IF EXISTS trg_money_ledger_tenant_fk_update ON money_ledger;
DROP TRIGGER IF EXISTS trg_note_sequences_tenant_fk_insert ON note_sequences;
DROP TRIGGER IF EXISTS trg_note_sequences_tenant_fk_update ON note_sequences;
DROP TRIGGER IF EXISTS trg_offline_sync_log_tenant_fk_insert ON offline_sync_log;
DROP TRIGGER IF EXISTS trg_offline_sync_log_tenant_fk_update ON offline_sync_log;
DROP TRIGGER IF EXISTS trg_ops_alerts_tenant_fk_insert ON ops_alerts;
DROP TRIGGER IF EXISTS trg_ops_alerts_tenant_fk_update ON ops_alerts;
DROP TRIGGER IF EXISTS trg_payments_tenant_fk_insert ON payments;
DROP TRIGGER IF EXISTS trg_payments_tenant_fk_update ON payments;
DROP TRIGGER IF EXISTS trg_pnl_daily_tenant_fk_insert ON pnl_daily;
DROP TRIGGER IF EXISTS trg_pnl_daily_tenant_fk_update ON pnl_daily;
DROP TRIGGER IF EXISTS trg_provider_poll_state_tenant_fk_insert ON provider_poll_state;
DROP TRIGGER IF EXISTS trg_provider_poll_state_tenant_fk_update ON provider_poll_state;
DROP TRIGGER IF EXISTS trg_route_optimization_jobs_tenant_fk_insert ON route_optimization_jobs;
DROP TRIGGER IF EXISTS trg_route_optimization_jobs_tenant_fk_update ON route_optimization_jobs;
DROP TRIGGER IF EXISTS trg_routes_tenant_fk_insert ON routes;
DROP TRIGGER IF EXISTS trg_routes_tenant_fk_update ON routes;
DROP TRIGGER IF EXISTS trg_telemetry_devices_tenant_fk_insert ON telemetry_devices;
DROP TRIGGER IF EXISTS trg_telemetry_devices_tenant_fk_update ON telemetry_devices;
DROP TRIGGER IF EXISTS trg_telemetry_positions_tenant_fk_insert ON telemetry_positions;
DROP TRIGGER IF EXISTS trg_telemetry_positions_tenant_fk_update ON telemetry_positions;
DROP TRIGGER IF EXISTS trg_telemetry_raw_events_tenant_fk_insert ON telemetry_raw_events;
DROP TRIGGER IF EXISTS trg_telemetry_raw_events_tenant_fk_update ON telemetry_raw_events;
DROP TRIGGER IF EXISTS trg_trip_detentions_tenant_fk_insert ON trip_detentions;
DROP TRIGGER IF EXISTS trg_trip_detentions_tenant_fk_update ON trip_detentions;
DROP TRIGGER IF EXISTS trg_trip_feedback_tenant_fk_insert ON trip_feedback;
DROP TRIGGER IF EXISTS trg_trip_feedback_tenant_fk_update ON trip_feedback;
DROP TRIGGER IF EXISTS trg_trips_tenant_fk_insert ON trips;
DROP TRIGGER IF EXISTS trg_trips_tenant_fk_update ON trips;
DROP TRIGGER IF EXISTS trg_users_tenant_fk_insert ON users;
DROP TRIGGER IF EXISTS trg_users_tenant_fk_update ON users;
DROP TRIGGER IF EXISTS trg_vehicle_geofences_tenant_fk_insert ON vehicle_geofences;
DROP TRIGGER IF EXISTS trg_vehicle_geofences_tenant_fk_update ON vehicle_geofences;
DROP TRIGGER IF EXISTS trg_vehicle_latest_position_tenant_fk_insert ON vehicle_latest_position;
DROP TRIGGER IF EXISTS trg_vehicle_latest_position_tenant_fk_update ON vehicle_latest_position;
DROP TRIGGER IF EXISTS trg_vehicles_tenant_fk_insert ON vehicles;
DROP TRIGGER IF EXISTS trg_vehicles_tenant_fk_update ON vehicles;
DROP TRIGGER IF EXISTS trg_alerts_tenant_fk_insert ON alerts;
CREATE TRIGGER trg_alerts_tenant_fk_insert BEFORE INSERT ON alerts
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_alerts_tenant_fk_update ON alerts;
CREATE TRIGGER trg_alerts_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON alerts
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_bookings_tenant_fk_insert ON bookings;
CREATE TRIGGER trg_bookings_tenant_fk_insert BEFORE INSERT ON bookings
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_bookings_tenant_fk_update ON bookings;
CREATE TRIGGER trg_bookings_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON bookings
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_company_config_tenant_fk_insert ON company_config;
CREATE TRIGGER trg_company_config_tenant_fk_insert BEFORE INSERT ON company_config
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_company_config_tenant_fk_update ON company_config;
CREATE TRIGGER trg_company_config_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON company_config
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_credit_debit_notes_tenant_fk_insert ON credit_debit_notes;
CREATE TRIGGER trg_credit_debit_notes_tenant_fk_insert BEFORE INSERT ON credit_debit_notes
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_credit_debit_notes_tenant_fk_update ON credit_debit_notes;
CREATE TRIGGER trg_credit_debit_notes_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON credit_debit_notes
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_customers_tenant_fk_insert ON customers;
CREATE TRIGGER trg_customers_tenant_fk_insert BEFORE INSERT ON customers
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_customers_tenant_fk_update ON customers;
CREATE TRIGGER trg_customers_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON customers
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_device_quarantine_tenant_fk_insert ON device_quarantine;
CREATE TRIGGER trg_device_quarantine_tenant_fk_insert BEFORE INSERT ON device_quarantine
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_device_quarantine_tenant_fk_update ON device_quarantine;
CREATE TRIGGER trg_device_quarantine_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON device_quarantine
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_dispatch_overrides_tenant_fk_insert ON dispatch_overrides;
CREATE TRIGGER trg_dispatch_overrides_tenant_fk_insert BEFORE INSERT ON dispatch_overrides
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_dispatch_overrides_tenant_fk_update ON dispatch_overrides;
CREATE TRIGGER trg_dispatch_overrides_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON dispatch_overrides
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_dispatches_tenant_fk_insert ON dispatches;
CREATE TRIGGER trg_dispatches_tenant_fk_insert BEFORE INSERT ON dispatches
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_dispatches_tenant_fk_update ON dispatches;
CREATE TRIGGER trg_dispatches_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON dispatches
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_driver_advance_requests_tenant_fk_insert ON driver_advance_requests;
CREATE TRIGGER trg_driver_advance_requests_tenant_fk_insert BEFORE INSERT ON driver_advance_requests
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_driver_advance_requests_tenant_fk_update ON driver_advance_requests;
CREATE TRIGGER trg_driver_advance_requests_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON driver_advance_requests
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_driver_expenses_tenant_fk_insert ON driver_expenses;
CREATE TRIGGER trg_driver_expenses_tenant_fk_insert BEFORE INSERT ON driver_expenses
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_driver_expenses_tenant_fk_update ON driver_expenses;
CREATE TRIGGER trg_driver_expenses_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON driver_expenses
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_driver_issues_tenant_fk_insert ON driver_issues;
CREATE TRIGGER trg_driver_issues_tenant_fk_insert BEFORE INSERT ON driver_issues
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_driver_issues_tenant_fk_update ON driver_issues;
CREATE TRIGGER trg_driver_issues_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON driver_issues
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_drivers_tenant_fk_insert ON drivers;
CREATE TRIGGER trg_drivers_tenant_fk_insert BEFORE INSERT ON drivers
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_drivers_tenant_fk_update ON drivers;
CREATE TRIGGER trg_drivers_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON drivers
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_engine_state_tenant_fk_insert ON engine_state;
CREATE TRIGGER trg_engine_state_tenant_fk_insert BEFORE INSERT ON engine_state
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_engine_state_tenant_fk_update ON engine_state;
CREATE TRIGGER trg_engine_state_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON engine_state
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_error_reports_tenant_fk_insert ON error_reports;
CREATE TRIGGER trg_error_reports_tenant_fk_insert BEFORE INSERT ON error_reports
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_error_reports_tenant_fk_update ON error_reports;
CREATE TRIGGER trg_error_reports_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON error_reports
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_eta_history_tenant_fk_insert ON eta_history;
CREATE TRIGGER trg_eta_history_tenant_fk_insert BEFORE INSERT ON eta_history
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_eta_history_tenant_fk_update ON eta_history;
CREATE TRIGGER trg_eta_history_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON eta_history
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_eta_history_monthly_tenant_fk_insert ON eta_history_monthly;
CREATE TRIGGER trg_eta_history_monthly_tenant_fk_insert BEFORE INSERT ON eta_history_monthly
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_eta_history_monthly_tenant_fk_update ON eta_history_monthly;
CREATE TRIGGER trg_eta_history_monthly_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON eta_history_monthly
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_experiment_assignments_tenant_fk_insert ON experiment_assignments;
CREATE TRIGGER trg_experiment_assignments_tenant_fk_insert BEFORE INSERT ON experiment_assignments
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_experiment_assignments_tenant_fk_update ON experiment_assignments;
CREATE TRIGGER trg_experiment_assignments_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON experiment_assignments
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_experiment_events_tenant_fk_insert ON experiment_events;
CREATE TRIGGER trg_experiment_events_tenant_fk_insert BEFORE INSERT ON experiment_events
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_experiment_events_tenant_fk_update ON experiment_events;
CREATE TRIGGER trg_experiment_events_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON experiment_events
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_experiments_spec16_tenant_fk_insert ON experiments_spec16;
CREATE TRIGGER trg_experiments_spec16_tenant_fk_insert BEFORE INSERT ON experiments_spec16
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_experiments_spec16_tenant_fk_update ON experiments_spec16;
CREATE TRIGGER trg_experiments_spec16_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON experiments_spec16
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_fastag_tags_tenant_fk_insert ON fastag_tags;
CREATE TRIGGER trg_fastag_tags_tenant_fk_insert BEFORE INSERT ON fastag_tags
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_fastag_tags_tenant_fk_update ON fastag_tags;
CREATE TRIGGER trg_fastag_tags_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON fastag_tags
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_fastag_transactions_tenant_fk_insert ON fastag_transactions;
CREATE TRIGGER trg_fastag_transactions_tenant_fk_insert BEFORE INSERT ON fastag_transactions
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_fastag_transactions_tenant_fk_update ON fastag_transactions;
CREATE TRIGGER trg_fastag_transactions_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON fastag_transactions
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_feature_flags_tenant_fk_insert ON feature_flags;
CREATE TRIGGER trg_feature_flags_tenant_fk_insert BEFORE INSERT ON feature_flags
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_feature_flags_tenant_fk_update ON feature_flags;
CREATE TRIGGER trg_feature_flags_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON feature_flags
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_founder_audit_tenant_fk_insert ON founder_audit;
CREATE TRIGGER trg_founder_audit_tenant_fk_insert BEFORE INSERT ON founder_audit
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_founder_audit_tenant_fk_update ON founder_audit;
CREATE TRIGGER trg_founder_audit_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON founder_audit
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_founder_signals_tenant_fk_insert ON founder_signals;
CREATE TRIGGER trg_founder_signals_tenant_fk_insert BEFORE INSERT ON founder_signals
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_founder_signals_tenant_fk_update ON founder_signals;
CREATE TRIGGER trg_founder_signals_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON founder_signals
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_fuel_prices_tenant_fk_insert ON fuel_prices;
CREATE TRIGGER trg_fuel_prices_tenant_fk_insert BEFORE INSERT ON fuel_prices
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_fuel_prices_tenant_fk_update ON fuel_prices;
CREATE TRIGGER trg_fuel_prices_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON fuel_prices
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_geofence_events_tenant_fk_insert ON geofence_events;
CREATE TRIGGER trg_geofence_events_tenant_fk_insert BEFORE INSERT ON geofence_events
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_geofence_events_tenant_fk_update ON geofence_events;
CREATE TRIGGER trg_geofence_events_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON geofence_events
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_geofences_tenant_fk_insert ON geofences;
CREATE TRIGGER trg_geofences_tenant_fk_insert BEFORE INSERT ON geofences
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_geofences_tenant_fk_update ON geofences;
CREATE TRIGGER trg_geofences_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON geofences
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_incidents_tenant_fk_insert ON incidents;
CREATE TRIGGER trg_incidents_tenant_fk_insert BEFORE INSERT ON incidents
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_incidents_tenant_fk_update ON incidents;
CREATE TRIGGER trg_incidents_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON incidents
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_invoice_line_items_tenant_fk_insert ON invoice_line_items;
CREATE TRIGGER trg_invoice_line_items_tenant_fk_insert BEFORE INSERT ON invoice_line_items
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_invoice_line_items_tenant_fk_update ON invoice_line_items;
CREATE TRIGGER trg_invoice_line_items_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON invoice_line_items
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_invoice_sequences_tenant_fk_insert ON invoice_sequences;
CREATE TRIGGER trg_invoice_sequences_tenant_fk_insert BEFORE INSERT ON invoice_sequences
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_invoice_sequences_tenant_fk_update ON invoice_sequences;
CREATE TRIGGER trg_invoice_sequences_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON invoice_sequences
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_invoices_tenant_fk_insert ON invoices;
CREATE TRIGGER trg_invoices_tenant_fk_insert BEFORE INSERT ON invoices
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_invoices_tenant_fk_update ON invoices;
CREATE TRIGGER trg_invoices_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON invoices
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_maintenance_records_tenant_fk_insert ON maintenance_records;
CREATE TRIGGER trg_maintenance_records_tenant_fk_insert BEFORE INSERT ON maintenance_records
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_maintenance_records_tenant_fk_update ON maintenance_records;
CREATE TRIGGER trg_maintenance_records_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON maintenance_records
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_money_ledger_tenant_fk_insert ON money_ledger;
CREATE TRIGGER trg_money_ledger_tenant_fk_insert BEFORE INSERT ON money_ledger
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_money_ledger_tenant_fk_update ON money_ledger;
CREATE TRIGGER trg_money_ledger_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON money_ledger
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_note_sequences_tenant_fk_insert ON note_sequences;
CREATE TRIGGER trg_note_sequences_tenant_fk_insert BEFORE INSERT ON note_sequences
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_note_sequences_tenant_fk_update ON note_sequences;
CREATE TRIGGER trg_note_sequences_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON note_sequences
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_offline_sync_log_tenant_fk_insert ON offline_sync_log;
CREATE TRIGGER trg_offline_sync_log_tenant_fk_insert BEFORE INSERT ON offline_sync_log
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_offline_sync_log_tenant_fk_update ON offline_sync_log;
CREATE TRIGGER trg_offline_sync_log_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON offline_sync_log
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_ops_alerts_tenant_fk_insert ON ops_alerts;
CREATE TRIGGER trg_ops_alerts_tenant_fk_insert BEFORE INSERT ON ops_alerts
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_ops_alerts_tenant_fk_update ON ops_alerts;
CREATE TRIGGER trg_ops_alerts_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON ops_alerts
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_payments_tenant_fk_insert ON payments;
CREATE TRIGGER trg_payments_tenant_fk_insert BEFORE INSERT ON payments
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_payments_tenant_fk_update ON payments;
CREATE TRIGGER trg_payments_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON payments
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_pnl_daily_tenant_fk_insert ON pnl_daily;
CREATE TRIGGER trg_pnl_daily_tenant_fk_insert BEFORE INSERT ON pnl_daily
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_pnl_daily_tenant_fk_update ON pnl_daily;
CREATE TRIGGER trg_pnl_daily_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON pnl_daily
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_provider_poll_state_tenant_fk_insert ON provider_poll_state;
CREATE TRIGGER trg_provider_poll_state_tenant_fk_insert BEFORE INSERT ON provider_poll_state
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_provider_poll_state_tenant_fk_update ON provider_poll_state;
CREATE TRIGGER trg_provider_poll_state_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON provider_poll_state
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_route_optimization_jobs_tenant_fk_insert ON route_optimization_jobs;
CREATE TRIGGER trg_route_optimization_jobs_tenant_fk_insert BEFORE INSERT ON route_optimization_jobs
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_route_optimization_jobs_tenant_fk_update ON route_optimization_jobs;
CREATE TRIGGER trg_route_optimization_jobs_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON route_optimization_jobs
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_routes_tenant_fk_insert ON routes;
CREATE TRIGGER trg_routes_tenant_fk_insert BEFORE INSERT ON routes
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_routes_tenant_fk_update ON routes;
CREATE TRIGGER trg_routes_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON routes
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_telemetry_devices_tenant_fk_insert ON telemetry_devices;
CREATE TRIGGER trg_telemetry_devices_tenant_fk_insert BEFORE INSERT ON telemetry_devices
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_telemetry_devices_tenant_fk_update ON telemetry_devices;
CREATE TRIGGER trg_telemetry_devices_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON telemetry_devices
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_telemetry_positions_tenant_fk_insert ON telemetry_positions;
CREATE TRIGGER trg_telemetry_positions_tenant_fk_insert BEFORE INSERT ON telemetry_positions
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_telemetry_positions_tenant_fk_update ON telemetry_positions;
CREATE TRIGGER trg_telemetry_positions_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON telemetry_positions
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_telemetry_raw_events_tenant_fk_insert ON telemetry_raw_events;
CREATE TRIGGER trg_telemetry_raw_events_tenant_fk_insert BEFORE INSERT ON telemetry_raw_events
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_telemetry_raw_events_tenant_fk_update ON telemetry_raw_events;
CREATE TRIGGER trg_telemetry_raw_events_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON telemetry_raw_events
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_trip_detentions_tenant_fk_insert ON trip_detentions;
CREATE TRIGGER trg_trip_detentions_tenant_fk_insert BEFORE INSERT ON trip_detentions
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_trip_detentions_tenant_fk_update ON trip_detentions;
CREATE TRIGGER trg_trip_detentions_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON trip_detentions
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_trip_feedback_tenant_fk_insert ON trip_feedback;
CREATE TRIGGER trg_trip_feedback_tenant_fk_insert BEFORE INSERT ON trip_feedback
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_trip_feedback_tenant_fk_update ON trip_feedback;
CREATE TRIGGER trg_trip_feedback_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON trip_feedback
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_trips_tenant_fk_insert ON trips;
CREATE TRIGGER trg_trips_tenant_fk_insert BEFORE INSERT ON trips
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_trips_tenant_fk_update ON trips;
CREATE TRIGGER trg_trips_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON trips
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_users_tenant_fk_insert ON users;
CREATE TRIGGER trg_users_tenant_fk_insert BEFORE INSERT ON users
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_users_tenant_fk_update ON users;
CREATE TRIGGER trg_users_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON users
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_vehicle_geofences_tenant_fk_insert ON vehicle_geofences;
CREATE TRIGGER trg_vehicle_geofences_tenant_fk_insert BEFORE INSERT ON vehicle_geofences
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_vehicle_geofences_tenant_fk_update ON vehicle_geofences;
CREATE TRIGGER trg_vehicle_geofences_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON vehicle_geofences
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_vehicle_latest_position_tenant_fk_insert ON vehicle_latest_position;
CREATE TRIGGER trg_vehicle_latest_position_tenant_fk_insert BEFORE INSERT ON vehicle_latest_position
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_vehicle_latest_position_tenant_fk_update ON vehicle_latest_position;
CREATE TRIGGER trg_vehicle_latest_position_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON vehicle_latest_position
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_vehicles_tenant_fk_insert ON vehicles;
CREATE TRIGGER trg_vehicles_tenant_fk_insert BEFORE INSERT ON vehicles
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_vehicles_tenant_fk_update ON vehicles;
CREATE TRIGGER trg_vehicles_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON vehicles
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
-- DELETE FROM tenants WHERE id IN ('7','9','other-tenant','another-tenant','tenant-2','tenant-7','tenant-9','tenant-999','tenant-A','tenant-B','tenant-zz','tenant-seq','tenant-cap','tenant-dn','tenant-ledger','tenant-val','tenant-fmt','tenant-loop','tn-b','tn-kpi','tenant-c','tenant-d','tenant-forged','tenant-42','test-tenant');
-- +goose Down
-- Shared guard: one function for all tenant-FK triggers. TG_TABLE_NAME
-- reproduces the sqlite per-table error text.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION tenant_fk_guard_fn()
RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id) THEN
        RAISE EXCEPTION 'FK violation: tenants(id) missing for %.tenant_id', TG_TABLE_NAME;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
-- Recreate 00104 triggers with != '' bypass and re-seed test tenants.

DROP TRIGGER IF EXISTS trg_alerts_tenant_fk_insert ON alerts;
DROP TRIGGER IF EXISTS trg_alerts_tenant_fk_update ON alerts;
DROP TRIGGER IF EXISTS trg_bookings_tenant_fk_insert ON bookings;
DROP TRIGGER IF EXISTS trg_bookings_tenant_fk_update ON bookings;
DROP TRIGGER IF EXISTS trg_company_config_tenant_fk_insert ON company_config;
DROP TRIGGER IF EXISTS trg_company_config_tenant_fk_update ON company_config;
DROP TRIGGER IF EXISTS trg_credit_debit_notes_tenant_fk_insert ON credit_debit_notes;
DROP TRIGGER IF EXISTS trg_credit_debit_notes_tenant_fk_update ON credit_debit_notes;
DROP TRIGGER IF EXISTS trg_customers_tenant_fk_insert ON customers;
DROP TRIGGER IF EXISTS trg_customers_tenant_fk_update ON customers;
DROP TRIGGER IF EXISTS trg_device_quarantine_tenant_fk_insert ON device_quarantine;
DROP TRIGGER IF EXISTS trg_device_quarantine_tenant_fk_update ON device_quarantine;
DROP TRIGGER IF EXISTS trg_dispatch_overrides_tenant_fk_insert ON dispatch_overrides;
DROP TRIGGER IF EXISTS trg_dispatch_overrides_tenant_fk_update ON dispatch_overrides;
DROP TRIGGER IF EXISTS trg_dispatches_tenant_fk_insert ON dispatches;
DROP TRIGGER IF EXISTS trg_dispatches_tenant_fk_update ON dispatches;
DROP TRIGGER IF EXISTS trg_driver_advance_requests_tenant_fk_insert ON driver_advance_requests;
DROP TRIGGER IF EXISTS trg_driver_advance_requests_tenant_fk_update ON driver_advance_requests;
DROP TRIGGER IF EXISTS trg_driver_expenses_tenant_fk_insert ON driver_expenses;
DROP TRIGGER IF EXISTS trg_driver_expenses_tenant_fk_update ON driver_expenses;
DROP TRIGGER IF EXISTS trg_driver_issues_tenant_fk_insert ON driver_issues;
DROP TRIGGER IF EXISTS trg_driver_issues_tenant_fk_update ON driver_issues;
DROP TRIGGER IF EXISTS trg_drivers_tenant_fk_insert ON drivers;
DROP TRIGGER IF EXISTS trg_drivers_tenant_fk_update ON drivers;
DROP TRIGGER IF EXISTS trg_engine_state_tenant_fk_insert ON engine_state;
DROP TRIGGER IF EXISTS trg_engine_state_tenant_fk_update ON engine_state;
DROP TRIGGER IF EXISTS trg_error_reports_tenant_fk_insert ON error_reports;
DROP TRIGGER IF EXISTS trg_error_reports_tenant_fk_update ON error_reports;
DROP TRIGGER IF EXISTS trg_eta_history_tenant_fk_insert ON eta_history;
DROP TRIGGER IF EXISTS trg_eta_history_tenant_fk_update ON eta_history;
DROP TRIGGER IF EXISTS trg_eta_history_monthly_tenant_fk_insert ON eta_history_monthly;
DROP TRIGGER IF EXISTS trg_eta_history_monthly_tenant_fk_update ON eta_history_monthly;
DROP TRIGGER IF EXISTS trg_experiment_assignments_tenant_fk_insert ON experiment_assignments;
DROP TRIGGER IF EXISTS trg_experiment_assignments_tenant_fk_update ON experiment_assignments;
DROP TRIGGER IF EXISTS trg_experiment_events_tenant_fk_insert ON experiment_events;
DROP TRIGGER IF EXISTS trg_experiment_events_tenant_fk_update ON experiment_events;
DROP TRIGGER IF EXISTS trg_experiments_spec16_tenant_fk_insert ON experiments_spec16;
DROP TRIGGER IF EXISTS trg_experiments_spec16_tenant_fk_update ON experiments_spec16;
DROP TRIGGER IF EXISTS trg_fastag_tags_tenant_fk_insert ON fastag_tags;
DROP TRIGGER IF EXISTS trg_fastag_tags_tenant_fk_update ON fastag_tags;
DROP TRIGGER IF EXISTS trg_fastag_transactions_tenant_fk_insert ON fastag_transactions;
DROP TRIGGER IF EXISTS trg_fastag_transactions_tenant_fk_update ON fastag_transactions;
DROP TRIGGER IF EXISTS trg_feature_flags_tenant_fk_insert ON feature_flags;
DROP TRIGGER IF EXISTS trg_feature_flags_tenant_fk_update ON feature_flags;
DROP TRIGGER IF EXISTS trg_founder_audit_tenant_fk_insert ON founder_audit;
DROP TRIGGER IF EXISTS trg_founder_audit_tenant_fk_update ON founder_audit;
DROP TRIGGER IF EXISTS trg_founder_signals_tenant_fk_insert ON founder_signals;
DROP TRIGGER IF EXISTS trg_founder_signals_tenant_fk_update ON founder_signals;
DROP TRIGGER IF EXISTS trg_fuel_prices_tenant_fk_insert ON fuel_prices;
DROP TRIGGER IF EXISTS trg_fuel_prices_tenant_fk_update ON fuel_prices;
DROP TRIGGER IF EXISTS trg_geofence_events_tenant_fk_insert ON geofence_events;
DROP TRIGGER IF EXISTS trg_geofence_events_tenant_fk_update ON geofence_events;
DROP TRIGGER IF EXISTS trg_geofences_tenant_fk_insert ON geofences;
DROP TRIGGER IF EXISTS trg_geofences_tenant_fk_update ON geofences;
DROP TRIGGER IF EXISTS trg_incidents_tenant_fk_insert ON incidents;
DROP TRIGGER IF EXISTS trg_incidents_tenant_fk_update ON incidents;
DROP TRIGGER IF EXISTS trg_invoice_line_items_tenant_fk_insert ON invoice_line_items;
DROP TRIGGER IF EXISTS trg_invoice_line_items_tenant_fk_update ON invoice_line_items;
DROP TRIGGER IF EXISTS trg_invoice_sequences_tenant_fk_insert ON invoice_sequences;
DROP TRIGGER IF EXISTS trg_invoice_sequences_tenant_fk_update ON invoice_sequences;
DROP TRIGGER IF EXISTS trg_invoices_tenant_fk_insert ON invoices;
DROP TRIGGER IF EXISTS trg_invoices_tenant_fk_update ON invoices;
DROP TRIGGER IF EXISTS trg_maintenance_records_tenant_fk_insert ON maintenance_records;
DROP TRIGGER IF EXISTS trg_maintenance_records_tenant_fk_update ON maintenance_records;
DROP TRIGGER IF EXISTS trg_money_ledger_tenant_fk_insert ON money_ledger;
DROP TRIGGER IF EXISTS trg_money_ledger_tenant_fk_update ON money_ledger;
DROP TRIGGER IF EXISTS trg_note_sequences_tenant_fk_insert ON note_sequences;
DROP TRIGGER IF EXISTS trg_note_sequences_tenant_fk_update ON note_sequences;
DROP TRIGGER IF EXISTS trg_offline_sync_log_tenant_fk_insert ON offline_sync_log;
DROP TRIGGER IF EXISTS trg_offline_sync_log_tenant_fk_update ON offline_sync_log;
DROP TRIGGER IF EXISTS trg_ops_alerts_tenant_fk_insert ON ops_alerts;
DROP TRIGGER IF EXISTS trg_ops_alerts_tenant_fk_update ON ops_alerts;
DROP TRIGGER IF EXISTS trg_payments_tenant_fk_insert ON payments;
DROP TRIGGER IF EXISTS trg_payments_tenant_fk_update ON payments;
DROP TRIGGER IF EXISTS trg_pnl_daily_tenant_fk_insert ON pnl_daily;
DROP TRIGGER IF EXISTS trg_pnl_daily_tenant_fk_update ON pnl_daily;
DROP TRIGGER IF EXISTS trg_provider_poll_state_tenant_fk_insert ON provider_poll_state;
DROP TRIGGER IF EXISTS trg_provider_poll_state_tenant_fk_update ON provider_poll_state;
DROP TRIGGER IF EXISTS trg_route_optimization_jobs_tenant_fk_insert ON route_optimization_jobs;
DROP TRIGGER IF EXISTS trg_route_optimization_jobs_tenant_fk_update ON route_optimization_jobs;
DROP TRIGGER IF EXISTS trg_routes_tenant_fk_insert ON routes;
DROP TRIGGER IF EXISTS trg_routes_tenant_fk_update ON routes;
DROP TRIGGER IF EXISTS trg_telemetry_devices_tenant_fk_insert ON telemetry_devices;
DROP TRIGGER IF EXISTS trg_telemetry_devices_tenant_fk_update ON telemetry_devices;
DROP TRIGGER IF EXISTS trg_telemetry_positions_tenant_fk_insert ON telemetry_positions;
DROP TRIGGER IF EXISTS trg_telemetry_positions_tenant_fk_update ON telemetry_positions;
DROP TRIGGER IF EXISTS trg_telemetry_raw_events_tenant_fk_insert ON telemetry_raw_events;
DROP TRIGGER IF EXISTS trg_telemetry_raw_events_tenant_fk_update ON telemetry_raw_events;
DROP TRIGGER IF EXISTS trg_trip_detentions_tenant_fk_insert ON trip_detentions;
DROP TRIGGER IF EXISTS trg_trip_detentions_tenant_fk_update ON trip_detentions;
DROP TRIGGER IF EXISTS trg_trip_feedback_tenant_fk_insert ON trip_feedback;
DROP TRIGGER IF EXISTS trg_trip_feedback_tenant_fk_update ON trip_feedback;
DROP TRIGGER IF EXISTS trg_trips_tenant_fk_insert ON trips;
DROP TRIGGER IF EXISTS trg_trips_tenant_fk_update ON trips;
DROP TRIGGER IF EXISTS trg_users_tenant_fk_insert ON users;
DROP TRIGGER IF EXISTS trg_users_tenant_fk_update ON users;
DROP TRIGGER IF EXISTS trg_vehicle_geofences_tenant_fk_insert ON vehicle_geofences;
DROP TRIGGER IF EXISTS trg_vehicle_geofences_tenant_fk_update ON vehicle_geofences;
DROP TRIGGER IF EXISTS trg_vehicle_latest_position_tenant_fk_insert ON vehicle_latest_position;
DROP TRIGGER IF EXISTS trg_vehicle_latest_position_tenant_fk_update ON vehicle_latest_position;
DROP TRIGGER IF EXISTS trg_vehicles_tenant_fk_insert ON vehicles;
DROP TRIGGER IF EXISTS trg_vehicles_tenant_fk_update ON vehicles;
DROP TRIGGER IF EXISTS trg_alerts_tenant_fk_insert ON alerts;
CREATE TRIGGER trg_alerts_tenant_fk_insert BEFORE INSERT ON alerts
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_alerts_tenant_fk_update ON alerts;
CREATE TRIGGER trg_alerts_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON alerts
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_bookings_tenant_fk_insert ON bookings;
CREATE TRIGGER trg_bookings_tenant_fk_insert BEFORE INSERT ON bookings
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_bookings_tenant_fk_update ON bookings;
CREATE TRIGGER trg_bookings_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON bookings
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_company_config_tenant_fk_insert ON company_config;
CREATE TRIGGER trg_company_config_tenant_fk_insert BEFORE INSERT ON company_config
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_company_config_tenant_fk_update ON company_config;
CREATE TRIGGER trg_company_config_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON company_config
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_credit_debit_notes_tenant_fk_insert ON credit_debit_notes;
CREATE TRIGGER trg_credit_debit_notes_tenant_fk_insert BEFORE INSERT ON credit_debit_notes
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_credit_debit_notes_tenant_fk_update ON credit_debit_notes;
CREATE TRIGGER trg_credit_debit_notes_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON credit_debit_notes
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_customers_tenant_fk_insert ON customers;
CREATE TRIGGER trg_customers_tenant_fk_insert BEFORE INSERT ON customers
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_customers_tenant_fk_update ON customers;
CREATE TRIGGER trg_customers_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON customers
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_device_quarantine_tenant_fk_insert ON device_quarantine;
CREATE TRIGGER trg_device_quarantine_tenant_fk_insert BEFORE INSERT ON device_quarantine
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_device_quarantine_tenant_fk_update ON device_quarantine;
CREATE TRIGGER trg_device_quarantine_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON device_quarantine
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_dispatch_overrides_tenant_fk_insert ON dispatch_overrides;
CREATE TRIGGER trg_dispatch_overrides_tenant_fk_insert BEFORE INSERT ON dispatch_overrides
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_dispatch_overrides_tenant_fk_update ON dispatch_overrides;
CREATE TRIGGER trg_dispatch_overrides_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON dispatch_overrides
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_dispatches_tenant_fk_insert ON dispatches;
CREATE TRIGGER trg_dispatches_tenant_fk_insert BEFORE INSERT ON dispatches
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_dispatches_tenant_fk_update ON dispatches;
CREATE TRIGGER trg_dispatches_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON dispatches
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_driver_advance_requests_tenant_fk_insert ON driver_advance_requests;
CREATE TRIGGER trg_driver_advance_requests_tenant_fk_insert BEFORE INSERT ON driver_advance_requests
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_driver_advance_requests_tenant_fk_update ON driver_advance_requests;
CREATE TRIGGER trg_driver_advance_requests_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON driver_advance_requests
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_driver_expenses_tenant_fk_insert ON driver_expenses;
CREATE TRIGGER trg_driver_expenses_tenant_fk_insert BEFORE INSERT ON driver_expenses
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_driver_expenses_tenant_fk_update ON driver_expenses;
CREATE TRIGGER trg_driver_expenses_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON driver_expenses
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_driver_issues_tenant_fk_insert ON driver_issues;
CREATE TRIGGER trg_driver_issues_tenant_fk_insert BEFORE INSERT ON driver_issues
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_driver_issues_tenant_fk_update ON driver_issues;
CREATE TRIGGER trg_driver_issues_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON driver_issues
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_drivers_tenant_fk_insert ON drivers;
CREATE TRIGGER trg_drivers_tenant_fk_insert BEFORE INSERT ON drivers
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_drivers_tenant_fk_update ON drivers;
CREATE TRIGGER trg_drivers_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON drivers
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_engine_state_tenant_fk_insert ON engine_state;
CREATE TRIGGER trg_engine_state_tenant_fk_insert BEFORE INSERT ON engine_state
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_engine_state_tenant_fk_update ON engine_state;
CREATE TRIGGER trg_engine_state_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON engine_state
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_error_reports_tenant_fk_insert ON error_reports;
CREATE TRIGGER trg_error_reports_tenant_fk_insert BEFORE INSERT ON error_reports
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_error_reports_tenant_fk_update ON error_reports;
CREATE TRIGGER trg_error_reports_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON error_reports
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_eta_history_tenant_fk_insert ON eta_history;
CREATE TRIGGER trg_eta_history_tenant_fk_insert BEFORE INSERT ON eta_history
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_eta_history_tenant_fk_update ON eta_history;
CREATE TRIGGER trg_eta_history_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON eta_history
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_eta_history_monthly_tenant_fk_insert ON eta_history_monthly;
CREATE TRIGGER trg_eta_history_monthly_tenant_fk_insert BEFORE INSERT ON eta_history_monthly
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_eta_history_monthly_tenant_fk_update ON eta_history_monthly;
CREATE TRIGGER trg_eta_history_monthly_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON eta_history_monthly
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_experiment_assignments_tenant_fk_insert ON experiment_assignments;
CREATE TRIGGER trg_experiment_assignments_tenant_fk_insert BEFORE INSERT ON experiment_assignments
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_experiment_assignments_tenant_fk_update ON experiment_assignments;
CREATE TRIGGER trg_experiment_assignments_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON experiment_assignments
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_experiment_events_tenant_fk_insert ON experiment_events;
CREATE TRIGGER trg_experiment_events_tenant_fk_insert BEFORE INSERT ON experiment_events
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_experiment_events_tenant_fk_update ON experiment_events;
CREATE TRIGGER trg_experiment_events_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON experiment_events
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_experiments_spec16_tenant_fk_insert ON experiments_spec16;
CREATE TRIGGER trg_experiments_spec16_tenant_fk_insert BEFORE INSERT ON experiments_spec16
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_experiments_spec16_tenant_fk_update ON experiments_spec16;
CREATE TRIGGER trg_experiments_spec16_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON experiments_spec16
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_fastag_tags_tenant_fk_insert ON fastag_tags;
CREATE TRIGGER trg_fastag_tags_tenant_fk_insert BEFORE INSERT ON fastag_tags
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_fastag_tags_tenant_fk_update ON fastag_tags;
CREATE TRIGGER trg_fastag_tags_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON fastag_tags
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_fastag_transactions_tenant_fk_insert ON fastag_transactions;
CREATE TRIGGER trg_fastag_transactions_tenant_fk_insert BEFORE INSERT ON fastag_transactions
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_fastag_transactions_tenant_fk_update ON fastag_transactions;
CREATE TRIGGER trg_fastag_transactions_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON fastag_transactions
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_feature_flags_tenant_fk_insert ON feature_flags;
CREATE TRIGGER trg_feature_flags_tenant_fk_insert BEFORE INSERT ON feature_flags
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_feature_flags_tenant_fk_update ON feature_flags;
CREATE TRIGGER trg_feature_flags_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON feature_flags
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_founder_audit_tenant_fk_insert ON founder_audit;
CREATE TRIGGER trg_founder_audit_tenant_fk_insert BEFORE INSERT ON founder_audit
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_founder_audit_tenant_fk_update ON founder_audit;
CREATE TRIGGER trg_founder_audit_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON founder_audit
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_founder_signals_tenant_fk_insert ON founder_signals;
CREATE TRIGGER trg_founder_signals_tenant_fk_insert BEFORE INSERT ON founder_signals
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_founder_signals_tenant_fk_update ON founder_signals;
CREATE TRIGGER trg_founder_signals_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON founder_signals
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_fuel_prices_tenant_fk_insert ON fuel_prices;
CREATE TRIGGER trg_fuel_prices_tenant_fk_insert BEFORE INSERT ON fuel_prices
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_fuel_prices_tenant_fk_update ON fuel_prices;
CREATE TRIGGER trg_fuel_prices_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON fuel_prices
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_geofence_events_tenant_fk_insert ON geofence_events;
CREATE TRIGGER trg_geofence_events_tenant_fk_insert BEFORE INSERT ON geofence_events
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_geofence_events_tenant_fk_update ON geofence_events;
CREATE TRIGGER trg_geofence_events_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON geofence_events
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_geofences_tenant_fk_insert ON geofences;
CREATE TRIGGER trg_geofences_tenant_fk_insert BEFORE INSERT ON geofences
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_geofences_tenant_fk_update ON geofences;
CREATE TRIGGER trg_geofences_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON geofences
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_incidents_tenant_fk_insert ON incidents;
CREATE TRIGGER trg_incidents_tenant_fk_insert BEFORE INSERT ON incidents
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_incidents_tenant_fk_update ON incidents;
CREATE TRIGGER trg_incidents_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON incidents
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_invoice_line_items_tenant_fk_insert ON invoice_line_items;
CREATE TRIGGER trg_invoice_line_items_tenant_fk_insert BEFORE INSERT ON invoice_line_items
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_invoice_line_items_tenant_fk_update ON invoice_line_items;
CREATE TRIGGER trg_invoice_line_items_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON invoice_line_items
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_invoice_sequences_tenant_fk_insert ON invoice_sequences;
CREATE TRIGGER trg_invoice_sequences_tenant_fk_insert BEFORE INSERT ON invoice_sequences
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_invoice_sequences_tenant_fk_update ON invoice_sequences;
CREATE TRIGGER trg_invoice_sequences_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON invoice_sequences
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_invoices_tenant_fk_insert ON invoices;
CREATE TRIGGER trg_invoices_tenant_fk_insert BEFORE INSERT ON invoices
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_invoices_tenant_fk_update ON invoices;
CREATE TRIGGER trg_invoices_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON invoices
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_maintenance_records_tenant_fk_insert ON maintenance_records;
CREATE TRIGGER trg_maintenance_records_tenant_fk_insert BEFORE INSERT ON maintenance_records
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_maintenance_records_tenant_fk_update ON maintenance_records;
CREATE TRIGGER trg_maintenance_records_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON maintenance_records
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_money_ledger_tenant_fk_insert ON money_ledger;
CREATE TRIGGER trg_money_ledger_tenant_fk_insert BEFORE INSERT ON money_ledger
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_money_ledger_tenant_fk_update ON money_ledger;
CREATE TRIGGER trg_money_ledger_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON money_ledger
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_note_sequences_tenant_fk_insert ON note_sequences;
CREATE TRIGGER trg_note_sequences_tenant_fk_insert BEFORE INSERT ON note_sequences
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_note_sequences_tenant_fk_update ON note_sequences;
CREATE TRIGGER trg_note_sequences_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON note_sequences
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_offline_sync_log_tenant_fk_insert ON offline_sync_log;
CREATE TRIGGER trg_offline_sync_log_tenant_fk_insert BEFORE INSERT ON offline_sync_log
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_offline_sync_log_tenant_fk_update ON offline_sync_log;
CREATE TRIGGER trg_offline_sync_log_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON offline_sync_log
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_ops_alerts_tenant_fk_insert ON ops_alerts;
CREATE TRIGGER trg_ops_alerts_tenant_fk_insert BEFORE INSERT ON ops_alerts
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_ops_alerts_tenant_fk_update ON ops_alerts;
CREATE TRIGGER trg_ops_alerts_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON ops_alerts
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_payments_tenant_fk_insert ON payments;
CREATE TRIGGER trg_payments_tenant_fk_insert BEFORE INSERT ON payments
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_payments_tenant_fk_update ON payments;
CREATE TRIGGER trg_payments_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON payments
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_pnl_daily_tenant_fk_insert ON pnl_daily;
CREATE TRIGGER trg_pnl_daily_tenant_fk_insert BEFORE INSERT ON pnl_daily
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_pnl_daily_tenant_fk_update ON pnl_daily;
CREATE TRIGGER trg_pnl_daily_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON pnl_daily
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_provider_poll_state_tenant_fk_insert ON provider_poll_state;
CREATE TRIGGER trg_provider_poll_state_tenant_fk_insert BEFORE INSERT ON provider_poll_state
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_provider_poll_state_tenant_fk_update ON provider_poll_state;
CREATE TRIGGER trg_provider_poll_state_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON provider_poll_state
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_route_optimization_jobs_tenant_fk_insert ON route_optimization_jobs;
CREATE TRIGGER trg_route_optimization_jobs_tenant_fk_insert BEFORE INSERT ON route_optimization_jobs
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_route_optimization_jobs_tenant_fk_update ON route_optimization_jobs;
CREATE TRIGGER trg_route_optimization_jobs_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON route_optimization_jobs
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_routes_tenant_fk_insert ON routes;
CREATE TRIGGER trg_routes_tenant_fk_insert BEFORE INSERT ON routes
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_routes_tenant_fk_update ON routes;
CREATE TRIGGER trg_routes_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON routes
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_telemetry_devices_tenant_fk_insert ON telemetry_devices;
CREATE TRIGGER trg_telemetry_devices_tenant_fk_insert BEFORE INSERT ON telemetry_devices
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_telemetry_devices_tenant_fk_update ON telemetry_devices;
CREATE TRIGGER trg_telemetry_devices_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON telemetry_devices
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_telemetry_positions_tenant_fk_insert ON telemetry_positions;
CREATE TRIGGER trg_telemetry_positions_tenant_fk_insert BEFORE INSERT ON telemetry_positions
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_telemetry_positions_tenant_fk_update ON telemetry_positions;
CREATE TRIGGER trg_telemetry_positions_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON telemetry_positions
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_telemetry_raw_events_tenant_fk_insert ON telemetry_raw_events;
CREATE TRIGGER trg_telemetry_raw_events_tenant_fk_insert BEFORE INSERT ON telemetry_raw_events
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_telemetry_raw_events_tenant_fk_update ON telemetry_raw_events;
CREATE TRIGGER trg_telemetry_raw_events_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON telemetry_raw_events
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_trip_detentions_tenant_fk_insert ON trip_detentions;
CREATE TRIGGER trg_trip_detentions_tenant_fk_insert BEFORE INSERT ON trip_detentions
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_trip_detentions_tenant_fk_update ON trip_detentions;
CREATE TRIGGER trg_trip_detentions_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON trip_detentions
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_trip_feedback_tenant_fk_insert ON trip_feedback;
CREATE TRIGGER trg_trip_feedback_tenant_fk_insert BEFORE INSERT ON trip_feedback
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_trip_feedback_tenant_fk_update ON trip_feedback;
CREATE TRIGGER trg_trip_feedback_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON trip_feedback
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_trips_tenant_fk_insert ON trips;
CREATE TRIGGER trg_trips_tenant_fk_insert BEFORE INSERT ON trips
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_trips_tenant_fk_update ON trips;
CREATE TRIGGER trg_trips_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON trips
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_users_tenant_fk_insert ON users;
CREATE TRIGGER trg_users_tenant_fk_insert BEFORE INSERT ON users
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_users_tenant_fk_update ON users;
CREATE TRIGGER trg_users_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON users
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_vehicle_geofences_tenant_fk_insert ON vehicle_geofences;
CREATE TRIGGER trg_vehicle_geofences_tenant_fk_insert BEFORE INSERT ON vehicle_geofences
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_vehicle_geofences_tenant_fk_update ON vehicle_geofences;
CREATE TRIGGER trg_vehicle_geofences_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON vehicle_geofences
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_vehicle_latest_position_tenant_fk_insert ON vehicle_latest_position;
CREATE TRIGGER trg_vehicle_latest_position_tenant_fk_insert BEFORE INSERT ON vehicle_latest_position
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_vehicle_latest_position_tenant_fk_update ON vehicle_latest_position;
CREATE TRIGGER trg_vehicle_latest_position_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON vehicle_latest_position
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_vehicles_tenant_fk_insert ON vehicles;
CREATE TRIGGER trg_vehicles_tenant_fk_insert BEFORE INSERT ON vehicles
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_vehicles_tenant_fk_update ON vehicles;
CREATE TRIGGER trg_vehicles_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON vehicles
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
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
