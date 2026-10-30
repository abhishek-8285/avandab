package rfq

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// RFQ Master CRUD

func (r *Repository) CreateRFQ(ctx context.Context, rfq *RFQ) error {
	query := `INSERT INTO rfqs (
		id, tenant_id, rfq_number, title, origin_city, destination_city,
		vehicle_type_required, cargo_type, cargo_weight_tons, target_rate,
		max_acceptable_rate, pickup_date, delivery_deadline, bid_deadline,
		status, booking_id, sto_id, awarded_bid_id, awarded_carrier_id,
		awarded_carrier_name, awarded_rate, awarded_at, created_by,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(ctx, query,
		rfq.ID,
		rfq.TenantID,
		rfq.RFQNumber,
		rfq.Title,
		rfq.OriginCity,
		rfq.DestinationCity,
		rfq.VehicleTypeRequired,
		rfq.CargoType,
		rfq.CargoWeightTons,
		rfq.TargetRate,
		rfq.MaxAcceptableRate,
		rfq.PickupDate,
		rfq.DeliveryDeadline,
		rfq.BidDeadline,
		string(rfq.Status),
		rfq.BookingID,
		rfq.STOID,
		rfq.AwardedBidID,
		rfq.AwardedCarrierID,
		rfq.AwardedCarrierName,
		rfq.AwardedRate,
		rfq.AwardedAt,
		rfq.CreatedBy,
		rfq.CreatedAt,
		rfq.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert rfq: %w", err)
	}
	return nil
}

func (r *Repository) GetRFQByID(ctx context.Context, tenantID, id string) (*RFQ, error) {
	query := `SELECT 
		r.id, r.tenant_id, r.rfq_number, r.title, r.origin_city, r.destination_city,
		r.vehicle_type_required, r.cargo_type, r.cargo_weight_tons, r.target_rate,
		r.max_acceptable_rate, r.pickup_date, r.delivery_deadline, r.bid_deadline,
		r.status, r.booking_id, r.sto_id, r.awarded_bid_id, r.awarded_carrier_id,
		r.awarded_carrier_name, r.awarded_rate, r.awarded_at, r.created_by,
		r.created_at, r.updated_at,
		(SELECT COUNT(*) FROM rfq_bids b WHERE b.rfq_id = r.id) as bids_count
	FROM rfqs r
	WHERE r.id = ? AND r.tenant_id = ?`

	row := r.db.QueryRowContext(ctx, query, id, tenantID)

	var (
		rfq       RFQ
		status    string
		awardedAt sql.NullTime
	)

	err := row.Scan(
		&rfq.ID,
		&rfq.TenantID,
		&rfq.RFQNumber,
		&rfq.Title,
		&rfq.OriginCity,
		&rfq.DestinationCity,
		&rfq.VehicleTypeRequired,
		&rfq.CargoType,
		&rfq.CargoWeightTons,
		&rfq.TargetRate,
		&rfq.MaxAcceptableRate,
		&rfq.PickupDate,
		&rfq.DeliveryDeadline,
		&rfq.BidDeadline,
		&status,
		&rfq.BookingID,
		&rfq.STOID,
		&rfq.AwardedBidID,
		&rfq.AwardedCarrierID,
		&rfq.AwardedCarrierName,
		&rfq.AwardedRate,
		&awardedAt,
		&rfq.CreatedBy,
		&rfq.CreatedAt,
		&rfq.UpdatedAt,
		&rfq.BidsCount,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to query rfq: %w", err)
	}

	rfq.Status = RFQStatus(status)
	if awardedAt.Valid {
		rfq.AwardedAt = &awardedAt.Time
	}

	return &rfq, nil
}

func (r *Repository) ListRFQs(ctx context.Context, tenantID string, filter ListRFQsFilter) ([]RFQ, int, error) {
	whereClauses := []string{"r.tenant_id = ?"}
	args := []interface{}{tenantID}

	if filter.Status != nil && *filter.Status != "" {
		whereClauses = append(whereClauses, "r.status = ?")
		args = append(args, string(*filter.Status))
	}
	if filter.OriginCity != "" {
		whereClauses = append(whereClauses, "r.origin_city LIKE ?")
		args = append(args, "%"+filter.OriginCity+"%")
	}
	if filter.DestinationCity != "" {
		whereClauses = append(whereClauses, "r.destination_city LIKE ?")
		args = append(args, "%"+filter.DestinationCity+"%")
	}
	if filter.VehicleType != "" {
		whereClauses = append(whereClauses, "r.vehicle_type_required = ?")
		args = append(args, filter.VehicleType)
	}
	if filter.Search != "" {
		whereClauses = append(whereClauses, "(r.title LIKE ? OR r.rfq_number LIKE ?)")
		pattern := "%" + filter.Search + "%"
		args = append(args, pattern, pattern)
	}

	whereSQL := " WHERE " + strings.Join(whereClauses, " AND ")

	countQuery := "SELECT COUNT(*) FROM rfqs r" + whereSQL
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count rfqs: %w", err)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	selectQuery := `SELECT 
		r.id, r.tenant_id, r.rfq_number, r.title, r.origin_city, r.destination_city,
		r.vehicle_type_required, r.cargo_type, r.cargo_weight_tons, r.target_rate,
		r.max_acceptable_rate, r.pickup_date, r.delivery_deadline, r.bid_deadline,
		r.status, r.booking_id, r.sto_id, r.awarded_bid_id, r.awarded_carrier_id,
		r.awarded_carrier_name, r.awarded_rate, r.awarded_at, r.created_by,
		r.created_at, r.updated_at,
		(SELECT COUNT(*) FROM rfq_bids b WHERE b.rfq_id = r.id) as bids_count
	FROM rfqs r` + whereSQL + " ORDER BY r.created_at DESC LIMIT ? OFFSET ?"

	queryArgs := append([]interface{}{}, args...)
	queryArgs = append(queryArgs, limit, offset)

	rows, err := r.db.QueryContext(ctx, selectQuery, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query rfqs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var rfqs []RFQ
	for rows.Next() {
		var (
			rfq       RFQ
			status    string
			awardedAt sql.NullTime
		)
		err := rows.Scan(
			&rfq.ID,
			&rfq.TenantID,
			&rfq.RFQNumber,
			&rfq.Title,
			&rfq.OriginCity,
			&rfq.DestinationCity,
			&rfq.VehicleTypeRequired,
			&rfq.CargoType,
			&rfq.CargoWeightTons,
			&rfq.TargetRate,
			&rfq.MaxAcceptableRate,
			&rfq.PickupDate,
			&rfq.DeliveryDeadline,
			&rfq.BidDeadline,
			&status,
			&rfq.BookingID,
			&rfq.STOID,
			&rfq.AwardedBidID,
			&rfq.AwardedCarrierID,
			&rfq.AwardedCarrierName,
			&rfq.AwardedRate,
			&awardedAt,
			&rfq.CreatedBy,
			&rfq.CreatedAt,
			&rfq.UpdatedAt,
			&rfq.BidsCount,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan rfq: %w", err)
		}
		rfq.Status = RFQStatus(status)
		if awardedAt.Valid {
			rfq.AwardedAt = &awardedAt.Time
		}
		rfqs = append(rfqs, rfq)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("rows error: %w", err)
	}

	return rfqs, total, nil
}

func (r *Repository) UpdateRFQStatus(ctx context.Context, tenantID, id string, status RFQStatus) error {
	query := `UPDATE rfqs SET status = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`
	res, err := r.db.ExecContext(ctx, query, string(status), time.Now().UTC(), id, tenantID)
	if err != nil {
		return fmt.Errorf("failed to update rfq status: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) GetNextRFQNumber(ctx context.Context, tenantID string) (string, error) {
	year := time.Now().UTC().Year()
	prefix := fmt.Sprintf("RFQ-%d-", year)

	var count int
	query := `SELECT COUNT(*) FROM rfqs WHERE tenant_id = ? AND rfq_number LIKE ?`
	if err := r.db.QueryRowContext(ctx, query, tenantID, prefix+"%").Scan(&count); err != nil {
		return "", fmt.Errorf("failed to count rfqs for sequence: %w", err)
	}

	return fmt.Sprintf("%s%05d", prefix, count+1), nil
}

// RFQ Bids CRUD

func (r *Repository) CreateRFQBid(ctx context.Context, bid *RFQBid) error {
	query := `INSERT INTO rfq_bids (
		id, tenant_id, rfq_id, carrier_id, carrier_name,
		bid_amount, transit_days, vehicles_available, remarks,
		status, submitted_at, decided_at, decided_by
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(ctx, query,
		bid.ID,
		bid.TenantID,
		bid.RFQID,
		bid.CarrierID,
		bid.CarrierName,
		bid.BidAmount,
		bid.TransitDays,
		bid.VehiclesAvailable,
		bid.Remarks,
		string(bid.Status),
		bid.SubmittedAt,
		bid.DecidedAt,
		bid.DecidedBy,
	)
	if err != nil {
		return fmt.Errorf("failed to insert rfq bid: %w", err)
	}
	return nil
}

func (r *Repository) GetRFQBidByID(ctx context.Context, tenantID, id string) (*RFQBid, error) {
	query := `SELECT 
		id, tenant_id, rfq_id, carrier_id, carrier_name,
		bid_amount, transit_days, vehicles_available, remarks,
		status, submitted_at, decided_at, decided_by
	FROM rfq_bids
	WHERE id = ? AND tenant_id = ?`

	row := r.db.QueryRowContext(ctx, query, id, tenantID)

	var (
		bid       RFQBid
		status    string
		decidedAt sql.NullTime
	)

	err := row.Scan(
		&bid.ID,
		&bid.TenantID,
		&bid.RFQID,
		&bid.CarrierID,
		&bid.CarrierName,
		&bid.BidAmount,
		&bid.TransitDays,
		&bid.VehiclesAvailable,
		&bid.Remarks,
		&status,
		&bid.SubmittedAt,
		&decidedAt,
		&bid.DecidedBy,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to query rfq bid: %w", err)
	}

	bid.Status = RFQBidStatus(status)
	if decidedAt.Valid {
		bid.DecidedAt = &decidedAt.Time
	}

	return &bid, nil
}

func (r *Repository) ListBidsByRFQ(ctx context.Context, tenantID, rfqID string) ([]RFQBid, error) {
	query := `SELECT 
		id, tenant_id, rfq_id, carrier_id, carrier_name,
		bid_amount, transit_days, vehicles_available, remarks,
		status, submitted_at, decided_at, decided_by
	FROM rfq_bids
	WHERE tenant_id = ? AND rfq_id = ?
	ORDER BY bid_amount ASC, submitted_at ASC`

	rows, err := r.db.QueryContext(ctx, query, tenantID, rfqID)
	if err != nil {
		return nil, fmt.Errorf("failed to query rfq bids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var bids []RFQBid
	for rows.Next() {
		var (
			bid       RFQBid
			status    string
			decidedAt sql.NullTime
		)
		err := rows.Scan(
			&bid.ID,
			&bid.TenantID,
			&bid.RFQID,
			&bid.CarrierID,
			&bid.CarrierName,
			&bid.BidAmount,
			&bid.TransitDays,
			&bid.VehiclesAvailable,
			&bid.Remarks,
			&status,
			&bid.SubmittedAt,
			&decidedAt,
			&bid.DecidedBy,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan rfq bid: %w", err)
		}
		bid.Status = RFQBidStatus(status)
		if decidedAt.Valid {
			bid.DecidedAt = &decidedAt.Time
		}
		bids = append(bids, bid)
	}

	return bids, rows.Err()
}

func (r *Repository) UpdateRFQBidStatus(ctx context.Context, tenantID, bidID string, status RFQBidStatus, decidedBy string) error {
	query := `UPDATE rfq_bids SET status = ?, decided_at = ?, decided_by = ? WHERE id = ? AND tenant_id = ?`
	_, err := r.db.ExecContext(ctx, query, string(status), time.Now().UTC(), decidedBy, bidID, tenantID)
	return err
}

// Counter-Offers CRUD & Multi-Round Negotiation

func (r *Repository) CreateCounterOffer(ctx context.Context, co *CounterOffer) error {
	query := `INSERT INTO counter_offers (
		id, tenant_id, entity_type, rfq_id, listing_id, bid_id,
		round_number, sender_role, sender_id, sender_name,
		offered_amount, remarks, status, expires_at, created_at,
		decided_at, decided_by
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(ctx, query,
		co.ID,
		co.TenantID,
		string(co.EntityType),
		co.RFQID,
		co.ListingID,
		co.BidID,
		co.RoundNumber,
		string(co.SenderRole),
		co.SenderID,
		co.SenderName,
		co.OfferedAmount,
		co.Remarks,
		string(co.Status),
		co.ExpiresAt,
		co.CreatedAt,
		co.DecidedAt,
		co.DecidedBy,
	)
	if err != nil {
		return fmt.Errorf("failed to insert counter offer: %w", err)
	}
	return nil
}

func (r *Repository) GetCounterOfferByID(ctx context.Context, tenantID, id string) (*CounterOffer, error) {
	query := `SELECT 
		id, tenant_id, entity_type, rfq_id, listing_id, bid_id,
		round_number, sender_role, sender_id, sender_name,
		offered_amount, remarks, status, expires_at, created_at,
		decided_at, decided_by
	FROM counter_offers
	WHERE id = ? AND tenant_id = ?`

	row := r.db.QueryRowContext(ctx, query, id, tenantID)

	var (
		co        CounterOffer
		entType   string
		role      string
		status    string
		expiresAt sql.NullTime
		decidedAt sql.NullTime
	)

	err := row.Scan(
		&co.ID,
		&co.TenantID,
		&entType,
		&co.RFQID,
		&co.ListingID,
		&co.BidID,
		&co.RoundNumber,
		&role,
		&co.SenderID,
		&co.SenderName,
		&co.OfferedAmount,
		&co.Remarks,
		&status,
		&expiresAt,
		&co.CreatedAt,
		&decidedAt,
		&co.DecidedBy,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to query counter offer: %w", err)
	}

	co.EntityType = EntityType(entType)
	co.SenderRole = SenderRole(role)
	co.Status = CounterOfferStatus(status)
	if expiresAt.Valid {
		co.ExpiresAt = &expiresAt.Time
	}
	if decidedAt.Valid {
		co.DecidedAt = &decidedAt.Time
	}

	return &co, nil
}

func (r *Repository) ListCounterOffersByBid(ctx context.Context, tenantID, bidID string) ([]CounterOffer, error) {
	query := `SELECT 
		id, tenant_id, entity_type, rfq_id, listing_id, bid_id,
		round_number, sender_role, sender_id, sender_name,
		offered_amount, remarks, status, expires_at, created_at,
		decided_at, decided_by
	FROM counter_offers
	WHERE tenant_id = ? AND bid_id = ?
	ORDER BY round_number ASC, created_at ASC`

	rows, err := r.db.QueryContext(ctx, query, tenantID, bidID)
	if err != nil {
		return nil, fmt.Errorf("failed to query counter offers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var counters []CounterOffer
	for rows.Next() {
		var (
			co        CounterOffer
			entType   string
			role      string
			status    string
			expiresAt sql.NullTime
			decidedAt sql.NullTime
		)
		err := rows.Scan(
			&co.ID,
			&co.TenantID,
			&entType,
			&co.RFQID,
			&co.ListingID,
			&co.BidID,
			&co.RoundNumber,
			&role,
			&co.SenderID,
			&co.SenderName,
			&co.OfferedAmount,
			&co.Remarks,
			&status,
			&expiresAt,
			&co.CreatedAt,
			&decidedAt,
			&co.DecidedBy,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan counter offer: %w", err)
		}
		co.EntityType = EntityType(entType)
		co.SenderRole = SenderRole(role)
		co.Status = CounterOfferStatus(status)
		if expiresAt.Valid {
			co.ExpiresAt = &expiresAt.Time
		}
		if decidedAt.Valid {
			co.DecidedAt = &decidedAt.Time
		}
		counters = append(counters, co)
	}

	return counters, rows.Err()
}

func (r *Repository) GetLatestCounterOffer(ctx context.Context, tenantID, bidID string) (*CounterOffer, error) {
	counters, err := r.ListCounterOffersByBid(ctx, tenantID, bidID)
	if err != nil {
		return nil, err
	}
	if len(counters) == 0 {
		return nil, ErrNotFound
	}
	return &counters[len(counters)-1], nil
}

func (r *Repository) SupersedePendingCounters(ctx context.Context, tenantID, bidID string) error {
	query := `UPDATE counter_offers SET status = 'SUPERSEDED' WHERE tenant_id = ? AND bid_id = ? AND status = 'PENDING'`
	_, err := r.db.ExecContext(ctx, query, tenantID, bidID)
	return err
}

func (r *Repository) UpdateCounterOfferStatus(ctx context.Context, tenantID, id string, status CounterOfferStatus, decidedBy string) error {
	query := `UPDATE counter_offers SET status = ?, decided_at = ?, decided_by = ? WHERE id = ? AND tenant_id = ?`
	res, err := r.db.ExecContext(ctx, query, string(status), time.Now().UTC(), decidedBy, id, tenantID)
	if err != nil {
		return fmt.Errorf("failed to update counter offer status: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Awarding Transactions

func (r *Repository) AwardRFQBidTransaction(
	ctx context.Context,
	tenantID, rfqID, bidID string,
	awardedRate float64,
	carrierID, carrierName, userID string,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start award tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()

	// 1. Mark winning bid accepted
	_, err = tx.ExecContext(ctx, `
		UPDATE rfq_bids SET 
			status = 'ACCEPTED',
			bid_amount = ?,
			decided_at = ?,
			decided_by = ?
		WHERE id = ? AND tenant_id = ?`,
		awardedRate, now, userID, bidID, tenantID)
	if err != nil {
		return fmt.Errorf("failed to accept winning bid: %w", err)
	}

	// 2. Reject other bids
	_, err = tx.ExecContext(ctx, `
		UPDATE rfq_bids SET 
			status = 'REJECTED',
			decided_at = ?,
			decided_by = ?
		WHERE rfq_id = ? AND id != ? AND tenant_id = ? AND status IN ('SUBMITTED', 'COUNTER_OFFERED')`,
		now, userID, rfqID, bidID, tenantID)
	if err != nil {
		return fmt.Errorf("failed to reject competitor bids: %w", err)
	}

	// 3. Mark RFQ awarded
	_, err = tx.ExecContext(ctx, `
		UPDATE rfqs SET
			status = 'AWARDED',
			awarded_bid_id = ?,
			awarded_carrier_id = ?,
			awarded_carrier_name = ?,
			awarded_rate = ?,
			awarded_at = ?,
			updated_at = ?
		WHERE id = ? AND tenant_id = ?`,
		bidID, carrierID, carrierName, awardedRate, now, now, rfqID, tenantID)
	if err != nil {
		return fmt.Errorf("failed to update rfq awarded status: %w", err)
	}

	return tx.Commit()
}

func (r *Repository) AwardLoadBoardBidTransaction(
	ctx context.Context,
	tenantID, listingID, bidID string,
	awardedRate float64,
	userID string,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start load board award tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()

	// 1. Mark winning bid accepted
	_, err = tx.ExecContext(ctx, `
		UPDATE load_board_bids SET 
			status = 'ACCEPTED',
			bid_amount = ?,
			decided_at = ?
		WHERE id = ? AND tenant_id = ?`,
		awardedRate, now, bidID, tenantID)
	if err != nil {
		return fmt.Errorf("failed to accept winning load board bid: %w", err)
	}

	// 2. Reject other bids
	_, err = tx.ExecContext(ctx, `
		UPDATE load_board_bids SET 
			status = 'REJECTED',
			decided_at = ?
		WHERE listing_id = ? AND id != ? AND tenant_id = ? AND status = 'SUBMITTED'`,
		now, listingID, bidID, tenantID)
	if err != nil {
		return fmt.Errorf("failed to reject competitor load board bids: %w", err)
	}

	// 3. Mark listing awarded
	_, err = tx.ExecContext(ctx, `
		UPDATE load_board_listings SET
			status = 'AWARDED',
			updated_at = ?
		WHERE id = ? AND tenant_id = ?`,
		now, listingID, tenantID)
	if err != nil {
		return fmt.Errorf("failed to update load board listing awarded status: %w", err)
	}

	return tx.Commit()
}
