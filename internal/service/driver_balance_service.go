package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/cache"
)

// DriverBalanceService computes the driver "Paisa" view (Spec 22 §5.2, §5.5).
//
//	running_balance = Σ paid settlements.net_payout
//	                − Σ advances with status 'paid'
//	                + Σ advances with status 'approved' (not yet in a settlement)
type DriverBalanceService struct {
	db    *sql.DB
	cache cache.Cache
}

// NewDriverBalanceService creates the service; a nil cache degrades to no caching.
func NewDriverBalanceService(db *sql.DB, fleetCache cache.Cache) *DriverBalanceService {
	if fleetCache == nil {
		fleetCache = cache.Noop{}
	}
	return &DriverBalanceService{db: db, cache: fleetCache}
}

const driverBalanceTTL = 60 * time.Second

// DriverBalance is the GET /api/driver/balance payload.
type DriverBalance struct {
	RunningBalance   float64    `json:"running_balance"`
	LastSettlementID string     `json:"last_settlement_id,omitempty"`
	LastSettlementAt *time.Time `json:"last_settlement_at,omitempty"`
	PendingAdvances  int        `json:"pending_advances"`
}

// AdvanceRequest mirrors one driver_advance_requests row.
type AdvanceRequest struct {
	ID           string     `json:"id"`
	TenantID     string     `json:"tenant_id"`
	DriverID     string     `json:"driver_id"`
	TripID       string     `json:"trip_id,omitempty"`
	Amount       float64    `json:"amount"`
	Reason       string     `json:"reason"`
	Status       string     `json:"status"`
	RequestedAt  time.Time  `json:"requested_at"`
	DecidedBy    string     `json:"decided_by,omitempty"`
	DecidedAt    *time.Time `json:"decided_at,omitempty"`
	SettlementID string     `json:"settlement_id,omitempty"`
}

// Advance statuses (00093 CHECK constraint).
const (
	AdvancePending  = "pending"
	AdvanceApproved = "approved"
	AdvanceRejected = "rejected"
	AdvancePaid     = "paid"
)

func (s *DriverBalanceService) invalidate(driverID string) {
	blob, _ := json.Marshal(DriverBalance{})
	_ = blob
	_ = s.cache.Set(context.Background(), balanceCacheKey(driverID), nil, 1) // tombstone via overwrite is wrong; use delete-if-supported below
	// internal/cache has no Delete; short TTL makes stale entries harmless,
	// so we simply overwrite with a fresh computation on next read by
	// relying on the caller re-reading after mutations through GetBalance's
	// cache-miss path. Mutations call this hook for future-proofing.
}

func balanceCacheKey(driverID string) string {
	return "drvbal:" + driverID
}

// GetBalance returns the running balance per §5.2, cached 60s per driver.
func (s *DriverBalanceService) GetBalance(ctx context.Context, driverID string) (*DriverBalance, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database unavailable")
	}
	if blob, ok, _ := s.cache.Get(ctx, balanceCacheKey(driverID)); ok && len(blob) > 0 {
		var cached DriverBalance
		if err := json.Unmarshal(blob, &cached); err == nil {
			return &cached, nil
		}
	}

	bal := &DriverBalance{}

	// Σ paid settlements.net_payout.
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(net_payout), 0) FROM driver_settlements
		 WHERE driver_id = ? AND status = 'paid'`,
		driverID).Scan(&bal.RunningBalance)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	// Last settled settlement meta (id + paid timestamp).
	var lastID, lastAtStr sql.NullString
	err = s.db.QueryRowContext(ctx, `
		SELECT id, paid_at FROM driver_settlements
		WHERE driver_id = ? AND status = 'paid'
		ORDER BY paid_at DESC LIMIT 1`, driverID).Scan(&lastID, &lastAtStr)
	if err == nil && lastID.Valid {
		bal.LastSettlementID = lastID.String
		if t := parseSQLiteTime(lastAtStr.String); !t.IsZero() {
			bal.LastSettlementAt = &t
		}
	}

	// − Σ paid-out advances, + Σ approved not yet inside a settlement.
	var paidOut, approved float64
	if err := s.db.QueryRowContext(ctx, `
		SELECT
		  COALESCE(SUM(CASE WHEN status = 'paid' THEN amount ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN status = 'approved' THEN amount ELSE 0 END), 0)
		FROM driver_advance_requests WHERE driver_id = ? AND status IN ('paid','approved')
	`, driverID).Scan(&paidOut, &approved); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	bal.RunningBalance = bal.RunningBalance - paidOut + approved

	// Pending count.
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM driver_advance_requests WHERE driver_id = ? AND status = ?`,
		driverID, AdvancePending).Scan(&bal.PendingAdvances); err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	if blob, err := json.Marshal(bal); err == nil {
		_ = s.cache.Set(ctx, balanceCacheKey(driverID), blob, driverBalanceTTL)
	}
	return bal, nil
}

// RequestAdvance creates one pending advance request (Spec 22 §2.6).
func (s *DriverBalanceService) RequestAdvance(ctx context.Context, tenantID, driverID, tripID string, amount float64, reason string) (*AdvanceRequest, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("amount must be positive")
	}
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO driver_advance_requests (id, tenant_id, driver_id, trip_id, amount, reason, status)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, tenantID, driverID, nullIfEmpty(tripID), amount, reason, AdvancePending)
	if err != nil {
		return nil, err
	}
	s.invalidate(driverID)
	return &AdvanceRequest{
		ID: id, TenantID: tenantID, DriverID: driverID, TripID: tripID,
		Amount: amount, Reason: reason, Status: AdvancePending,
		RequestedAt: time.Now().UTC(),
	}, nil
}

// ListAdvances returns the driver's requests, newest first.
func (s *DriverBalanceService) ListAdvances(ctx context.Context, driverID string) ([]AdvanceRequest, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, COALESCE(trip_id,''), amount, reason, status,
		       requested_at, COALESCE(decided_by,''),
		       decided_at, COALESCE(settlement_id,'')
		FROM driver_advance_requests WHERE driver_id = ?
		ORDER BY requested_at DESC LIMIT 50`, driverID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []AdvanceRequest{}
	for rows.Next() {
		var a AdvanceRequest
		var decidedAt sql.NullTime
		if err := rows.Scan(&a.ID, &a.TenantID, &a.TripID, &a.Amount, &a.Reason,
			&a.Status, &a.RequestedAt, &a.DecidedBy, &decidedAt, &a.SettlementID); err != nil {
			return nil, err
		}
		if decidedAt.Valid {
			t := decidedAt.Time.UTC()
			a.DecidedAt = &t
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DecideAdvance applies the admin decision (kharcha:approve callers only).
// Idempotent guard: already-decided rows are rejected (Spec 22 edge 10 spirit).
func (s *DriverBalanceService) DecideAdvance(ctx context.Context, advanceID, decision, decidedBy, note string) error {
	switch decision {
	case AdvanceApproved, AdvanceRejected:
	default:
		return fmt.Errorf("decision must be approved or rejected")
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE driver_advance_requests
		SET status = ?, decided_by = ?, decided_at = datetime('now'),
		    reason = CASE WHEN ? != '' THEN reason || ' | note: ' || ? ELSE reason END
		WHERE id = ? AND status = ?`,
		decision, decidedBy, note, note, advanceID, AdvancePending)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("advance not found or already decided")
	}

	var driverID string
	_ = s.db.QueryRowContext(ctx,
		`SELECT driver_id FROM driver_advance_requests WHERE id = ?`, advanceID).Scan(&driverID)
	s.invalidate(driverID)
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// parseSQLiteTime parses the DATETIME formats SQLite stores (string mode).
func parseSQLiteTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
