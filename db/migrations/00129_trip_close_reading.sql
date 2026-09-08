-- +goose Up
-- 00129 — TMS SOP trip close parity (fleet-registry-sop-parity.md §1
-- follow-up, ZMOTM_MMS pp.6-8): the Trip Close dialog writes close
-- reading/date/time. CompletedAt already records close date/time; this adds
-- the close odometer reading. NULL = closed without a reading (legacy rows
-- and callers that omit it stay valid). Breakdown at close reuses
-- ops_alerts.vehicle_breakdown (00058) — no DDL needed for it.
ALTER TABLE trips ADD COLUMN close_odometer REAL;

-- +goose Down
ALTER TABLE trips DROP COLUMN close_odometer;
