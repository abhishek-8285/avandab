package sqlite

import "context"

// CountUnassignedBookings returns confirmed/pending bookings with no trip yet.
func (r *SQLRepository) CountUnassignedBookings(ctx context.Context) (int64, error) {
	var n int64
	err := r.queryRow(ctx, `
SELECT COUNT(*)
FROM bookings b
WHERE b.tenant_id = ?
  AND b.status IN ('pending', 'confirmed')
  AND NOT EXISTS (SELECT 1 FROM trips t WHERE t.booking_id = b.id)`,
		tenantIDFromCtx(ctx)).Scan(&n)
	return n, err
}

// CountMaintenanceDue returns vehicles with a maintenance_due date set
// (same semantics as the maintenance page's due list).
func (r *SQLRepository) CountMaintenanceDue(ctx context.Context) (int64, error) {
	var n int64
	err := r.queryRow(ctx, `
SELECT COUNT(*) FROM vehicles WHERE tenant_id = ? AND maintenance_due IS NOT NULL`,
		tenantIDFromCtx(ctx)).Scan(&n)
	return n, err
}

// CountOpenWorkOrders returns non-terminal job cards.
func (r *SQLRepository) CountOpenWorkOrders(ctx context.Context) (int64, error) {
	var n int64
	err := r.queryRow(ctx, `
SELECT COUNT(*) FROM work_orders
WHERE tenant_id = ? AND status IN ('open', 'assigned', 'in_progress', 'on_hold')`,
		tenantIDFromCtx(ctx)).Scan(&n)
	return n, err
}

// CountGarageVehicles returns vehicles marked in-maintenance.
func (r *SQLRepository) CountGarageVehicles(ctx context.Context) (int64, error) {
	var n int64
	err := r.queryRow(ctx, `
SELECT COUNT(*) FROM vehicles WHERE tenant_id = ? AND status = 'maintenance'`,
		tenantIDFromCtx(ctx)).Scan(&n)
	return n, err
}

// CountOpenAlerts returns unactioned alerts (open + escalated).
func (r *SQLRepository) CountOpenAlerts(ctx context.Context) (int64, error) {
	var n int64
	err := r.queryRow(ctx, `
SELECT COUNT(*) FROM alerts WHERE tenant_id = ? AND status IN ('open', 'escalated')`,
		tenantIDFromCtx(ctx)).Scan(&n)
	return n, err
}

// CountActiveDTCs returns unresolved engine-fault events on own-org vehicles.
func (r *SQLRepository) CountActiveDTCs(ctx context.Context) (int64, error) {
	var n int64
	err := r.queryRow(ctx, `
SELECT COUNT(*)
FROM dtc_events d
WHERE d.resolved_at IS NULL
  AND EXISTS (SELECT 1 FROM vehicles v WHERE v.id = d.vehicle_id AND v.tenant_id = ?)`,
		tenantIDFromCtx(ctx)).Scan(&n)
	return n, err
}

// CountExpiringEwaybills returns active EWBs expiring within 8h on own-org
// trips — the same window the ewaybill monitor sweeps (monitor.go,
// processExpiringSoonEWBs). EWBs carry no tenant_id; attribution is via trip.
func (r *SQLRepository) CountExpiringEwaybills(ctx context.Context) (int64, error) {
	var n int64
	err := r.queryRow(ctx, `
SELECT COUNT(*)
FROM eway_bills e
WHERE e.status IN ('active', 'part_a')
  AND e.valid_until > datetime('now')
  AND e.valid_until <= datetime('now', '+8 hours')
  AND EXISTS (SELECT 1 FROM trips t WHERE t.id = e.trip_id AND t.tenant_id = ?)`,
		tenantIDFromCtx(ctx)).Scan(&n)
	return n, err
}

// CountPendingKharcha returns unapproved driver expense claims.
func (r *SQLRepository) CountPendingKharcha(ctx context.Context) (int64, error) {
	var n int64
	err := r.queryRow(ctx, `
SELECT COUNT(*) FROM driver_expenses
WHERE tenant_id = ? AND COALESCE(status, 'pending') = 'pending'`,
		tenantIDFromCtx(ctx)).Scan(&n)
	return n, err
}

// CountLowFastag returns active tags below the attention threshold.
func (r *SQLRepository) CountLowFastag(ctx context.Context, threshold float64) (int64, error) {
	var n int64
	err := r.queryRow(ctx, `
SELECT COUNT(*) FROM fastag_tags
WHERE tenant_id = ? AND status = 'ACTIVE' AND balance < ?`,
		tenantIDFromCtx(ctx), threshold).Scan(&n)
	return n, err
}
