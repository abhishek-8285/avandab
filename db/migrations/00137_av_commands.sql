-- +goose Up
-- 00137: Autonomous vehicle commands table and av_operator role

CREATE TABLE IF NOT EXISTS vehicle_commands (
    id                TEXT PRIMARY KEY,
    tenant_id         TEXT NOT NULL DEFAULT '1',
    vehicle_id        TEXT NOT NULL,
    command_type      TEXT NOT NULL CHECK (command_type IN ('E_STOP', 'HOLD_POSITION', 'RESUME_MISSION', 'SET_SPEED_LIMIT', 'REROUTE', 'RETURN_TO_BASE')),
    parameters_json   TEXT NOT NULL DEFAULT '{}',
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'transmitted', 'acknowledged', 'executed', 'failed', 'cancelled')),
    issued_by         TEXT NOT NULL,
    created_at        DATETIME NOT NULL DEFAULT (datetime('now')),
    acknowledged_at   DATETIME,
    executed_at       DATETIME,
    error_message     TEXT,
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_vehicle_commands_tenant_veh ON vehicle_commands(tenant_id, vehicle_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_vehicle_commands_status ON vehicle_commands(status);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_vehicle_commands_tenant_fk_insert
BEFORE INSERT ON vehicle_commands
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicle_commands.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_vehicle_commands_tenant_fk_update
BEFORE UPDATE OF tenant_id ON vehicle_commands
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicle_commands.tenant_id') END;
END;
-- +goose StatementEnd

-- Seed roles and permissions for AV operations
INSERT OR IGNORE INTO roles (name, description) VALUES ('av_operator', 'Autonomous Vehicle Teleoperation & Cockpit Operator');
INSERT OR IGNORE INTO roles (name, description) VALUES ('technician', 'Maintenance and Repair Technician');

INSERT OR IGNORE INTO permissions (name, description) VALUES ('vehicles:command', 'Dispatch drive-by-wire commands to autonomous vehicles');

-- Assign vehicles:command to admin and av_operator
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r, permissions p
WHERE r.name IN ('admin', 'av_operator') AND p.name IN ('vehicles:command', 'vehicles:read', 'telemetry:read');

-- Assign maintenance permissions to technician
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r, permissions p
WHERE r.name = 'technician' AND p.name IN ('maintenance:read', 'maintenance:create', 'maintenance:update', 'vehicles:read');

-- +goose Down
DROP TRIGGER IF EXISTS trg_vehicle_commands_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_vehicle_commands_tenant_fk_insert;
DROP TABLE IF EXISTS vehicle_commands;
