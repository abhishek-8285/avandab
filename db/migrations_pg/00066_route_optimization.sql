-- PG port of 00066_route_optimization.sql | status: PORTABLE | flags: none
-- +goose Up
-- 00066: Route optimization jobs + constraints (Spec 18 Wave A)
-- Stores VRP inputs/outputs, provider-agnostic. No FK to routes — shipments are ad-hoc.

CREATE TABLE IF NOT EXISTS route_optimization_jobs (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    input_json TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','completed','failed')),
    result_json TEXT,
    error_message TEXT,
    provider TEXT NOT NULL DEFAULT 'mock',
    created_at TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    completed_at TIMESTAMPTZ,
    created_by TEXT REFERENCES users(id)
);
CREATE TABLE IF NOT EXISTS route_constraints (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES route_optimization_jobs(id) ON DELETE CASCADE,
    constraint_type TEXT NOT NULL CHECK (constraint_type IN ('time_window','capacity','terrain','driver_hours','skill')),
    constraint_json TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
CREATE INDEX IF NOT EXISTS idx_optimization_jobs_tenant ON route_optimization_jobs(tenant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_optimization_jobs_status ON route_optimization_jobs(status);
CREATE INDEX IF NOT EXISTS idx_route_constraints_job ON route_constraints(job_id);
-- +goose Down
DROP TABLE IF EXISTS route_constraints;
DROP TABLE IF EXISTS route_optimization_jobs;
