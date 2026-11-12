-- +goose Up
-- 00180 — Uptime availability samples for the public /status page.
-- Written by the in-process sampler every UPTIME_SAMPLE_INTERVAL (default 60s).
-- Platform-level signal: deliberately not tenant-scoped.
CREATE TABLE IF NOT EXISTS availability_samples (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    sampled_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    component   TEXT NOT NULL DEFAULT 'overall',
    healthy     BOOLEAN NOT NULL,
    latency_ms  INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_availability_samples_sampled_at
    ON availability_samples(sampled_at);

-- +goose Down
DROP INDEX IF EXISTS idx_availability_samples_sampled_at;
DROP TABLE IF EXISTS availability_samples;
