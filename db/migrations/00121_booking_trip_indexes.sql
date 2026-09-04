-- +goose Up
-- 00121 — Booking/trip list + lookup indexes (P6 scale hardening).
-- Every bookings/trips list query filters (tenant_id, status) and orders by
-- date; FindByBookingID and dispatch conflict checks hit booking_id and
-- (driver_id/vehicle_id, tenant_id). Single-column tenant indexes already
-- exist (00103); these composites turn the tenant+status+date pattern from
-- full scans into index range scans. LIKE '%..%' search terms still scan,
-- but only within the indexed tenant+status slice.

CREATE INDEX IF NOT EXISTS idx_trips_tenant_status_departure
    ON trips(tenant_id, status, departure_time DESC);

CREATE INDEX IF NOT EXISTS idx_bookings_tenant_status_pickup
    ON bookings(tenant_id, status, pickup_date DESC);

CREATE INDEX IF NOT EXISTS idx_trips_booking_tenant
    ON trips(booking_id, tenant_id);

CREATE INDEX IF NOT EXISTS idx_trips_driver_tenant_departure
    ON trips(driver_id, tenant_id, departure_time);

CREATE INDEX IF NOT EXISTS idx_trips_vehicle_tenant_departure
    ON trips(vehicle_id, tenant_id, departure_time);

-- +goose Down
DROP INDEX IF EXISTS idx_trips_vehicle_tenant_departure;
DROP INDEX IF EXISTS idx_trips_driver_tenant_departure;
DROP INDEX IF EXISTS idx_trips_booking_tenant;
DROP INDEX IF EXISTS idx_bookings_tenant_status_pickup;
DROP INDEX IF EXISTS idx_trips_tenant_status_departure;
