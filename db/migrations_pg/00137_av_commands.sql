-- PG port of 00137_av_commands.sql
-- +goose Up
CREATE TABLE IF NOT EXISTS vehicle_commands (
    id                TEXT PRIMARY KEY,
    tenant_id         TEXT NOT NULL DEFAULT '1',
    vehicle_id        TEXT NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
    command_type      TEXT NOT NULL CHECK (command_type IN ('E_STOP', 'HOLD_POSITION', 'RESUME_MISSION', 'SET_SPEED_LIMIT', 'REROUTE', 'RETURN_TO_BASE')),
    parameters_json   TEXT NOT NULL DEFAULT '{}',
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'transmitted', 'acknowledged', 'executed', 'failed', 'cancelled')),
    issued_by         TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    acknowledged_at   TIMESTAMPTZ,
    executed_at       TIMESTAMPTZ,
    error_message     TEXT
);

CREATE INDEX IF NOT EXISTS idx_vehicle_commands_tenant_veh ON vehicle_commands(tenant_id, vehicle_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_vehicle_commands_status ON vehicle_commands(status);

INSERT INTO roles (name, description) VALUES ('av_operator', 'Autonomous Vehicle Teleoperation & Cockpit Operator') ON CONFLICT (name) DO NOTHING;
INSERT INTO roles (name, description) VALUES ('technician', 'Maintenance and Repair Technician') ON CONFLICT (name) DO NOTHING;

INSERT INTO permissions (name, description) VALUES ('vehicles:command', 'Dispatch drive-by-wire commands to autonomous vehicles') ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r, permissions p
WHERE r.name IN ('admin', 'av_operator') AND p.name IN ('vehicles:command', 'vehicles:read', 'telemetry:read')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r, permissions p
WHERE r.name = 'technician' AND p.name IN ('maintenance:read', 'maintenance:create', 'maintenance:update', 'vehicles:read')
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS vehicle_commands;
