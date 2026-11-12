package application

// Driver money read model, extracted from internal/handlers/driver_money.go
// (handlers-end-state ADR, Phase 2). The implicit driver money ledger lives in
// the untouched service.DriverBalanceService; only the per-driver settlement
// list read moved here. The legacy SQL and row mapping are preserved verbatim,
// including the historical quirk that trip_id is surfaced under the "period"
// JSON key by the handler.

import (
	"context"
	"database/sql"
	"time"
)

// DriverSettlementItem is one row of the driver's settlement history.
type DriverSettlementItem struct {
	ID         string
	Period     string // legacy mapping of the settlement trip_id
	Gross      float64
	Deductions float64
	Net        float64
	Status     string
	TDS        float64
	PaidAt     *time.Time
}

// DriverSettlements returns the driver's 50 newest settlements (the query is
// driver-scoped; driver_settlements rows carry the driver id and the legacy
// read never filtered by tenant - preserved verbatim). Scan errors are
// surfaced, matching the old handler's 500 path.
func DriverSettlements(ctx context.Context, db *sql.DB, driverID string) ([]DriverSettlementItem, error) {
	if db == nil {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, trip_id, gross_fare, deductions, net_payout,
		       status, COALESCE(tds_amount,0), paid_at
		FROM driver_settlements WHERE driver_id = $1
		ORDER BY created_at DESC LIMIT 50`, driverID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var settlements []DriverSettlementItem
	for rows.Next() {
		var item DriverSettlementItem
		var period sql.NullString
		var paidAt sql.NullTime
		if err := rows.Scan(&item.ID, &period, &item.Gross, &item.Deductions, &item.Net, &item.Status, &item.TDS, &paidAt); err != nil {
			return nil, err
		}
		item.Period = period.String
		if paidAt.Valid {
			paidAtUTC := paidAt.Time.UTC()
			item.PaidAt = &paidAtUTC
		}
		settlements = append(settlements, item)
	}
	return settlements, nil
}
