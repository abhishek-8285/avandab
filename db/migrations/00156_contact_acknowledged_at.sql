-- +goose Up
-- 00156 — consumer grievance acknowledgement timestamp (E-Commerce
-- Amendment 2026, eff. 1 Jan 2027: acknowledge within 48h, redress within
-- 1 month). NULL = never acknowledged. Set on first admin status touch;
-- contactSLA() prefers it over the status proxy for legacy rows.

ALTER TABLE contact_submissions ADD COLUMN acknowledged_at DATETIME;

-- +goose Down
ALTER TABLE contact_submissions DROP COLUMN acknowledged_at;
