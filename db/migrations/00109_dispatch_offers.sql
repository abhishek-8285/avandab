-- +goose Up
CREATE TABLE IF NOT EXISTS dispatch_offers (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    booking_id TEXT NOT NULL,
    driver_id TEXT NOT NULL,
    vehicle_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('offered', 'accepted', 'rejected', 'expired', 'cancelled')),
    offered_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    responded_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_dispatch_offers_driver ON dispatch_offers(tenant_id, driver_id, status);
CREATE INDEX IF NOT EXISTS idx_dispatch_offers_booking ON dispatch_offers(tenant_id, booking_id, status);

CREATE TABLE IF NOT EXISTS driver_commands (
    command_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    driver_id TEXT NOT NULL,
    command_type TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('processed', 'failed')),
    response_payload TEXT,
    executed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_driver_commands_driver ON driver_commands(tenant_id, driver_id, executed_at);

-- +goose Down
DROP TABLE IF EXISTS driver_commands;
DROP TABLE IF EXISTS dispatch_offers;
