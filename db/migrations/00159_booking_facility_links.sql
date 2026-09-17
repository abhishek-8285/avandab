-- +goose Up
-- 00159: Booking → facility links for coordinate-based dispatch planning.
-- Spec: docs/design/dispatcher-route-planner/03-cto-architecture.md §2/§7 (P0)
-- Bookings historically carry no coordinates; the planner resolves stop
-- coords by joining these to facilities(id) (00145).
-- Trigger-based FK enforcement per 00103+ rule (SQLite cannot ALTER ADD FK).

ALTER TABLE bookings ADD COLUMN pickup_facility_id TEXT;
ALTER TABLE bookings ADD COLUMN drop_facility_id TEXT;

CREATE INDEX IF NOT EXISTS idx_bookings_pickup_facility
    ON bookings(tenant_id, pickup_facility_id) WHERE pickup_facility_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_bookings_drop_facility
    ON bookings(tenant_id, drop_facility_id) WHERE drop_facility_id IS NOT NULL;

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_bookings_pickup_facility_fk
BEFORE INSERT ON bookings
FOR EACH ROW WHEN NEW.pickup_facility_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM facilities WHERE id = NEW.pickup_facility_id
      AND tenant_id = NEW.tenant_id
  ) THEN RAISE(ABORT, 'FK violation: facilities(id) missing for bookings.pickup_facility_id') END;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_bookings_pickup_facility_fk_update
BEFORE UPDATE OF pickup_facility_id ON bookings
FOR EACH ROW WHEN NEW.pickup_facility_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM facilities WHERE id = NEW.pickup_facility_id
      AND tenant_id = NEW.tenant_id
  ) THEN RAISE(ABORT, 'FK violation: facilities(id) missing for bookings.pickup_facility_id') END;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_bookings_drop_facility_fk
BEFORE INSERT ON bookings
FOR EACH ROW WHEN NEW.drop_facility_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM facilities WHERE id = NEW.drop_facility_id
      AND tenant_id = NEW.tenant_id
  ) THEN RAISE(ABORT, 'FK violation: facilities(id) missing for bookings.drop_facility_id') END;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_bookings_drop_facility_fk_update
BEFORE UPDATE OF drop_facility_id ON bookings
FOR EACH ROW WHEN NEW.drop_facility_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM facilities WHERE id = NEW.drop_facility_id
      AND tenant_id = NEW.tenant_id
  ) THEN RAISE(ABORT, 'FK violation: facilities(id) missing for bookings.drop_facility_id') END;
END;
-- +goose StatementEnd

-- Permission seeding per 00150 backfill rule: every guard string used by
-- ResourcePermission must exist as a permissions row with role grants.
-- Route guards: internal/handlers/dispatch_planner.go (dispatch:read/create/update).
INSERT OR IGNORE INTO permissions (name, description) VALUES
('dispatch:read', 'View dispatch planner runs and plans'),
('dispatch:create', 'Create dispatch planner runs'),
('dispatch:update', 'Solve and modify dispatch planner runs');

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name LIKE 'dispatch:%';

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name LIKE 'dispatch:%';

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name LIKE 'dispatch:%';

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (
    SELECT id FROM permissions WHERE name LIKE 'dispatch:%'
);
DELETE FROM permissions WHERE name IN ('dispatch:read', 'dispatch:create', 'dispatch:update');

DROP TRIGGER IF EXISTS trg_bookings_drop_facility_fk_update;
DROP TRIGGER IF EXISTS trg_bookings_drop_facility_fk;
DROP TRIGGER IF EXISTS trg_bookings_pickup_facility_fk_update;
DROP TRIGGER IF EXISTS trg_bookings_pickup_facility_fk;
DROP INDEX IF EXISTS idx_bookings_drop_facility;
DROP INDEX IF EXISTS idx_bookings_pickup_facility;
ALTER TABLE bookings DROP COLUMN drop_facility_id;
ALTER TABLE bookings DROP COLUMN pickup_facility_id;
