-- PG port of 00026_trip_execution_timeline.sql | status: MANUAL | flags: MANUAL-REBUILD | reviewed: YES
-- Native PG port: sqlite rebuilds the table (no ALTER CONSTRAINT support);
-- PG uses ADD COLUMN + DROP/ADD CONSTRAINT. Same end state, dependents kept.
-- +goose Up
ALTER TABLE trips
    ADD COLUMN started_at TIMESTAMPTZ,
    ADD COLUMN reached_pickup_at TIMESTAMPTZ,
    ADD COLUMN in_transit_at TIMESTAMPTZ,
    ADD COLUMN delivered_at TIMESTAMPTZ,
    ADD COLUMN completed_at TIMESTAMPTZ;
ALTER TABLE trips DROP CONSTRAINT trips_status_check;
ALTER TABLE trips ADD CONSTRAINT trips_status_check CHECK (status IN ('draft', 'scheduled', 'assigned', 'started', 'reached_pickup', 'in_transit', 'delivered', 'completed', 'cancelled'));
-- +goose Down
ALTER TABLE trips DROP CONSTRAINT trips_status_check;
ALTER TABLE trips ADD CONSTRAINT trips_status_check CHECK (status IN ('draft', 'scheduled', 'assigned', 'started', 'completed', 'cancelled'));
ALTER TABLE trips
    DROP COLUMN completed_at,
    DROP COLUMN delivered_at,
    DROP COLUMN in_transit_at,
    DROP COLUMN reached_pickup_at,
    DROP COLUMN started_at;
