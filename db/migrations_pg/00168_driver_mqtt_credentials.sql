-- PG port of 00168_driver_mqtt_credentials.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE IF NOT EXISTS driver_mqtt_credentials (
    driver_key    TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at    TIMESTAMPTZ,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX IF NOT EXISTS idx_dmc_tenant
    ON driver_mqtt_credentials(tenant_id);

-- +goose Down
DROP INDEX IF EXISTS idx_dmc_tenant;
DROP TABLE IF EXISTS driver_mqtt_credentials;
