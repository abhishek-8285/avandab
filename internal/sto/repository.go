package sto

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Repository defines persistence operations for STOs, LoadBoard Listings and Bids.
type Repository interface {
	CreateSTO(ctx context.Context, sto *StockTransferOrder) error
	GetSTOByID(ctx context.Context, tenantID, id string) (*StockTransferOrder, error)
	UpdateSTOStatus(ctx context.Context, tenantID, id string, status STOStatus) error
	ListSTOs(ctx context.Context, tenantID string, status string, limit, offset int) ([]StockTransferOrder, int, error)

	CreateListing(ctx context.Context, l *LoadBoardListing) error
	GetListingByID(ctx context.Context, tenantID, id string) (*LoadBoardListing, error)
	ListListings(ctx context.Context, tenantID string, status, originCity, destCity string, limit, offset int) ([]LoadBoardListing, int, error)

	CreateBid(ctx context.Context, bid *LoadBoardBid) error
	GetBidByID(ctx context.Context, tenantID, listingID, bidID string) (*LoadBoardBid, error)
	ListBidsForListing(ctx context.Context, tenantID, listingID string) ([]LoadBoardBid, error)

	AwardBidTransaction(ctx context.Context, tenantID, listingID, bidID string) (*AwardResult, error)
}

// SQLRepository implements Repository for SQLite and PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewSQLRepository creates a new SQLRepository.
func NewSQLRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func (r *SQLRepository) CreateSTO(ctx context.Context, s *StockTransferOrder) error {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	now := time.Now()
	s.CreatedAt = now
	s.UpdatedAt = now

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO stock_transfer_orders (
			id, tenant_id, sto_number, origin_facility_id, destination_facility_id,
			material_code, material_description, quantity, uom, required_delivery_date,
			status, notes, created_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		s.ID, s.TenantID, s.STONumber, s.OriginFacilityID, s.DestinationFacilityID,
		s.MaterialCode, s.MaterialDescription, s.Quantity, s.UOM, s.RequiredDeliveryDate,
		s.Status, s.Notes, s.CreatedBy, s.CreatedAt, s.UpdatedAt,
	)
	return err
}

func (r *SQLRepository) GetSTOByID(ctx context.Context, tenantID, id string) (*StockTransferOrder, error) {
	var s StockTransferOrder
	var notes sql.NullString
	var origName, dstName sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT 
			sto.id, sto.tenant_id, sto.sto_number, sto.origin_facility_id, sto.destination_facility_id,
			sto.material_code, sto.material_description, sto.quantity, sto.uom, sto.required_delivery_date,
			sto.status, sto.notes, sto.created_by, sto.created_at, sto.updated_at,
			f1.name as origin_name, f2.name as dest_name
		FROM stock_transfer_orders sto
		LEFT JOIN facilities f1 ON f1.id = sto.origin_facility_id AND f1.tenant_id = sto.tenant_id
		LEFT JOIN facilities f2 ON f2.id = sto.destination_facility_id AND f2.tenant_id = sto.tenant_id
		WHERE sto.tenant_id = $1 AND sto.id = $2`,
		tenantID, id).Scan(
		&s.ID, &s.TenantID, &s.STONumber, &s.OriginFacilityID, &s.DestinationFacilityID,
		&s.MaterialCode, &s.MaterialDescription, &s.Quantity, &s.UOM, &s.RequiredDeliveryDate,
		&s.Status, &notes, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt,
		&origName, &dstName,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("sto not found: %s", id)
		}
		return nil, err
	}
	if notes.Valid {
		s.Notes = notes.String
	}
	if origName.Valid {
		s.OriginFacilityName = origName.String
	}
	if dstName.Valid {
		s.DestFacilityName = dstName.String
	}
	return &s, nil
}

func (r *SQLRepository) UpdateSTOStatus(ctx context.Context, tenantID, id string, status STOStatus) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE stock_transfer_orders
		SET status = $1, updated_at = $2
		WHERE tenant_id = $3 AND id = $4`,
		status, time.Now(), tenantID, id)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("sto not found: %s", id)
	}
	return nil
}

func (r *SQLRepository) ListSTOs(ctx context.Context, tenantID string, status string, limit, offset int) ([]StockTransferOrder, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	whereClause := "WHERE sto.tenant_id = $1"
	args := []interface{}{tenantID}
	idx := 2

	if status != "" {
		whereClause += fmt.Sprintf(" AND sto.status = $%d", idx)
		args = append(args, status)
		idx++
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM stock_transfer_orders sto %s", whereClause)
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := fmt.Sprintf(`
		SELECT 
			sto.id, sto.tenant_id, sto.sto_number, sto.origin_facility_id, sto.destination_facility_id,
			sto.material_code, sto.material_description, sto.quantity, sto.uom, sto.required_delivery_date,
			sto.status, sto.notes, sto.created_by, sto.created_at, sto.updated_at,
			f1.name as origin_name, f2.name as dest_name
		FROM stock_transfer_orders sto
		LEFT JOIN facilities f1 ON f1.id = sto.origin_facility_id AND f1.tenant_id = sto.tenant_id
		LEFT JOIN facilities f2 ON f2.id = sto.destination_facility_id AND f2.tenant_id = sto.tenant_id
		%s
		ORDER BY sto.created_at DESC
		LIMIT $%d OFFSET $%d`, whereClause, idx, idx+1)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var results []StockTransferOrder
	for rows.Next() {
		var s StockTransferOrder
		var notes, origName, dstName sql.NullString
		if err := rows.Scan(
			&s.ID, &s.TenantID, &s.STONumber, &s.OriginFacilityID, &s.DestinationFacilityID,
			&s.MaterialCode, &s.MaterialDescription, &s.Quantity, &s.UOM, &s.RequiredDeliveryDate,
			&s.Status, &notes, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt,
			&origName, &dstName,
		); err != nil {
			return nil, 0, err
		}
		if notes.Valid {
			s.Notes = notes.String
		}
		if origName.Valid {
			s.OriginFacilityName = origName.String
		}
		if dstName.Valid {
			s.DestFacilityName = dstName.String
		}
		results = append(results, s)
	}
	if results == nil {
		results = []StockTransferOrder{}
	}
	return results, total, nil
}

func (r *SQLRepository) CreateListing(ctx context.Context, l *LoadBoardListing) error {
	if l.ID == "" {
		l.ID = uuid.NewString()
	}
	now := time.Now()
	l.CreatedAt = now
	l.UpdatedAt = now

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO load_board_listings (
			id, tenant_id, sto_id, booking_id, origin_city, destination_city,
			vehicle_type_required, target_rate, max_rate, visibility, status,
			expires_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		l.ID, l.TenantID, l.STOID, l.BookingID, l.OriginCity, l.DestinationCity,
		l.VehicleTypeRequired, l.TargetRate, l.MaxRate, l.Visibility, l.Status,
		l.ExpiresAt, l.CreatedAt, l.UpdatedAt,
	)
	return err
}

func (r *SQLRepository) GetListingByID(ctx context.Context, tenantID, id string) (*LoadBoardListing, error) {
	var l LoadBoardListing
	var stoID, bkgID sql.NullString
	var stoNumber sql.NullString
	var bidsCount int

	err := r.db.QueryRowContext(ctx, `
		SELECT 
			lb.id, lb.tenant_id, lb.sto_id, lb.booking_id, lb.origin_city, lb.destination_city,
			lb.vehicle_type_required, lb.target_rate, lb.max_rate, lb.visibility, lb.status,
			lb.expires_at, lb.created_at, lb.updated_at,
			sto.sto_number,
			(SELECT COUNT(*) FROM load_board_bids b WHERE b.listing_id = lb.id) as bids_count
		FROM load_board_listings lb
		LEFT JOIN stock_transfer_orders sto ON sto.id = lb.sto_id
		WHERE (lb.tenant_id = $1 OR lb.visibility IN ('FEDERATED', 'PUBLIC')) AND lb.id = $2`,
		tenantID, id).Scan(
		&l.ID, &l.TenantID, &stoID, &bkgID, &l.OriginCity, &l.DestinationCity,
		&l.VehicleTypeRequired, &l.TargetRate, &l.MaxRate, &l.Visibility, &l.Status,
		&l.ExpiresAt, &l.CreatedAt, &l.UpdatedAt,
		&stoNumber, &bidsCount,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("listing not found: %s", id)
		}
		return nil, err
	}
	if stoID.Valid {
		l.STOID = &stoID.String
	}
	if bkgID.Valid {
		l.BookingID = &bkgID.String
	}
	if stoNumber.Valid {
		l.STONumber = stoNumber.String
	}
	l.BidsCount = bidsCount
	return &l, nil
}

func (r *SQLRepository) ListListings(ctx context.Context, tenantID string, status, originCity, destCity string, limit, offset int) ([]LoadBoardListing, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	whereClause := "WHERE (lb.tenant_id = $1 OR lb.visibility IN ('FEDERATED', 'PUBLIC'))"
	args := []interface{}{tenantID}
	idx := 2

	if status != "" {
		whereClause += fmt.Sprintf(" AND lb.status = $%d", idx)
		args = append(args, status)
		idx++
	}
	if originCity != "" {
		whereClause += fmt.Sprintf(" AND LOWER(lb.origin_city) LIKE LOWER($%d)", idx)
		args = append(args, "%"+originCity+"%")
		idx++
	}
	if destCity != "" {
		whereClause += fmt.Sprintf(" AND LOWER(lb.destination_city) LIKE LOWER($%d)", idx)
		args = append(args, "%"+destCity+"%")
		idx++
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM load_board_listings lb %s", whereClause)
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := fmt.Sprintf(`
		SELECT 
			lb.id, lb.tenant_id, lb.sto_id, lb.booking_id, lb.origin_city, lb.destination_city,
			lb.vehicle_type_required, lb.target_rate, lb.max_rate, lb.visibility, lb.status,
			lb.expires_at, lb.created_at, lb.updated_at,
			sto.sto_number,
			(SELECT COUNT(*) FROM load_board_bids b WHERE b.listing_id = lb.id) as bids_count
		FROM load_board_listings lb
		LEFT JOIN stock_transfer_orders sto ON sto.id = lb.sto_id
		%s
		ORDER BY lb.created_at DESC
		LIMIT $%d OFFSET $%d`, whereClause, idx, idx+1)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var results []LoadBoardListing
	for rows.Next() {
		var l LoadBoardListing
		var stoID, bkgID, stoNumber sql.NullString
		var bidsCount int
		if err := rows.Scan(
			&l.ID, &l.TenantID, &stoID, &bkgID, &l.OriginCity, &l.DestinationCity,
			&l.VehicleTypeRequired, &l.TargetRate, &l.MaxRate, &l.Visibility, &l.Status,
			&l.ExpiresAt, &l.CreatedAt, &l.UpdatedAt,
			&stoNumber, &bidsCount,
		); err != nil {
			return nil, 0, err
		}
		if stoID.Valid {
			l.STOID = &stoID.String
		}
		if bkgID.Valid {
			l.BookingID = &bkgID.String
		}
		if stoNumber.Valid {
			l.STONumber = stoNumber.String
		}
		l.BidsCount = bidsCount
		results = append(results, l)
	}
	if results == nil {
		results = []LoadBoardListing{}
	}
	return results, total, nil
}

func (r *SQLRepository) CreateBid(ctx context.Context, b *LoadBoardBid) error {
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	now := time.Now()
	b.SubmittedAt = now
	b.Status = BidStatusSUBMITTED

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO load_board_bids (
			id, tenant_id, listing_id, carrier_id, carrier_name, bid_amount,
			vehicle_id, driver_id, status, remarks, submitted_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		b.ID, b.TenantID, b.ListingID, b.CarrierID, b.CarrierName, b.BidAmount,
		b.VehicleID, b.DriverID, b.Status, b.Remarks, b.SubmittedAt,
	)
	return err
}

func (r *SQLRepository) GetBidByID(ctx context.Context, tenantID, listingID, bidID string) (*LoadBoardBid, error) {
	var b LoadBoardBid
	var vehID, drvID, remarks sql.NullString
	var decidedAt sql.NullTime

	err := r.db.QueryRowContext(ctx, `
		SELECT 
			id, tenant_id, listing_id, carrier_id, carrier_name, bid_amount,
			vehicle_id, driver_id, status, remarks, submitted_at, decided_at
		FROM load_board_bids
		WHERE tenant_id = $1 AND listing_id = $2 AND id = $3`,
		tenantID, listingID, bidID).Scan(
		&b.ID, &b.TenantID, &b.ListingID, &b.CarrierID, &b.CarrierName, &b.BidAmount,
		&vehID, &drvID, &b.Status, &remarks, &b.SubmittedAt, &decidedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("bid not found: %s", bidID)
		}
		return nil, err
	}
	if vehID.Valid {
		b.VehicleID = &vehID.String
	}
	if drvID.Valid {
		b.DriverID = &drvID.String
	}
	if remarks.Valid {
		b.Remarks = remarks.String
	}
	if decidedAt.Valid {
		b.DecidedAt = &decidedAt.Time
	}
	return &b, nil
}

func (r *SQLRepository) ListBidsForListing(ctx context.Context, tenantID, listingID string) ([]LoadBoardBid, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT 
			id, tenant_id, listing_id, carrier_id, carrier_name, bid_amount,
			vehicle_id, driver_id, status, remarks, submitted_at, decided_at
		FROM load_board_bids
		WHERE tenant_id = $1 AND listing_id = $2
		ORDER BY bid_amount ASC, submitted_at ASC`,
		tenantID, listingID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var bids []LoadBoardBid
	for rows.Next() {
		var b LoadBoardBid
		var vehID, drvID, remarks sql.NullString
		var decidedAt sql.NullTime
		if err := rows.Scan(
			&b.ID, &b.TenantID, &b.ListingID, &b.CarrierID, &b.CarrierName, &b.BidAmount,
			&vehID, &drvID, &b.Status, &remarks, &b.SubmittedAt, &decidedAt,
		); err != nil {
			return nil, err
		}
		if vehID.Valid {
			b.VehicleID = &vehID.String
		}
		if drvID.Valid {
			b.DriverID = &drvID.String
		}
		if remarks.Valid {
			b.Remarks = remarks.String
		}
		if decidedAt.Valid {
			b.DecidedAt = &decidedAt.Time
		}
		bids = append(bids, b)
	}
	if bids == nil {
		bids = []LoadBoardBid{}
	}
	return bids, nil
}

func (r *SQLRepository) AwardBidTransaction(ctx context.Context, tenantID, listingID, bidID string) (*AwardResult, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// 1. Fetch & lock listing
	var currentLBStatus string
	var stoID, bkgID sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT status, sto_id, booking_id
		FROM load_board_listings
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, listingID).Scan(&currentLBStatus, &stoID, &bkgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("listing not found: %s", listingID)
		}
		return nil, err
	}

	if currentLBStatus != string(LBStatusOPEN) && currentLBStatus != string(LBStatusBIDDING) {
		return nil, fmt.Errorf("listing cannot be awarded (current status: %s)", currentLBStatus)
	}

	// 2. Fetch bid
	var winningBid LoadBoardBid
	var vehID, drvID, remarks sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT id, tenant_id, listing_id, carrier_id, carrier_name, bid_amount, vehicle_id, driver_id, status, remarks, submitted_at
		FROM load_board_bids
		WHERE tenant_id = $1 AND listing_id = $2 AND id = $3`,
		tenantID, listingID, bidID).Scan(
		&winningBid.ID, &winningBid.TenantID, &winningBid.ListingID, &winningBid.CarrierID,
		&winningBid.CarrierName, &winningBid.BidAmount, &vehID, &drvID, &winningBid.Status,
		&remarks, &winningBid.SubmittedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("bid not found: %s", bidID)
		}
		return nil, err
	}

	if winningBid.Status != BidStatusSUBMITTED {
		return nil, fmt.Errorf("bid is not open for award (status: %s)", winningBid.Status)
	}

	now := time.Now()
	// 3. Mark winning bid accepted
	_, err = tx.ExecContext(ctx, `
		UPDATE load_board_bids
		SET status = 'ACCEPTED', decided_at = $1
		WHERE tenant_id = $2 AND listing_id = $3 AND id = $4`,
		now, tenantID, listingID, bidID)
	if err != nil {
		return nil, err
	}

	// 4. Mark competing bids rejected
	_, err = tx.ExecContext(ctx, `
		UPDATE load_board_bids
		SET status = 'REJECTED', decided_at = $1
		WHERE tenant_id = $2 AND listing_id = $3 AND id != $4 AND status = 'SUBMITTED'`,
		now, tenantID, listingID, bidID)
	if err != nil {
		return nil, err
	}

	// 5. Mark listing awarded
	res, err := tx.ExecContext(ctx, `
		UPDATE load_board_listings
		SET status = 'AWARDED', updated_at = $1
		WHERE tenant_id = $2 AND id = $3 AND status IN ('OPEN', 'BIDDING')`,
		now, tenantID, listingID)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil || affected == 0 {
		return nil, errors.New("concurrent award conflict detected")
	}

	// 6. Update STO status to ASSIGNED if linked
	var stoIDPtr *string
	if stoID.Valid && stoID.String != "" {
		stoIDPtr = &stoID.String
		_, _ = tx.ExecContext(ctx, `
			UPDATE stock_transfer_orders
			SET status = 'ASSIGNED', updated_at = $1
			WHERE tenant_id = $2 AND id = $3`,
			now, tenantID, stoID.String)
	}

	var bkgIDPtr *string
	if bkgID.Valid && bkgID.String != "" {
		bkgIDPtr = &bkgID.String
	}

	// 7. Spawn Trip if STO or booking linked
	var tripIDPtr *string
	tripID := "trip_lb_" + uuid.NewString()
	driverIDVal := winningBid.DriverID
	vehicleIDVal := winningBid.VehicleID

	// Create trip execution record
	_, err = tx.ExecContext(ctx, `
		INSERT INTO trips (
			id, tenant_id, trip_number, booking_id, driver_id, vehicle_id, route_id,
			departure_time, status, remarks, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, 'route-sto-loadboard',
			$7, 'assigned', $8, $9, $10
		)`,
		tripID, tenantID, "TRIP-"+now.Format("20060102")+"-"+winningBid.CarrierID,
		bkgIDPtr, driverIDVal, vehicleIDVal, now,
		"Awarded from load board listing "+listingID, now, now,
	)
	if err == nil {
		tripIDPtr = &tripID
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	winningBid.Status = BidStatusACCEPTED
	winningBid.DecidedAt = &now

	return &AwardResult{
		ListingID:  listingID,
		WinningBid: winningBid,
		STOID:      stoIDPtr,
		BookingID:  bkgIDPtr,
		TripID:     tripIDPtr,
		AwardedAt:  now,
	}, nil
}
