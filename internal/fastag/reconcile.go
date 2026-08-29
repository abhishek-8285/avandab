package fastag

import (
	"context"
	"database/sql"
	"math"
	"time"

	"github.com/google/uuid"

	intFastag "transport-app/internal/integration/fastag"
	"transport-app/internal/shared"
)

type candidateTrip struct {
	ID            string
	DriverID      sql.NullString
	DepartureTime time.Time
	ArrivalTime   time.Time
}

// Reconcile pulls provider transactions, matches them greedily to active trips, and creates driver kharcha records.
func (s *FASTagService) Reconcile(ctx context.Context, vehicleNumber, fromDate, toDate string) (*intFastag.ReconcileResult, error) {
	// 1. Pull provider transactions
	pulledTxs, err := s.client.ListTransactions(ctx, vehicleNumber, 10)
	if err != nil {
		s.logger.Warn("fastag: error pulling provider transactions", "error", err)
	}

	pulledCount := len(pulledTxs)
	tenantID := string(shared.TenantIDFromContext(ctx))
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	// 2. Persist new transactions into fastag_transactions (source='PROVIDER')
	for _, p := range pulledTxs {
		var exists bool
		_ = s.db.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM fastag_transactions WHERE tag_id = ? AND txn_timestamp = ? AND amount = ?)
		`, p.TagID, p.Timestamp, p.Amount).Scan(&exists)

		if !exists {
			txnID := p.TransactionID
			if txnID == "" {
				txnID = uuid.NewString()
			}
			_, _ = s.db.ExecContext(ctx, `
				INSERT INTO fastag_transactions (
					id, tenant_id, tag_id, vehicle_number, plaza_id, plaza_name,
					amount, txn_timestamp, status, source, reconciled
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'SUCCESS', 'PROVIDER', 0)
			`, txnID, tenantID, p.TagID, p.VehicleNumber, p.PlazaID, p.PlazaName, p.Amount, p.Timestamp)
		}
	}

	// 3. Load unreconciled transactions
	query := `
		SELECT id, tag_id, vehicle_number, plaza_id, plaza_name, amount, txn_timestamp
		FROM fastag_transactions
		WHERE (vehicle_number = ? OR ? = '') AND reconciled = 0
		ORDER BY txn_timestamp ASC
	`
	rows, err := s.db.QueryContext(ctx, query, vehicleNumber, vehicleNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type unrecTxn struct {
		ID            string
		TagID         string
		VehicleNumber string
		PlazaID       string
		PlazaName     string
		Amount        float64
		Timestamp     time.Time
	}
	var unreconciled []unrecTxn
	for rows.Next() {
		var u unrecTxn
		var tagID, vehNum, plzID, plzName sql.NullString
		var tsRaw any
		if err := rows.Scan(&u.ID, &tagID, &vehNum, &plzID, &plzName, &u.Amount, &tsRaw); err == nil {
			u.TagID = tagID.String
			u.VehicleNumber = vehNum.String
			u.PlazaID = plzID.String
			u.PlazaName = plzName.String
			u.Timestamp = parseTimeFlexible(tsRaw)
			unreconciled = append(unreconciled, u)
		}
	}

	// 4. Load candidate trips
	tripQuery := `
		SELECT t.id, t.driver_id, t.departure_time, COALESCE(t.arrival_time, datetime('now'))
		FROM trips t
		LEFT JOIN vehicles v ON t.vehicle_id = v.id
		WHERE (v.registration_number = ? OR ? = '' OR t.vehicle_id = ?)
		  AND t.status IN ('scheduled', 'assigned', 'started', 'reached_pickup', 'in_transit', 'delivered', 'completed')
		  AND t.tenant_id = ?
		ORDER BY t.departure_time ASC
	`
	tRows, err := s.db.QueryContext(ctx, tripQuery, vehicleNumber, vehicleNumber, vehicleNumber, tenantID)
	if err != nil {
		return nil, err
	}
	defer tRows.Close()

	var candidateTrips []candidateTrip
	for tRows.Next() {
		var ct candidateTrip
		var depRaw, arrRaw any
		if err := tRows.Scan(&ct.ID, &ct.DriverID, &depRaw, &arrRaw); err == nil {
			ct.DepartureTime = parseTimeFlexible(depRaw)
			ct.ArrivalTime = parseTimeFlexible(arrRaw)
			candidateTrips = append(candidateTrips, ct)
		} else {
			s.logger.Warn("fastag reconcile: trip scan error", "error", err)
		}
	}

	// 5. Greedy matching
	matched := 0
	unmatched := 0
	kharchaCreated := 0
	var unmatchedIDs []string

	for _, txn := range unreconciled {
		var bestTrip *candidateTrip
		minDelta := math.MaxFloat64

		for i := range candidateTrips {
			trip := &candidateTrips[i]
			// Window check: allow transaction within departure-1h to arrival+1h
			startWin := trip.DepartureTime.Add(-1 * time.Hour)
			endWin := trip.ArrivalTime.Add(1 * time.Hour)

			if (txn.Timestamp.After(startWin) || txn.Timestamp.Equal(startWin)) &&
				(txn.Timestamp.Before(endWin) || txn.Timestamp.Equal(endWin)) {
				delta := math.Abs(txn.Timestamp.Sub(trip.DepartureTime).Seconds())
				if delta < minDelta {
					minDelta = delta
					bestTrip = trip
				}
			}
		}

		if bestTrip != nil {
			matched++
			var kharchaID sql.NullString

			// 6. Create driver kharcha if enabled
			if s.config.AutoKharcha {
				kID := uuid.NewString()
				desc := "FASTag toll at " + txn.PlazaName
				if desc == "FASTag toll at " {
					desc = "FASTag Toll Plaza deduction"
				}
				driverIDVal := ""
				if bestTrip.DriverID.Valid {
					driverIDVal = bestTrip.DriverID.String
				}

				_, kErr := s.db.ExecContext(ctx, `
					INSERT INTO driver_expenses (
						id, trip_id, driver_id, expense_type, category, amount, description, approved, status, approved_at, tenant_id
					) VALUES (?, ?, ?, 'toll', 'toll', ?, ?, 1, 'approved', datetime('now'), ?)
				`, kID, bestTrip.ID, driverIDVal, txn.Amount, desc, tenantID)

				if kErr == nil {
					kharchaCreated++
					kharchaID = sql.NullString{String: kID, Valid: true}
				} else {
					s.logger.Warn("fastag: failed to create auto-kharcha", "trip_id", bestTrip.ID, "error", kErr)
				}
			}

			// Mark transaction reconciled
			_, _ = s.db.ExecContext(ctx, `
				UPDATE fastag_transactions
				SET trip_id = ?, reconciled = 1, kharcha_id = ?
				WHERE id = ?
			`, bestTrip.ID, kharchaID, txn.ID)
		} else {
			unmatched++
			unmatchedIDs = append(unmatchedIDs, txn.ID)
		}
	}

	return &intFastag.ReconcileResult{
		Pulled:         pulledCount,
		Matched:        matched,
		Unmatched:      unmatched,
		KharchaCreated: kharchaCreated,
		UnmatchedIDs:   unmatchedIDs,
	}, nil
}

func parseTimeFlexible(val any) time.Time {
	switch v := val.(type) {
	case time.Time:
		return v
	case string:
		formats := []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02 15:04:05.999999999-07:00",
			"2006-01-02 15:04:05.999999999",
			"2006-01-02 15:04:05-07:00",
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
			"2006-01-02",
		}
		for _, f := range formats {
			if t, err := time.Parse(f, v); err == nil {
				return t
			}
		}
	}
	return time.Now()
}
