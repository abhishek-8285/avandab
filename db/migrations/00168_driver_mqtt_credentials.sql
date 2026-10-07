-- +goose Up
-- 00168: per-driver MQTT broker credentials (docs/13 §5.2).
-- The hardened broker runs allow_anonymous false + acl_file with
--   pattern write avandab/telemetry/drivers/%u/gps
-- so the broker itself binds a driver's credential to their own GPS topic
-- (verified on mosquitto 2.1.2: publishing to a sibling driver's topic is
-- dropped by the ACL; a denied QoS1 publish still returns PUBACK RC:0, so
-- nothing on the client side can tell).
--
-- driver_key is the identity the mobile publishes in the topic
-- (drivers.driver_id once a driver row exists, otherwise the auth user id).
-- username is globally UNIQUE on purpose: the broker has one namespace, and a
-- tenant-local driver code reused by another org would otherwise let one org
-- publish as the other.
--
-- Only the mosquitto password-file hash is stored. The plaintext is handed to
-- the device exactly once (GET /api/v1/telemetry/mqtt-credentials) and can be
-- re-issued by rotation; it is never persisted.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS driver_mqtt_credentials (
    driver_key    TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    rotated_at    DATETIME,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_dmc_tenant
    ON driver_mqtt_credentials(tenant_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_dmc_tenant_fk_insert
BEFORE INSERT ON driver_mqtt_credentials
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_mqtt_credentials.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_dmc_tenant_fk_update
BEFORE UPDATE OF tenant_id ON driver_mqtt_credentials
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_mqtt_credentials.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_dmc_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_dmc_tenant_fk_insert;
DROP INDEX IF EXISTS idx_dmc_tenant;
DROP TABLE IF EXISTS driver_mqtt_credentials;
