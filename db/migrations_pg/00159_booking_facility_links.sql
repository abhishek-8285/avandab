-- +goose Up
-- 00159: Booking → facility links for coordinate-based dispatch planning.
-- PG port of 00159_booking_facility_links.sql

ALTER TABLE bookings ADD COLUMN pickup_facility_id TEXT;
ALTER TABLE bookings ADD COLUMN drop_facility_id TEXT;

CREATE INDEX IF NOT EXISTS idx_bookings_pickup_facility
    ON bookings(tenant_id, pickup_facility_id) WHERE pickup_facility_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_bookings_drop_facility
    ON bookings(tenant_id, drop_facility_id) WHERE drop_facility_id IS NOT NULL;

ALTER TABLE bookings ADD CONSTRAINT fk_bookings_pickup_facility
    FOREIGN KEY (pickup_facility_id) REFERENCES facilities(id);
ALTER TABLE bookings ADD CONSTRAINT fk_bookings_drop_facility
    FOREIGN KEY (drop_facility_id) REFERENCES facilities(id);

-- Permission seeding per 00150 backfill rule (PG port).
INSERT INTO permissions (name, description) VALUES
('dispatch:read', 'View dispatch planner runs and plans'),
('dispatch:create', 'Create dispatch planner runs'),
('dispatch:update', 'Solve and modify dispatch planner runs')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name LIKE 'dispatch:%'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name LIKE 'dispatch:%'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name LIKE 'dispatch:%'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (
    SELECT id FROM permissions WHERE name LIKE 'dispatch:%'
);
DELETE FROM permissions WHERE name IN ('dispatch:read', 'dispatch:create', 'dispatch:update');

ALTER TABLE bookings DROP CONSTRAINT IF EXISTS fk_bookings_drop_facility;
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS fk_bookings_pickup_facility;
DROP INDEX IF EXISTS idx_bookings_drop_facility;
DROP INDEX IF EXISTS idx_bookings_pickup_facility;
-- Column drops must come last (indexes/constraints depend on them).
ALTER TABLE bookings DROP COLUMN IF EXISTS drop_facility_id;
ALTER TABLE bookings DROP COLUMN IF EXISTS pickup_facility_id;
