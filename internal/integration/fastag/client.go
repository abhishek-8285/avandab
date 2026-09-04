package fastag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/shared"
)

// Config holds connection settings for the FASTag aggregator API.
type Config struct {
	Endpoint string
	APIKey   string
	Enabled  bool
	UseMock  bool
}

// Balance represents the wallet balance linked to a FASTag.
type Balance struct {
	VehicleNumber string    `json:"vehicle_number"`
	TagID         string    `json:"tag_id"`
	Balance       float64   `json:"balance"`
	Currency      string    `json:"currency"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// DeductTollRequest carries toll deduction inputs.
type DeductTollRequest struct {
	VehicleNumber string  `json:"vehicle_number"`
	TagID         string  `json:"tag_id"`
	PlazaID       string  `json:"plaza_id"`
	PlazaName     string  `json:"plaza_name"`
	Amount        float64 `json:"amount"`
	TripID        string  `json:"trip_id"`
	Source        string  `json:"source"` // PROVIDER, MANUAL, GPS
}

// TollTransaction represents a toll deduction record.
type TollTransaction struct {
	TransactionID string    `json:"transaction_id"`
	VehicleNumber string    `json:"vehicle_number"`
	TagID         string    `json:"tag_id"`
	PlazaID       string    `json:"plaza_id"`
	PlazaName     string    `json:"plaza_name"`
	Amount        float64   `json:"amount"`
	Timestamp     time.Time `json:"timestamp"`
	Status        string    `json:"status"`
}

// ReconcileResult represents outcome of a FASTag toll reconciliation pass.
type ReconcileResult struct {
	Pulled         int      `json:"pulled"`
	Matched        int      `json:"matched"`
	Unmatched      int      `json:"unmatched"`
	KharchaCreated int      `json:"kharcha_created"`
	UnmatchedIDs   []string `json:"unmatched_ids"`
}

// Client defines operations supported by the FASTag aggregator API.
type Client interface {
	GetBalance(ctx context.Context, vehicleNumber, tagID string) (Balance, error)
	DeductToll(ctx context.Context, req DeductTollRequest) (TollTransaction, error)
	ListTransactions(ctx context.Context, vehicleNumber string, limit int) ([]TollTransaction, error)
	Reconcile(ctx context.Context, vehicleNumber string, from, to string) (ReconcileResult, error)
}

type clientImpl struct {
	cfg Config
	db  *sql.DB
}

func (c *clientImpl) GetBalance(ctx context.Context, vehicleNumber, tagID string) (Balance, error) {
	slog.Default().Info("[fastag] GetBalance called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "vehicle", vehicleNumber, "tag", tagID)
	if !c.cfg.Enabled {
		return Balance{}, fmt.Errorf("fastag integration disabled")
	}

	if c.db != nil {
		var balance float64
		var tagIDDB sql.NullString
		var vehNumDB sql.NullString
		var lastSync sql.NullTime

		var err error
		if vehicleNumber != "" {
			err = c.db.QueryRowContext(ctx, `
				SELECT tag_id, vehicle_number, balance, last_sync
				FROM fastag_tags
				WHERE vehicle_number = ? OR vehicle_id = ?
				LIMIT 1
			`, vehicleNumber, vehicleNumber).Scan(&tagIDDB, &vehNumDB, &balance, &lastSync)
		} else if tagID != "" {
			err = c.db.QueryRowContext(ctx, `
				SELECT tag_id, vehicle_number, balance, last_sync
				FROM fastag_tags
				WHERE tag_id = ?
				LIMIT 1
			`, tagID).Scan(&tagIDDB, &vehNumDB, &balance, &lastSync)
		} else {
			err = errors.New("neither vehicle_number nor tag_id provided")
		}

		if err == nil {
			syncTime := time.Now()
			if lastSync.Valid {
				syncTime = lastSync.Time
			}
			return Balance{
				VehicleNumber: vehNumDB.String,
				TagID:         tagIDDB.String,
				Balance:       balance,
				Currency:      "INR",
				UpdatedAt:     syncTime,
			}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Balance{}, err
		}
	}
	// No DB row. Never fabricate a balance outside explicit demo mode:
	// invented balances have real operational consequences.
	if !c.cfg.UseMock {
		return Balance{}, fmt.Errorf("fastag: no tag record for vehicle %q / tag %q", vehicleNumber, tagID)
	}

	return Balance{
		VehicleNumber: vehicleNumber,
		TagID:         tagID,
		Balance:       2475.50,
		Currency:      "INR",
		UpdatedAt:     time.Now(),
	}, nil
}

func (c *clientImpl) DeductToll(ctx context.Context, req DeductTollRequest) (TollTransaction, error) {
	slog.Default().Info("[fastag] DeductToll called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "vehicle", req.VehicleNumber, "plaza", req.PlazaName, "amount", req.Amount)
	if !c.cfg.Enabled {
		return TollTransaction{}, fmt.Errorf("fastag integration disabled")
	}

	txnID := uuid.New().String()
	now := time.Now()

	if c.db != nil {
		source := req.Source
		if source == "" {
			source = "MANUAL"
		}
		tenantID := string(shared.TenantIDFromContext(ctx))
		if tenantID == "" && req.TripID != "" {
			// Toll money must land in the trip owner's org.
			_ = c.db.QueryRowContext(ctx, `SELECT tenant_id FROM trips WHERE id = ?`, req.TripID).Scan(&tenantID)
		}
		if tenantID == "" && req.TagID != "" {
			_ = c.db.QueryRowContext(ctx, `SELECT tenant_id FROM fastag_tags WHERE tag_id = ? OR id = ?`, req.TagID, req.TagID).Scan(&tenantID)
		}
		if tenantID == "" {
			return TollTransaction{}, fmt.Errorf("fastag: cannot record toll without tenant (trip %q tag %q)", req.TripID, req.TagID)
		}
		_, err := c.db.ExecContext(ctx, `
			INSERT INTO fastag_transactions (
				id, tenant_id, tag_id, vehicle_number, trip_id, plaza_id, plaza_name,
				amount, txn_timestamp, status, source, reconciled
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'SUCCESS', ?, 0)
		`, txnID, string(tenantID), req.TagID, req.VehicleNumber, req.TripID, req.PlazaID, req.PlazaName, req.Amount, now, source)
		if err != nil {
			slog.Default().Warn("fastag: could not persist deduction txn", "error", err)
		} else {
			// Decrement balance in fastag_tags
			_, _ = c.db.ExecContext(ctx, `
				UPDATE fastag_tags
				SET balance = balance - ?, last_sync = ?
				WHERE tag_id = ? OR vehicle_number = ?
			`, req.Amount, now, req.TagID, req.VehicleNumber)
		}
	}

	return TollTransaction{
		TransactionID: txnID,
		VehicleNumber: req.VehicleNumber,
		TagID:         req.TagID,
		PlazaID:       req.PlazaID,
		PlazaName:     req.PlazaName,
		Amount:        req.Amount,
		Timestamp:     now,
		Status:        "SUCCESS",
	}, nil
}

func (c *clientImpl) ListTransactions(ctx context.Context, vehicleNumber string, limit int) ([]TollTransaction, error) {
	slog.Default().Info("[fastag] ListTransactions called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "vehicle", vehicleNumber, "limit", limit)
	if !c.cfg.Enabled {
		return nil, fmt.Errorf("fastag integration disabled")
	}
	if limit <= 0 {
		limit = 10
	}

	if c.db != nil {
		rows, err := c.db.QueryContext(ctx, `
			SELECT id, tag_id, vehicle_number, plaza_id, plaza_name, amount, txn_timestamp, status
			FROM fastag_transactions
			WHERE vehicle_number = ? OR ? = ''
			ORDER BY txn_timestamp DESC
			LIMIT ?
		`, vehicleNumber, vehicleNumber, limit)
		if err == nil {
			defer rows.Close()
			var list []TollTransaction
			for rows.Next() {
				var t TollTransaction
				var tagID, vehNum, plzID, plzName, status sql.NullString
				if err := rows.Scan(&t.TransactionID, &tagID, &vehNum, &plzID, &plzName, &t.Amount, &t.Timestamp, &status); err == nil {
					t.TagID = tagID.String
					t.VehicleNumber = vehNum.String
					t.PlazaID = plzID.String
					t.PlazaName = plzName.String
					t.Status = status.String
					list = append(list, t)
				}
			}
			if len(list) > 0 {
				return list, nil
			}
		}
	}

	// Empty DB is a valid state — return no transactions rather than
	// inventing plazas and amounts that downstream reconciliation would
	// treat as real toll spend.
	if !c.cfg.UseMock {
		return []TollTransaction{}, nil
	}

	now := time.Now()
	txs := make([]TollTransaction, limit)
	for i := 0; i < limit; i++ {
		txs[i] = TollTransaction{
			TransactionID: uuid.New().String(),
			VehicleNumber: vehicleNumber,
			TagID:         "TAG" + vehicleNumber,
			PlazaID:       fmt.Sprintf("PLZ%03d", i+1),
			PlazaName:     fmt.Sprintf("Toll Plaza %d", i+1),
			Amount:        85.00 + float64(i)*5,
			Timestamp:     now.Add(-time.Duration(i) * time.Hour),
			Status:        "SUCCESS",
		}
	}
	return txs, nil
}

func (c *clientImpl) Reconcile(ctx context.Context, vehicleNumber string, from, to string) (ReconcileResult, error) {
	slog.Default().Info("[fastag] Reconcile called on client", "vehicle", vehicleNumber, "from", from, "to", to)
	// Honest stub: the local reconciliation engine (internal/fastag)
	// performs real matching against the transactions table; this
	// client-level method only proxies a provider API. Never report fake
	// pull/match counts.
	if !c.cfg.UseMock {
		return ReconcileResult{}, fmt.Errorf("fastag: client-level reconcile requires a provider integration; use the reconciliation service")
	}
	return ReconcileResult{}, nil
}
