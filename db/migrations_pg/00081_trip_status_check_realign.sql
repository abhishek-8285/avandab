-- PG port of 00081_trip_status_check_realign.sql | status: MANUAL | flags: MANUAL-REBUILD | reviewed: YES
-- Native PG port: only the status CHECK changes (column set identical), so
-- DROP/ADD CONSTRAINT suffices — no rebuild, dependents kept.
-- +goose Up
ALTER TABLE trips DROP CONSTRAINT trips_status_check;
ALTER TABLE trips ADD CONSTRAINT trips_status_check CHECK (status IN ('draft', 'scheduled', 'assigned', 'started', 'reached_pickup', 'in_transit', 'delivered', 'completed', 'cancelled'));
-- +goose Down
-- Map newer statuses back to the legacy enum before restoring the old CHECK.
UPDATE trips SET status = 'completed' WHERE status IN ('reached_pickup', 'in_transit', 'delivered');
ALTER TABLE trips DROP CONSTRAINT trips_status_check;
ALTER TABLE trips ADD CONSTRAINT trips_status_check CHECK (status IN ('draft', 'scheduled', 'assigned', 'started', 'completed', 'cancelled'));
