-- +goose Up
-- PG port of 00180_availability_samples.sql
-- Uptime availability samples for the public /status page.
CREATE TABLE IF NOT EXISTS availability_samples (
    id          BIGSERIAL PRIMARY KEY,
    sampled_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    component   TEXT NOT NULL DEFAULT 'overall',
    healthy     BOOLEAN NOT NULL,
    latency_ms  BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_availability_samples_sampled_at
    ON availability_samples(sampled_at);

-- +goose Down
DROP INDEX IF EXISTS idx_availability_samples_sampled_at;
DROP TABLE IF EXISTS availability_samples;
