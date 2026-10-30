package tyre

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
)

type Repository interface {
	Create(ctx context.Context, t *Tyre) error
	GetByID(ctx context.Context, tenantID, id string) (*Tyre, error)
	GetBySerialNumber(ctx context.Context, tenantID, serialNumber string) (*Tyre, error)
	List(ctx context.Context, filter TyreFilter) ([]Tyre, int, error)
	Update(ctx context.Context, t *Tyre) error
	RecordActivity(ctx context.Context, act *TyreActivity) error
	ListActivities(ctx context.Context, tenantID, tyreID string) ([]TyreActivity, error)
	RecordInspection(ctx context.Context, insp *TyreInspection) error
	ListInspections(ctx context.Context, tenantID, tyreID string) ([]TyreInspection, error)
	GetVehicleMountedTyres(ctx context.Context, tenantID, vehicleID string) ([]Tyre, error)
	GetFleetSummary(ctx context.Context, tenantID string) (*FleetTyreSummary, error)
	GetVehicleLatestOdometer(ctx context.Context, tenantID, vehicleID string) (*float64, error)
	RotateTyresTx(ctx context.Context, tenantID, vehicleID string, rotations []TyreRotationItem, activities []*TyreActivity) error
}

type SQLRepository struct {
	db *sql.DB
}

func NewSQLRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func (r *SQLRepository) Create(ctx context.Context, t *Tyre) error {
	query := `INSERT INTO tyres (
		id, tenant_id, serial_number, brand, model, size, tyre_type, status,
		vehicle_id, axle_position, mount_odometer_km, mount_date,
		initial_tread_depth_mm, current_tread_depth_mm, min_tread_depth_mm,
		purchase_date, purchase_cost, vendor_name, invoice_number,
		retread_count, max_retreads, total_retread_cost, total_km_run,
		scrap_date, scrap_reason, scrap_recovery_amount, created_at, updated_at
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8,
		$9, $10, $11, $12,
		$13, $14, $15,
		$16, $17, $18, $19,
		$20, $21, $22, $23,
		$24, $25, $26, $27, $28
	)`

	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}

	_, err := r.db.ExecContext(ctx, query,
		t.ID, t.TenantID, t.SerialNumber, t.Brand, t.Model, t.Size, string(t.TyreType), string(t.Status),
		t.VehicleID, t.AxlePosition, t.MountOdometerKM, t.MountDate,
		t.InitialTreadDepthMM, t.CurrentTreadDepthMM, t.MinTreadDepthMM,
		t.PurchaseDate, t.PurchaseCost, t.VendorName, t.InvoiceNumber,
		t.RetreadCount, t.MaxRetreads, t.TotalRetreadCost, t.TotalKMRun,
		t.ScrapDate, t.ScrapReason, t.ScrapRecoveryAmount, t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (r *SQLRepository) GetByID(ctx context.Context, tenantID, id string) (*Tyre, error) {
	query := `SELECT
		id, tenant_id, serial_number, brand, model, size, tyre_type, status,
		vehicle_id, axle_position, mount_odometer_km, mount_date,
		initial_tread_depth_mm, current_tread_depth_mm, min_tread_depth_mm,
		purchase_date, purchase_cost, vendor_name, invoice_number,
		retread_count, max_retreads, total_retread_cost, total_km_run,
		scrap_date, scrap_reason, scrap_recovery_amount, created_at, updated_at
	FROM tyres WHERE tenant_id = $1 AND id = $2`

	row := r.db.QueryRowContext(ctx, query, tenantID, id)
	return r.scanTyre(row)
}

func (r *SQLRepository) GetBySerialNumber(ctx context.Context, tenantID, serialNumber string) (*Tyre, error) {
	query := `SELECT
		id, tenant_id, serial_number, brand, model, size, tyre_type, status,
		vehicle_id, axle_position, mount_odometer_km, mount_date,
		initial_tread_depth_mm, current_tread_depth_mm, min_tread_depth_mm,
		purchase_date, purchase_cost, vendor_name, invoice_number,
		retread_count, max_retreads, total_retread_cost, total_km_run,
		scrap_date, scrap_reason, scrap_recovery_amount, created_at, updated_at
	FROM tyres WHERE tenant_id = $1 AND serial_number = $2`

	row := r.db.QueryRowContext(ctx, query, tenantID, serialNumber)
	return r.scanTyre(row)
}

func (r *SQLRepository) List(ctx context.Context, filter TyreFilter) ([]Tyre, int, error) {
	var conditions []string
	var args []interface{}
	argIdx := 1

	conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIdx))
	args = append(args, filter.TenantID)
	argIdx++

	if filter.VehicleID != "" {
		conditions = append(conditions, fmt.Sprintf("vehicle_id = $%d", argIdx))
		args = append(args, filter.VehicleID)
		argIdx++
	}
	if filter.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, filter.Status)
		argIdx++
	}
	if filter.Brand != "" {
		conditions = append(conditions, fmt.Sprintf("brand = $%d", argIdx))
		args = append(args, filter.Brand)
		argIdx++
	}
	if filter.Search != "" {
		searchTerm := "%" + filter.Search + "%"
		conditions = append(conditions, fmt.Sprintf("(serial_number LIKE $%d OR brand LIKE $%d OR model LIKE $%d OR size LIKE $%d)", argIdx, argIdx, argIdx, argIdx))
		args = append(args, searchTerm)
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	countQuery := "SELECT COUNT(*) FROM tyres " + whereClause
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
		id, tenant_id, serial_number, brand, model, size, tyre_type, status,
		vehicle_id, axle_position, mount_odometer_km, mount_date,
		initial_tread_depth_mm, current_tread_depth_mm, min_tread_depth_mm,
		purchase_date, purchase_cost, vendor_name, invoice_number,
		retread_count, max_retreads, total_retread_cost, total_km_run,
		scrap_date, scrap_reason, scrap_recovery_amount, created_at, updated_at
	FROM tyres %s ORDER BY created_at DESC LIMIT %d OFFSET %d`, whereClause, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var result []Tyre
	for rows.Next() {
		t, err := r.scanTyre(rows)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, *t)
	}
	return result, total, rows.Err()
}

func (r *SQLRepository) Update(ctx context.Context, t *Tyre) error {
	query := `UPDATE tyres SET
		serial_number = $1, brand = $2, model = $3, size = $4, tyre_type = $5, status = $6,
		vehicle_id = $7, axle_position = $8, mount_odometer_km = $9, mount_date = $10,
		initial_tread_depth_mm = $11, current_tread_depth_mm = $12, min_tread_depth_mm = $13,
		purchase_date = $14, purchase_cost = $15, vendor_name = $16, invoice_number = $17,
		retread_count = $18, max_retreads = $19, total_retread_cost = $20, total_km_run = $21,
		scrap_date = $22, scrap_reason = $23, scrap_recovery_amount = $24, updated_at = $25
	WHERE tenant_id = $26 AND id = $27`

	t.UpdatedAt = time.Now().UTC()

	res, err := r.db.ExecContext(ctx, query,
		t.SerialNumber, t.Brand, t.Model, t.Size, string(t.TyreType), string(t.Status),
		t.VehicleID, t.AxlePosition, t.MountOdometerKM, t.MountDate,
		t.InitialTreadDepthMM, t.CurrentTreadDepthMM, t.MinTreadDepthMM,
		t.PurchaseDate, t.PurchaseCost, t.VendorName, t.InvoiceNumber,
		t.RetreadCount, t.MaxRetreads, t.TotalRetreadCost, t.TotalKMRun,
		t.ScrapDate, t.ScrapReason, t.ScrapRecoveryAmount, t.UpdatedAt,
		t.TenantID, t.ID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *SQLRepository) RecordActivity(ctx context.Context, act *TyreActivity) error {
	query := `INSERT INTO tyre_activities (
		id, tenant_id, tyre_id, activity_type, vehicle_id, from_position, to_position,
		odometer_km, tread_depth_mm, pressure_psi, cost, notes, performed_by, recorded_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`

	if act.RecordedAt.IsZero() {
		act.RecordedAt = time.Now().UTC()
	}

	_, err := r.db.ExecContext(ctx, query,
		act.ID, act.TenantID, act.TyreID, string(act.ActivityType), act.VehicleID, act.FromPosition, act.ToPosition,
		act.OdometerKM, act.TreadDepthMM, act.PressurePSI, act.Cost, act.Notes, act.PerformedBy, act.RecordedAt,
	)
	return err
}

func (r *SQLRepository) ListActivities(ctx context.Context, tenantID, tyreID string) ([]TyreActivity, error) {
	query := `SELECT
		id, tenant_id, tyre_id, activity_type, vehicle_id, from_position, to_position,
		odometer_km, tread_depth_mm, pressure_psi, cost, notes, performed_by, recorded_at
	FROM tyre_activities
	WHERE tenant_id = $1 AND tyre_id = $2
	ORDER BY recorded_at DESC`

	rows, err := r.db.QueryContext(ctx, query, tenantID, tyreID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []TyreActivity
	for rows.Next() {
		var a TyreActivity
		var actType string
		var vehID, fromPos, toPos sql.NullString
		var treadDepth, pressure sql.NullFloat64

		if err := rows.Scan(
			&a.ID, &a.TenantID, &a.TyreID, &actType, &vehID, &fromPos, &toPos,
			&a.OdometerKM, &treadDepth, &pressure, &a.Cost, &a.Notes, &a.PerformedBy, &a.RecordedAt,
		); err != nil {
			return nil, err
		}

		a.ActivityType = ActivityType(actType)
		if vehID.Valid {
			a.VehicleID = &vehID.String
		}
		if fromPos.Valid {
			a.FromPosition = &fromPos.String
		}
		if toPos.Valid {
			a.ToPosition = &toPos.String
		}
		if treadDepth.Valid {
			a.TreadDepthMM = &treadDepth.Float64
		}
		if pressure.Valid {
			a.PressurePSI = &pressure.Float64
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func (r *SQLRepository) RecordInspection(ctx context.Context, insp *TyreInspection) error {
	query := `INSERT INTO tyre_inspections (
		id, tenant_id, tyre_id, vehicle_id, inspection_date, odometer_km,
		tread_depth_mm, pressure_psi, condition, action_taken, inspector_name, notes, created_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`

	now := time.Now().UTC()
	if insp.InspectionDate.IsZero() {
		insp.InspectionDate = now
	}
	if insp.CreatedAt.IsZero() {
		insp.CreatedAt = now
	}

	_, err := r.db.ExecContext(ctx, query,
		insp.ID, insp.TenantID, insp.TyreID, insp.VehicleID, insp.InspectionDate, insp.OdometerKM,
		insp.TreadDepthMM, insp.PressurePSI, string(insp.Condition), insp.ActionTaken, insp.InspectorName, insp.Notes, insp.CreatedAt,
	)
	return err
}

func (r *SQLRepository) ListInspections(ctx context.Context, tenantID, tyreID string) ([]TyreInspection, error) {
	query := `SELECT
		id, tenant_id, tyre_id, vehicle_id, inspection_date, odometer_km,
		tread_depth_mm, pressure_psi, condition, action_taken, inspector_name, notes, created_at
	FROM tyre_inspections
	WHERE tenant_id = $1 AND tyre_id = $2
	ORDER BY inspection_date DESC`

	rows, err := r.db.QueryContext(ctx, query, tenantID, tyreID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []TyreInspection
	for rows.Next() {
		var i TyreInspection
		var cond string
		var vehID sql.NullString
		var pressure sql.NullFloat64

		if err := rows.Scan(
			&i.ID, &i.TenantID, &i.TyreID, &vehID, &i.InspectionDate, &i.OdometerKM,
			&i.TreadDepthMM, &pressure, &cond, &i.ActionTaken, &i.InspectorName, &i.Notes, &i.CreatedAt,
		); err != nil {
			return nil, err
		}

		i.Condition = InspectionCondition(cond)
		if vehID.Valid {
			i.VehicleID = &vehID.String
		}
		if pressure.Valid {
			i.PressurePSI = &pressure.Float64
		}
		result = append(result, i)
	}
	return result, rows.Err()
}

func (r *SQLRepository) GetVehicleMountedTyres(ctx context.Context, tenantID, vehicleID string) ([]Tyre, error) {
	query := `SELECT
		id, tenant_id, serial_number, brand, model, size, tyre_type, status,
		vehicle_id, axle_position, mount_odometer_km, mount_date,
		initial_tread_depth_mm, current_tread_depth_mm, min_tread_depth_mm,
		purchase_date, purchase_cost, vendor_name, invoice_number,
		retread_count, max_retreads, total_retread_cost, total_km_run,
		scrap_date, scrap_reason, scrap_recovery_amount, created_at, updated_at
	FROM tyres
	WHERE tenant_id = $1 AND vehicle_id = $2 AND status = 'MOUNTED'
	ORDER BY axle_position ASC`

	rows, err := r.db.QueryContext(ctx, query, tenantID, vehicleID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []Tyre
	for rows.Next() {
		t, err := r.scanTyre(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *t)
	}
	return result, rows.Err()
}

func (r *SQLRepository) GetVehicleLatestOdometer(ctx context.Context, tenantID, vehicleID string) (*float64, error) {
	// First probe vehicle_latest_position
	var odo sql.NullFloat64
	queryVLP := `SELECT odometer FROM vehicle_latest_position WHERE vehicle_id = $1`
	err := r.db.QueryRowContext(ctx, queryVLP, vehicleID).Scan(&odo)
	if err == nil && odo.Valid {
		val := odo.Float64
		return &val, nil
	}

	// Fallback to vehicles current_mileage
	queryVeh := `SELECT current_mileage FROM vehicles WHERE tenant_id = $1 AND id = $2`
	err = r.db.QueryRowContext(ctx, queryVeh, tenantID, vehicleID).Scan(&odo)
	if err == nil && odo.Valid {
		val := odo.Float64
		return &val, nil
	}

	return nil, nil
}

func (r *SQLRepository) GetFleetSummary(ctx context.Context, tenantID string) (*FleetTyreSummary, error) {
	query := `SELECT
		COUNT(*),
		COALESCE(SUM(CASE WHEN status = 'MOUNTED' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'IN_STOCK' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'UNDER_RETREAD' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'SCRAPPED' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status != 'SCRAPPED' AND current_tread_depth_mm <= min_tread_depth_mm + 1.5 AND current_tread_depth_mm > min_tread_depth_mm THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status != 'SCRAPPED' AND current_tread_depth_mm <= min_tread_depth_mm THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(purchase_cost + total_retread_cost), 0.0),
		COALESCE(SUM(total_km_run), 0.0)
	FROM tyres WHERE tenant_id = $1`

	var s FleetTyreSummary
	var totalCost, totalKM float64
	err := r.db.QueryRowContext(ctx, query, tenantID).Scan(
		&s.TotalTyres,
		&s.MountedCount,
		&s.InStockCount,
		&s.UnderRetreadCount,
		&s.ScrappedCount,
		&s.LowTreadCount,
		&s.CriticalTreadCount,
		&totalCost,
		&totalKM,
	)
	if err != nil {
		return nil, err
	}

	s.TotalInvestmentCost = math.Round(totalCost*100) / 100
	if totalKM > 0 {
		s.FleetAverageCPKM = math.Round((totalCost/totalKM)*1000) / 1000
	}
	return &s, nil
}

func (r *SQLRepository) RotateTyresTx(ctx context.Context, tenantID, vehicleID string, rotations []TyreRotationItem, activities []*TyreActivity) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()

	// Step 1: Temporarily set axle_position = NULL for all rotating tyres to avoid unique constraint collisions
	for _, rot := range rotations {
		_, err := tx.ExecContext(ctx,
			`UPDATE tyres SET axle_position = NULL, updated_at = $1 WHERE tenant_id = $2 AND id = $3`,
			now, tenantID, rot.TyreID,
		)
		if err != nil {
			return err
		}
	}

	// Step 2: Assign final axle positions
	for _, rot := range rotations {
		toPos := strings.ToUpper(strings.TrimSpace(rot.ToPosition))
		res, err := tx.ExecContext(ctx,
			`UPDATE tyres SET axle_position = $1, updated_at = $2 WHERE tenant_id = $3 AND id = $4`,
			toPos, now, tenantID, rot.TyreID,
		)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return sql.ErrNoRows
		}
	}

	// Step 3: Insert activity log entries
	actQuery := `INSERT INTO tyre_activities (
		id, tenant_id, tyre_id, activity_type, vehicle_id, from_position, to_position,
		odometer_km, tread_depth_mm, pressure_psi, cost, notes, performed_by, recorded_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`

	for _, act := range activities {
		if act.RecordedAt.IsZero() {
			act.RecordedAt = now
		}
		_, err := tx.ExecContext(ctx, actQuery,
			act.ID, act.TenantID, act.TyreID, string(act.ActivityType), act.VehicleID, act.FromPosition, act.ToPosition,
			act.OdometerKM, act.TreadDepthMM, act.PressurePSI, act.Cost, act.Notes, act.PerformedBy, act.RecordedAt,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

type scannable interface {
	Scan(dest ...interface{}) error
}

func (r *SQLRepository) scanTyre(s scannable) (*Tyre, error) {
	var t Tyre
	var tyreType, status string
	var vehID, axlePos sql.NullString
	var mountOdo, purchaseCost, totalRetreadCost, totalKMRun, scrapRecovery sql.NullFloat64
	var mountDate, purchaseDate, scrapDate sql.NullTime

	err := s.Scan(
		&t.ID, &t.TenantID, &t.SerialNumber, &t.Brand, &t.Model, &t.Size, &tyreType, &status,
		&vehID, &axlePos, &mountOdo, &mountDate,
		&t.InitialTreadDepthMM, &t.CurrentTreadDepthMM, &t.MinTreadDepthMM,
		&purchaseDate, &purchaseCost, &t.VendorName, &t.InvoiceNumber,
		&t.RetreadCount, &t.MaxRetreads, &totalRetreadCost, &totalKMRun,
		&scrapDate, &t.ScrapReason, &scrapRecovery, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	t.TyreType = TyreType(tyreType)
	t.Status = TyreStatus(status)
	if vehID.Valid {
		t.VehicleID = &vehID.String
	}
	if axlePos.Valid {
		t.AxlePosition = &axlePos.String
	}
	if mountOdo.Valid {
		t.MountOdometerKM = &mountOdo.Float64
	}
	if mountDate.Valid {
		t.MountDate = &mountDate.Time
	}
	if purchaseDate.Valid {
		t.PurchaseDate = &purchaseDate.Time
	}
	if purchaseCost.Valid {
		t.PurchaseCost = purchaseCost.Float64
	}
	if totalRetreadCost.Valid {
		t.TotalRetreadCost = totalRetreadCost.Float64
	}
	if totalKMRun.Valid {
		t.TotalKMRun = totalKMRun.Float64
	}
	if scrapDate.Valid {
		t.ScrapDate = &scrapDate.Time
	}
	if scrapRecovery.Valid {
		t.ScrapRecoveryAmount = scrapRecovery.Float64
	}

	return &t, nil
}
