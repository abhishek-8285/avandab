package freightaudit

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

type TripAuditData struct {
	TripID          string
	VehicleID       string
	VehicleNumber   string
	RouteID         string
	RouteDistanceKM float64
	StandardFare    float64
	StartOdometer   *float64
	CloseOdometer   *float64
	CargoWeightKG   float64
	BookingPrice    float64
	FASTagTolls     float64
}

func (r *Repository) GetTripAuditData(ctx context.Context, tenantID, tripID string) (*TripAuditData, error) {
	query := `SELECT 
		t.id,
		COALESCE(t.vehicle_id, ''),
		COALESCE(v.plate_number, ''),
		COALESCE(t.route_id, ''),
		COALESCE(r.distance, 0.0),
		COALESCE(r.standard_fare, 0.0),
		t.start_odometer,
		t.close_odometer,
		COALESCE(b.cargo_weight, 0.0),
		COALESCE(b.price, 0.0)
	FROM trips t
	LEFT JOIN vehicles v ON v.id = t.vehicle_id AND v.tenant_id = t.tenant_id
	LEFT JOIN routes r ON r.id = t.route_id
	LEFT JOIN bookings b ON b.id = t.booking_id AND b.tenant_id = t.tenant_id
	WHERE t.tenant_id = $1 AND t.id = $2`

	var data TripAuditData
	var startOdo, closeOdo sql.NullFloat64
	err := r.db.QueryRowContext(ctx, query, tenantID, tripID).Scan(
		&data.TripID,
		&data.VehicleID,
		&data.VehicleNumber,
		&data.RouteID,
		&data.RouteDistanceKM,
		&data.StandardFare,
		&startOdo,
		&closeOdo,
		&data.CargoWeightKG,
		&data.BookingPrice,
	)
	if err != nil {
		if errorsIs(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if startOdo.Valid {
		data.StartOdometer = &startOdo.Float64
	}
	if closeOdo.Valid {
		data.CloseOdometer = &closeOdo.Float64
	}

	// FASTag toll transactions sum
	tollQuery := `SELECT COALESCE(SUM(amount), 0.0) FROM fastag_transactions 
		WHERE tenant_id = $1 AND trip_id = $2 AND status = 'SUCCESS'`
	var fastagTolls float64
	if err := r.db.QueryRowContext(ctx, tollQuery, tenantID, tripID).Scan(&fastagTolls); err == nil {
		data.FASTagTolls = fastagTolls
	}

	return &data, nil
}

// Rate Card Repository Methods

func (r *Repository) CreateRateCard(ctx context.Context, rc *RateCard) error {
	query := `INSERT INTO freight_rate_cards (
		id, tenant_id, contract_number, name, vendor_id, customer_id, route_id, vehicle_type,
		billing_basis, rate, min_charge, free_detention_hours, detention_rate_per_hour,
		effective_from, effective_to, is_active, created_at, updated_at
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
	)`

	isActiveInt := 1
	if !rc.IsActive {
		isActiveInt = 0
	}

	_, err := r.db.ExecContext(ctx, query,
		rc.ID, rc.TenantID, rc.ContractNumber, rc.Name, rc.VendorID, rc.CustomerID, rc.RouteID, rc.VehicleType,
		string(rc.BillingBasis), rc.Rate, rc.MinCharge, rc.FreeDetentionHours, rc.DetentionRatePerHour,
		rc.EffectiveFrom, rc.EffectiveTo, isActiveInt, rc.CreatedAt, rc.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "duplicate") {
			return ErrContractNumberExists
		}
		return err
	}
	return nil
}

func (r *Repository) GetRateCardByID(ctx context.Context, tenantID, id string) (*RateCard, error) {
	query := `SELECT 
		id, tenant_id, contract_number, name, vendor_id, customer_id, route_id, vehicle_type,
		billing_basis, rate, min_charge, free_detention_hours, detention_rate_per_hour,
		effective_from, effective_to, is_active, created_at, updated_at
	FROM freight_rate_cards
	WHERE tenant_id = $1 AND id = $2`

	var rc RateCard
	var vendorID, customerID, routeID sql.NullString
	var effectiveTo sql.NullTime
	var isActiveInt int

	err := r.db.QueryRowContext(ctx, query, tenantID, id).Scan(
		&rc.ID, &rc.TenantID, &rc.ContractNumber, &rc.Name, &vendorID, &customerID, &routeID, &rc.VehicleType,
		&rc.BillingBasis, &rc.Rate, &rc.MinCharge, &rc.FreeDetentionHours, &rc.DetentionRatePerHour,
		&rc.EffectiveFrom, &effectiveTo, &isActiveInt, &rc.CreatedAt, &rc.UpdatedAt,
	)
	if err != nil {
		if errorsIs(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if vendorID.Valid {
		rc.VendorID = &vendorID.String
	}
	if customerID.Valid {
		rc.CustomerID = &customerID.String
	}
	if routeID.Valid {
		rc.RouteID = &routeID.String
	}
	if effectiveTo.Valid {
		rc.EffectiveTo = &effectiveTo.Time
	}
	rc.IsActive = isActiveInt == 1

	return &rc, nil
}

func (r *Repository) FindMatchingRateCard(ctx context.Context, tenantID string, routeID, vehicleType string) (*RateCard, error) {
	query := `SELECT 
		id, tenant_id, contract_number, name, vendor_id, customer_id, route_id, vehicle_type,
		billing_basis, rate, min_charge, free_detention_hours, detention_rate_per_hour,
		effective_from, effective_to, is_active, created_at, updated_at
	FROM freight_rate_cards
	WHERE tenant_id = $1 AND is_active = 1
	  AND (route_id = $2 OR route_id IS NULL OR route_id = '')
	  AND (vehicle_type = $3 OR vehicle_type = '')
	ORDER BY 
		CASE WHEN route_id = $2 THEN 1 ELSE 2 END,
		CASE WHEN vehicle_type = $3 THEN 1 ELSE 2 END,
		created_at DESC
	LIMIT 1`

	var rc RateCard
	var vendorID, customerID, rID sql.NullString
	var effectiveTo sql.NullTime
	var isActiveInt int

	err := r.db.QueryRowContext(ctx, query, tenantID, routeID, vehicleType).Scan(
		&rc.ID, &rc.TenantID, &rc.ContractNumber, &rc.Name, &vendorID, &customerID, &rID, &rc.VehicleType,
		&rc.BillingBasis, &rc.Rate, &rc.MinCharge, &rc.FreeDetentionHours, &rc.DetentionRatePerHour,
		&rc.EffectiveFrom, &effectiveTo, &isActiveInt, &rc.CreatedAt, &rc.UpdatedAt,
	)
	if err != nil {
		if errorsIs(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if vendorID.Valid {
		rc.VendorID = &vendorID.String
	}
	if customerID.Valid {
		rc.CustomerID = &customerID.String
	}
	if rID.Valid {
		rc.RouteID = &rID.String
	}
	if effectiveTo.Valid {
		rc.EffectiveTo = &effectiveTo.Time
	}
	rc.IsActive = isActiveInt == 1

	return &rc, nil
}

func (r *Repository) ListRateCards(ctx context.Context, tenantID string, filter RateCardFilter) ([]RateCard, int, error) {
	conditions := []string{"tenant_id = $1"}
	args := []interface{}{tenantID}
	argIdx := 2

	if filter.RouteID != "" {
		conditions = append(conditions, fmt.Sprintf("route_id = $%d", argIdx))
		args = append(args, filter.RouteID)
		argIdx++
	}
	if filter.VehicleType != "" {
		conditions = append(conditions, fmt.Sprintf("vehicle_type = $%d", argIdx))
		args = append(args, filter.VehicleType)
		argIdx++
	}
	if filter.ActiveOnly {
		conditions = append(conditions, "is_active = 1")
	}
	if filter.Search != "" {
		searchTerm := "%" + filter.Search + "%"
		conditions = append(conditions, fmt.Sprintf("(contract_number LIKE $%d OR name LIKE $%d)", argIdx, argIdx))
		args = append(args, searchTerm)
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM freight_rate_cards %s", whereClause)
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	query := fmt.Sprintf(`SELECT 
		id, tenant_id, contract_number, name, vendor_id, customer_id, route_id, vehicle_type,
		billing_basis, rate, min_charge, free_detention_hours, detention_rate_per_hour,
		effective_from, effective_to, is_active, created_at, updated_at
	FROM freight_rate_cards
	%s
	ORDER BY created_at DESC
	LIMIT %d OFFSET %d`, whereClause, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var cards []RateCard
	for rows.Next() {
		var rc RateCard
		var vendorID, customerID, routeID sql.NullString
		var effectiveTo sql.NullTime
		var isActiveInt int

		if err := rows.Scan(
			&rc.ID, &rc.TenantID, &rc.ContractNumber, &rc.Name, &vendorID, &customerID, &routeID, &rc.VehicleType,
			&rc.BillingBasis, &rc.Rate, &rc.MinCharge, &rc.FreeDetentionHours, &rc.DetentionRatePerHour,
			&rc.EffectiveFrom, &effectiveTo, &isActiveInt, &rc.CreatedAt, &rc.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}

		if vendorID.Valid {
			rc.VendorID = &vendorID.String
		}
		if customerID.Valid {
			rc.CustomerID = &customerID.String
		}
		if routeID.Valid {
			rc.RouteID = &routeID.String
		}
		if effectiveTo.Valid {
			rc.EffectiveTo = &effectiveTo.Time
		}
		rc.IsActive = isActiveInt == 1
		cards = append(cards, rc)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return cards, total, nil
}

func (r *Repository) UpdateRateCard(ctx context.Context, rc *RateCard) error {
	query := `UPDATE freight_rate_cards SET
		name = $1, rate = $2, min_charge = $3, free_detention_hours = $4,
		detention_rate_per_hour = $5, effective_to = $6, is_active = $7, updated_at = $8
	WHERE tenant_id = $9 AND id = $10`

	isActiveInt := 1
	if !rc.IsActive {
		isActiveInt = 0
	}

	res, err := r.db.ExecContext(ctx, query,
		rc.Name, rc.Rate, rc.MinCharge, rc.FreeDetentionHours,
		rc.DetentionRatePerHour, rc.EffectiveTo, isActiveInt, rc.UpdatedAt,
		rc.TenantID, rc.ID,
	)
	if err != nil {
		return err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Audit Repository Methods

func (r *Repository) CreateAuditTx(ctx context.Context, audit *FreightAudit, deductions []FreightDeduction) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	query := `INSERT INTO freight_audits (
		id, tenant_id, audit_number, trip_id, invoice_id, booking_id, rate_card_id,
		vendor_name, vehicle_number, status, billing_basis,
		billed_distance_km, billed_weight_kg, billed_rate, billed_base_freight,
		billed_toll_charges, billed_detention_charges, billed_total,
		gps_distance_km, actual_weight_kg, audited_rate, audited_base_freight,
		audited_toll_charges, audited_detention_hours, audited_detention_charges, audited_total,
		distance_variance_km, weight_variance_kg, rate_variance, toll_variance,
		overbilling_amount, variance_pct, deduction_amount, payable_amount,
		deduction_reason, debit_note_number, discrepancy_flags, dispute_reason,
		reviewed_by, reviewed_at, approved_by, approved_at, created_at, updated_at
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18,
		$19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34,
		$35, $36, $37, $38, $39, $40, $41, $42, $43, $44
	)`

	reviewedByVal := ""
	if audit.ReviewedBy != nil {
		reviewedByVal = *audit.ReviewedBy
	}
	approvedByVal := ""
	if audit.ApprovedBy != nil {
		approvedByVal = *audit.ApprovedBy
	}

	_, err = tx.ExecContext(ctx, query,
		audit.ID, audit.TenantID, audit.AuditNumber, audit.TripID, audit.InvoiceID, audit.BookingID, audit.RateCardID,
		audit.VendorName, audit.VehicleNumber, string(audit.Status), string(audit.BillingBasis),
		audit.BilledDistanceKM, audit.BilledWeightKG, audit.BilledRate, audit.BilledBaseFreight,
		audit.BilledTollCharges, audit.BilledDetentionCharges, audit.BilledTotal,
		audit.GPSDistanceKM, audit.ActualWeightKG, audit.AuditedRate, audit.AuditedBaseFreight,
		audit.AuditedTollCharges, audit.AuditedDetentionHours, audit.AuditedDetentionCharges, audit.AuditedTotal,
		audit.DistanceVarianceKM, audit.WeightVarianceKG, audit.RateVariance, audit.TollVariance,
		audit.OverbillingAmount, audit.VariancePct, audit.DeductionAmount, audit.PayableAmount,
		audit.DeductionReason, audit.DebitNoteNumber, audit.DiscrepancyFlags, audit.DisputeReason,
		reviewedByVal, audit.ReviewedAt, approvedByVal, audit.ApprovedAt, audit.CreatedAt, audit.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "duplicate") {
			return ErrAuditNumberExists
		}
		return err
	}

	deductQuery := `INSERT INTO freight_audit_deductions (
		id, tenant_id, audit_id, category, description, claimed_value, verified_value, unit_rate, deduction_amount, created_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	for _, d := range deductions {
		_, err := tx.ExecContext(ctx, deductQuery,
			d.ID, d.TenantID, d.AuditID, string(d.Category), d.Description,
			d.ClaimedValue, d.VerifiedValue, d.UnitRate, d.DeductionAmount, d.CreatedAt,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *Repository) GetAuditByID(ctx context.Context, tenantID, id string) (*FreightAudit, error) {
	query := `SELECT 
		id, tenant_id, audit_number, trip_id, invoice_id, booking_id, rate_card_id,
		vendor_name, vehicle_number, status, billing_basis,
		billed_distance_km, billed_weight_kg, billed_rate, billed_base_freight,
		billed_toll_charges, billed_detention_charges, billed_total,
		gps_distance_km, actual_weight_kg, audited_rate, audited_base_freight,
		audited_toll_charges, audited_detention_hours, audited_detention_charges, audited_total,
		distance_variance_km, weight_variance_kg, rate_variance, toll_variance,
		overbilling_amount, variance_pct, deduction_amount, payable_amount,
		deduction_reason, debit_note_number, discrepancy_flags, dispute_reason,
		reviewed_by, reviewed_at, approved_by, approved_at, created_at, updated_at
	FROM freight_audits
	WHERE tenant_id = $1 AND id = $2`

	var a FreightAudit
	var tripID, invoiceID, bookingID, rateCardID sql.NullString
	var reviewedBy, approvedBy sql.NullString
	var reviewedAt, approvedAt sql.NullTime

	err := r.db.QueryRowContext(ctx, query, tenantID, id).Scan(
		&a.ID, &a.TenantID, &a.AuditNumber, &tripID, &invoiceID, &bookingID, &rateCardID,
		&a.VendorName, &a.VehicleNumber, &a.Status, &a.BillingBasis,
		&a.BilledDistanceKM, &a.BilledWeightKG, &a.BilledRate, &a.BilledBaseFreight,
		&a.BilledTollCharges, &a.BilledDetentionCharges, &a.BilledTotal,
		&a.GPSDistanceKM, &a.ActualWeightKG, &a.AuditedRate, &a.AuditedBaseFreight,
		&a.AuditedTollCharges, &a.AuditedDetentionHours, &a.AuditedDetentionCharges, &a.AuditedTotal,
		&a.DistanceVarianceKM, &a.WeightVarianceKG, &a.RateVariance, &a.TollVariance,
		&a.OverbillingAmount, &a.VariancePct, &a.DeductionAmount, &a.PayableAmount,
		&a.DeductionReason, &a.DebitNoteNumber, &a.DiscrepancyFlags, &a.DisputeReason,
		&reviewedBy, &reviewedAt, &approvedBy, &approvedAt, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		if errorsIs(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if tripID.Valid {
		a.TripID = &tripID.String
	}
	if invoiceID.Valid {
		a.InvoiceID = &invoiceID.String
	}
	if bookingID.Valid {
		a.BookingID = &bookingID.String
	}
	if rateCardID.Valid {
		a.RateCardID = &rateCardID.String
	}
	if reviewedBy.Valid {
		a.ReviewedBy = &reviewedBy.String
	}
	if reviewedAt.Valid {
		a.ReviewedAt = &reviewedAt.Time
	}
	if approvedBy.Valid {
		a.ApprovedBy = &approvedBy.String
	}
	if approvedAt.Valid {
		a.ApprovedAt = &approvedAt.Time
	}

	// Fetch deduction line items
	deductQuery := `SELECT id, tenant_id, audit_id, category, description, claimed_value, verified_value, unit_rate, deduction_amount, created_at
		FROM freight_audit_deductions
		WHERE tenant_id = $1 AND audit_id = $2
		ORDER BY created_at ASC`

	rows, err := r.db.QueryContext(ctx, deductQuery, tenantID, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var d FreightDeduction
		if err := rows.Scan(
			&d.ID, &d.TenantID, &d.AuditID, &d.Category, &d.Description,
			&d.ClaimedValue, &d.VerifiedValue, &d.UnitRate, &d.DeductionAmount, &d.CreatedAt,
		); err != nil {
			return nil, err
		}
		a.Deductions = append(a.Deductions, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &a, nil
}

func (r *Repository) ListAudits(ctx context.Context, tenantID string, filter AuditFilter) ([]FreightAudit, int, error) {
	conditions := []string{"tenant_id = $1"}
	args := []interface{}{tenantID}
	argIdx := 2

	if filter.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, filter.Status)
		argIdx++
	}
	if filter.TripID != "" {
		conditions = append(conditions, fmt.Sprintf("trip_id = $%d", argIdx))
		args = append(args, filter.TripID)
		argIdx++
	}
	if filter.InvoiceID != "" {
		conditions = append(conditions, fmt.Sprintf("invoice_id = $%d", argIdx))
		args = append(args, filter.InvoiceID)
		argIdx++
	}
	if filter.DiscrepancyOnly {
		conditions = append(conditions, "overbilling_amount > 0.0")
	}
	if filter.Search != "" {
		searchTerm := "%" + filter.Search + "%"
		conditions = append(conditions, fmt.Sprintf("(audit_number LIKE $%d OR vendor_name LIKE $%d OR vehicle_number LIKE $%d)", argIdx, argIdx, argIdx))
		args = append(args, searchTerm)
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM freight_audits %s", whereClause)
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	query := fmt.Sprintf(`SELECT 
		id, tenant_id, audit_number, trip_id, invoice_id, booking_id, rate_card_id,
		vendor_name, vehicle_number, status, billing_basis,
		billed_distance_km, billed_weight_kg, billed_rate, billed_base_freight,
		billed_toll_charges, billed_detention_charges, billed_total,
		gps_distance_km, actual_weight_kg, audited_rate, audited_base_freight,
		audited_toll_charges, audited_detention_hours, audited_detention_charges, audited_total,
		distance_variance_km, weight_variance_kg, rate_variance, toll_variance,
		overbilling_amount, variance_pct, deduction_amount, payable_amount,
		deduction_reason, debit_note_number, discrepancy_flags, dispute_reason,
		reviewed_by, reviewed_at, approved_by, approved_at, created_at, updated_at
	FROM freight_audits
	%s
	ORDER BY created_at DESC
	LIMIT %d OFFSET %d`, whereClause, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var audits []FreightAudit
	for rows.Next() {
		var a FreightAudit
		var tripID, invoiceID, bookingID, rateCardID sql.NullString
		var reviewedBy, approvedBy sql.NullString
		var reviewedAt, approvedAt sql.NullTime

		if err := rows.Scan(
			&a.ID, &a.TenantID, &a.AuditNumber, &tripID, &invoiceID, &bookingID, &rateCardID,
			&a.VendorName, &a.VehicleNumber, &a.Status, &a.BillingBasis,
			&a.BilledDistanceKM, &a.BilledWeightKG, &a.BilledRate, &a.BilledBaseFreight,
			&a.BilledTollCharges, &a.BilledDetentionCharges, &a.BilledTotal,
			&a.GPSDistanceKM, &a.ActualWeightKG, &a.AuditedRate, &a.AuditedBaseFreight,
			&a.AuditedTollCharges, &a.AuditedDetentionHours, &a.AuditedDetentionCharges, &a.AuditedTotal,
			&a.DistanceVarianceKM, &a.WeightVarianceKG, &a.RateVariance, &a.TollVariance,
			&a.OverbillingAmount, &a.VariancePct, &a.DeductionAmount, &a.PayableAmount,
			&a.DeductionReason, &a.DebitNoteNumber, &a.DiscrepancyFlags, &a.DisputeReason,
			&reviewedBy, &reviewedAt, &approvedBy, &approvedAt, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}

		if tripID.Valid {
			a.TripID = &tripID.String
		}
		if invoiceID.Valid {
			a.InvoiceID = &invoiceID.String
		}
		if bookingID.Valid {
			a.BookingID = &bookingID.String
		}
		if rateCardID.Valid {
			a.RateCardID = &rateCardID.String
		}
		if reviewedBy.Valid {
			a.ReviewedBy = &reviewedBy.String
		}
		if reviewedAt.Valid {
			a.ReviewedAt = &reviewedAt.Time
		}
		if approvedBy.Valid {
			a.ApprovedBy = &approvedBy.String
		}
		if approvedAt.Valid {
			a.ApprovedAt = &approvedAt.Time
		}
		audits = append(audits, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return audits, total, nil
}

func (r *Repository) UpdateAudit(ctx context.Context, a *FreightAudit) error {
	query := `UPDATE freight_audits SET
		status = $1, deduction_amount = $2, payable_amount = $3,
		deduction_reason = $4, debit_note_number = $5, dispute_reason = $6,
		reviewed_by = $7, reviewed_at = $8, approved_by = $9, approved_at = $10,
		updated_at = $11
	WHERE tenant_id = $12 AND id = $13`

	reviewedByVal := ""
	if a.ReviewedBy != nil {
		reviewedByVal = *a.ReviewedBy
	}
	approvedByVal := ""
	if a.ApprovedBy != nil {
		approvedByVal = *a.ApprovedBy
	}

	res, err := r.db.ExecContext(ctx, query,
		string(a.Status), a.DeductionAmount, a.PayableAmount,
		a.DeductionReason, a.DebitNoteNumber, a.DisputeReason,
		reviewedByVal, a.ReviewedAt, approvedByVal, a.ApprovedAt,
		a.UpdatedAt, a.TenantID, a.ID,
	)
	if err != nil {
		return err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) GetSummary(ctx context.Context, tenantID string) (*AuditSummary, error) {
	query := `SELECT 
		COUNT(*) as total_audits,
		COALESCE(SUM(billed_total), 0.0) as total_billed,
		COALESCE(SUM(audited_total), 0.0) as total_audited,
		COALESCE(SUM(overbilling_amount), 0.0) as total_overbilling,
		COALESCE(SUM(deduction_amount), 0.0) as total_deductions,
		COALESCE(SUM(CASE WHEN overbilling_amount > 0 THEN 1 ELSE 0 END), 0) as discrepancies,
		COALESCE(SUM(CASE WHEN status IN ('SETTLED', 'DEDUCTION_APPROVED') THEN 1 ELSE 0 END), 0) as settled
	FROM freight_audits
	WHERE tenant_id = $1`

	var s AuditSummary
	err := r.db.QueryRowContext(ctx, query, tenantID).Scan(
		&s.TotalAudits,
		&s.TotalBilledAmount,
		&s.TotalAuditedAmount,
		&s.TotalOverbillingAmount,
		&s.TotalDeductionsAmount,
		&s.DiscrepancyCount,
		&s.SettledCount,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func errorsIs(err, target error) bool {
	return err == target
}
