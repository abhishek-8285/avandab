package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
)

type Repository interface {
	CreatePart(ctx context.Context, p *Part) error
	GetPartByID(ctx context.Context, tenantID, id string) (*Part, error)
	GetPartByNumber(ctx context.Context, tenantID, partNumber string) (*Part, error)
	UpdatePart(ctx context.Context, p *Part) error
	ListParts(ctx context.Context, filter PartFilter) ([]Part, int, error)

	GetPartStock(ctx context.Context, tenantID, partID, facilityID string) (*PartStock, error)
	ListStocks(ctx context.Context, filter StockFilter) ([]PartStock, int, error)

	CreatePurchaseBill(ctx context.Context, b *PartPurchaseBill, items []PartPurchaseItem) error
	GetPurchaseBillByID(ctx context.Context, tenantID, id string) (*PartPurchaseBill, error)
	ListPurchaseBills(ctx context.Context, filter PurchaseBillFilter) ([]PartPurchaseBill, int, error)

	ReceivePurchaseBillTx(ctx context.Context, tenantID, billID, receivedBy string, acts []*PartTransaction) error
	IssuePartTx(ctx context.Context, tenantID string, act *PartTransaction) error
	ReturnPartTx(ctx context.Context, tenantID string, act *PartTransaction) error
	AdjustStockTx(ctx context.Context, tenantID string, act *PartTransaction, newQty float64) error
	TransferStockTx(ctx context.Context, tenantID string, outAct, inAct *PartTransaction) error

	ListTransactions(ctx context.Context, filter TransactionFilter) ([]PartTransaction, int, error)
	GetSummary(ctx context.Context, tenantID string) (*InventorySummary, error)
}

type SQLRepository struct {
	db *sql.DB
}

func NewSQLRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func (r *SQLRepository) CreatePart(ctx context.Context, p *Part) error {
	query := `INSERT INTO parts (
		id, tenant_id, part_number, name, category, description, uom,
		min_stock_level, reorder_quantity, cost_price, selling_price,
		oem_brand, compatible_models, is_active, created_at, updated_at
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7,
		$8, $9, $10, $11,
		$12, $13, $14, $15, $16
	)`

	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}

	isActive := 1
	if !p.IsActive {
		isActive = 0
	}

	_, err := r.db.ExecContext(ctx, query,
		p.ID, p.TenantID, p.PartNumber, p.Name, string(p.Category), p.Description, string(p.UOM),
		p.MinStockLevel, p.ReorderQuantity, p.CostPrice, p.SellingPrice,
		p.OEMBrand, p.CompatibleModels, isActive, p.CreatedAt, p.UpdatedAt,
	)
	return err
}

func (r *SQLRepository) GetPartByID(ctx context.Context, tenantID, id string) (*Part, error) {
	query := `SELECT
		p.id, p.tenant_id, p.part_number, p.name, p.category, p.description, p.uom,
		p.min_stock_level, p.reorder_quantity, p.cost_price, p.selling_price,
		p.oem_brand, p.compatible_models, p.is_active, p.created_at, p.updated_at,
		COALESCE((SELECT SUM(quantity_on_hand) FROM part_stocks WHERE tenant_id = p.tenant_id AND part_id = p.id), 0.0) as total_stock
	FROM parts p
	WHERE p.tenant_id = $1 AND p.id = $2`

	row := r.db.QueryRowContext(ctx, query, tenantID, id)
	return r.scanPart(row)
}

func (r *SQLRepository) GetPartByNumber(ctx context.Context, tenantID, partNumber string) (*Part, error) {
	query := `SELECT
		p.id, p.tenant_id, p.part_number, p.name, p.category, p.description, p.uom,
		p.min_stock_level, p.reorder_quantity, p.cost_price, p.selling_price,
		p.oem_brand, p.compatible_models, p.is_active, p.created_at, p.updated_at,
		COALESCE((SELECT SUM(quantity_on_hand) FROM part_stocks WHERE tenant_id = p.tenant_id AND part_id = p.id), 0.0) as total_stock
	FROM parts p
	WHERE p.tenant_id = $1 AND p.part_number = $2`

	row := r.db.QueryRowContext(ctx, query, tenantID, partNumber)
	return r.scanPart(row)
}

func (r *SQLRepository) UpdatePart(ctx context.Context, p *Part) error {
	query := `UPDATE parts SET
		name = $1, category = $2, description = $3, uom = $4,
		min_stock_level = $5, reorder_quantity = $6, cost_price = $7, selling_price = $8,
		oem_brand = $9, compatible_models = $10, is_active = $11, updated_at = $12
	WHERE tenant_id = $13 AND id = $14`

	p.UpdatedAt = time.Now().UTC()
	isActive := 1
	if !p.IsActive {
		isActive = 0
	}

	res, err := r.db.ExecContext(ctx, query,
		p.Name, string(p.Category), p.Description, string(p.UOM),
		p.MinStockLevel, p.ReorderQuantity, p.CostPrice, p.SellingPrice,
		p.OEMBrand, p.CompatibleModels, isActive, p.UpdatedAt,
		p.TenantID, p.ID,
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

func (r *SQLRepository) ListParts(ctx context.Context, filter PartFilter) ([]Part, int, error) {
	var conditions []string
	var args []interface{}
	argIdx := 1

	conditions = append(conditions, fmt.Sprintf("p.tenant_id = $%d", argIdx))
	args = append(args, filter.TenantID)
	argIdx++

	if filter.Category != "" {
		conditions = append(conditions, fmt.Sprintf("p.category = $%d", argIdx))
		args = append(args, filter.Category)
		argIdx++
	}

	if filter.ActiveOnly {
		conditions = append(conditions, fmt.Sprintf("p.is_active = $%d", argIdx))
		args = append(args, 1)
		argIdx++
	}

	if filter.Search != "" {
		searchTerm := "%" + filter.Search + "%"
		conditions = append(conditions, fmt.Sprintf("(p.part_number LIKE $%d OR p.name LIKE $%d OR p.oem_brand LIKE $%d)", argIdx, argIdx, argIdx))
		args = append(args, searchTerm)
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	if filter.LowStockOnly {
		whereClause += " AND COALESCE((SELECT SUM(quantity_on_hand) FROM part_stocks WHERE tenant_id = p.tenant_id AND part_id = p.id), 0.0) <= p.min_stock_level"
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM parts p %s", whereClause)
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
		p.id, p.tenant_id, p.part_number, p.name, p.category, p.description, p.uom,
		p.min_stock_level, p.reorder_quantity, p.cost_price, p.selling_price,
		p.oem_brand, p.compatible_models, p.is_active, p.created_at, p.updated_at,
		COALESCE((SELECT SUM(quantity_on_hand) FROM part_stocks WHERE tenant_id = p.tenant_id AND part_id = p.id), 0.0) as total_stock
	FROM parts p %s
	ORDER BY p.part_number ASC
	LIMIT %d OFFSET %d`, whereClause, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var result []Part
	for rows.Next() {
		p, err := r.scanPart(rows)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, *p)
	}
	return result, total, rows.Err()
}

func (r *SQLRepository) GetPartStock(ctx context.Context, tenantID, partID, facilityID string) (*PartStock, error) {
	query := `SELECT
		ps.id, ps.tenant_id, ps.part_id, ps.facility_id, ps.quantity_on_hand, ps.quantity_reserved, ps.bin_location, ps.updated_at,
		p.part_number, p.name, p.uom, p.min_stock_level,
		f.name, f.facility_code
	FROM part_stocks ps
	JOIN parts p ON p.id = ps.part_id AND p.tenant_id = ps.tenant_id
	JOIN facilities f ON f.id = ps.facility_id AND f.tenant_id = ps.tenant_id
	WHERE ps.tenant_id = $1 AND ps.part_id = $2 AND ps.facility_id = $3`

	var s PartStock
	var uomStr string
	err := r.db.QueryRowContext(ctx, query, tenantID, partID, facilityID).Scan(
		&s.ID, &s.TenantID, &s.PartID, &s.FacilityID, &s.QuantityOnHand, &s.QuantityReserved, &s.BinLocation, &s.UpdatedAt,
		&s.PartNumber, &s.PartName, &uomStr, &s.MinStock,
		&s.FacilityName, &s.FacilityCode,
	)
	if err != nil {
		return nil, err
	}
	s.UOM = PartUOM(uomStr)
	s.IsLowStock = s.QuantityOnHand <= s.MinStock
	return &s, nil
}

func (r *SQLRepository) ListStocks(ctx context.Context, filter StockFilter) ([]PartStock, int, error) {
	var conditions []string
	var args []interface{}
	argIdx := 1

	conditions = append(conditions, fmt.Sprintf("ps.tenant_id = $%d", argIdx))
	args = append(args, filter.TenantID)
	argIdx++

	if filter.FacilityID != "" {
		conditions = append(conditions, fmt.Sprintf("ps.facility_id = $%d", argIdx))
		args = append(args, filter.FacilityID)
		argIdx++
	}

	if filter.PartID != "" {
		conditions = append(conditions, fmt.Sprintf("ps.part_id = $%d", argIdx))
		args = append(args, filter.PartID)
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")
	if filter.LowStockOnly {
		whereClause += " AND ps.quantity_on_hand <= p.min_stock_level"
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM part_stocks ps
		JOIN parts p ON p.id = ps.part_id AND p.tenant_id = ps.tenant_id
		JOIN facilities f ON f.id = ps.facility_id AND f.tenant_id = ps.tenant_id
		%s`, whereClause)

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
		ps.id, ps.tenant_id, ps.part_id, ps.facility_id, ps.quantity_on_hand, ps.quantity_reserved, ps.bin_location, ps.updated_at,
		p.part_number, p.name, p.uom, p.min_stock_level,
		f.name, f.facility_code
	FROM part_stocks ps
	JOIN parts p ON p.id = ps.part_id AND p.tenant_id = ps.tenant_id
	JOIN facilities f ON f.id = ps.facility_id AND f.tenant_id = ps.tenant_id
	%s
	ORDER BY f.name ASC, p.part_number ASC
	LIMIT %d OFFSET %d`, whereClause, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var result []PartStock
	for rows.Next() {
		var s PartStock
		var uomStr string
		if err := rows.Scan(
			&s.ID, &s.TenantID, &s.PartID, &s.FacilityID, &s.QuantityOnHand, &s.QuantityReserved, &s.BinLocation, &s.UpdatedAt,
			&s.PartNumber, &s.PartName, &uomStr, &s.MinStock,
			&s.FacilityName, &s.FacilityCode,
		); err != nil {
			return nil, 0, err
		}
		s.UOM = PartUOM(uomStr)
		s.IsLowStock = s.QuantityOnHand <= s.MinStock
		result = append(result, s)
	}
	return result, total, rows.Err()
}

func (r *SQLRepository) CreatePurchaseBill(ctx context.Context, b *PartPurchaseBill, items []PartPurchaseItem) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now
	}
	if b.UpdatedAt.IsZero() {
		b.UpdatedAt = now
	}

	billQuery := `INSERT INTO part_purchase_bills (
		id, tenant_id, bill_number, vendor_name, vendor_gstin, facility_id,
		bill_date, total_amount, tax_amount, status, notes, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`

	_, err = tx.ExecContext(ctx, billQuery,
		b.ID, b.TenantID, b.BillNumber, b.VendorName, b.VendorGSTIN, b.FacilityID,
		b.BillDate, b.TotalAmount, b.TaxAmount, string(b.Status), b.Notes, b.CreatedAt, b.UpdatedAt,
	)
	if err != nil {
		return err
	}

	itemQuery := `INSERT INTO part_purchase_items (
		id, tenant_id, bill_id, part_id, quantity, unit_cost, tax_rate, total_cost, created_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	for _, item := range items {
		itemCreatedAt := item.CreatedAt
		if itemCreatedAt.IsZero() {
			itemCreatedAt = now
		}
		_, err := tx.ExecContext(ctx, itemQuery,
			item.ID, item.TenantID, b.ID, item.PartID, item.Quantity, item.UnitCost, item.TaxRate, item.TotalCost, itemCreatedAt,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *SQLRepository) GetPurchaseBillByID(ctx context.Context, tenantID, id string) (*PartPurchaseBill, error) {
	billQuery := `SELECT
		b.id, b.tenant_id, b.bill_number, b.vendor_name, b.vendor_gstin, b.facility_id,
		b.bill_date, b.total_amount, b.tax_amount, b.status, b.received_date, b.received_by,
		b.notes, b.created_at, b.updated_at,
		f.name as facility_name
	FROM part_purchase_bills b
	JOIN facilities f ON f.id = b.facility_id AND f.tenant_id = b.tenant_id
	WHERE b.tenant_id = $1 AND b.id = $2`

	var b PartPurchaseBill
	var statusStr string
	var recDate sql.NullTime
	var recBy sql.NullString

	err := r.db.QueryRowContext(ctx, billQuery, tenantID, id).Scan(
		&b.ID, &b.TenantID, &b.BillNumber, &b.VendorName, &b.VendorGSTIN, &b.FacilityID,
		&b.BillDate, &b.TotalAmount, &b.TaxAmount, &statusStr, &recDate, &recBy,
		&b.Notes, &b.CreatedAt, &b.UpdatedAt,
		&b.FacilityName,
	)
	if err != nil {
		return nil, err
	}
	b.Status = BillStatus(statusStr)
	if recDate.Valid {
		b.ReceivedDate = &recDate.Time
	}
	if recBy.Valid {
		b.ReceivedBy = recBy.String
	}

	// Fetch items
	itemQuery := `SELECT
		pi.id, pi.tenant_id, pi.bill_id, pi.part_id, pi.quantity, pi.unit_cost, pi.tax_rate, pi.total_cost, pi.created_at,
		p.part_number, p.name
	FROM part_purchase_items pi
	JOIN parts p ON p.id = pi.part_id AND p.tenant_id = pi.tenant_id
	WHERE pi.tenant_id = $1 AND pi.bill_id = $2
	ORDER BY pi.created_at ASC`

	rows, err := r.db.QueryContext(ctx, itemQuery, tenantID, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var item PartPurchaseItem
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.BillID, &item.PartID, &item.Quantity, &item.UnitCost, &item.TaxRate, &item.TotalCost, &item.CreatedAt,
			&item.PartNumber, &item.PartName,
		); err != nil {
			return nil, err
		}
		b.Items = append(b.Items, item)
	}

	return &b, rows.Err()
}

func (r *SQLRepository) ListPurchaseBills(ctx context.Context, filter PurchaseBillFilter) ([]PartPurchaseBill, int, error) {
	var conditions []string
	var args []interface{}
	argIdx := 1

	conditions = append(conditions, fmt.Sprintf("b.tenant_id = $%d", argIdx))
	args = append(args, filter.TenantID)
	argIdx++

	if filter.FacilityID != "" {
		conditions = append(conditions, fmt.Sprintf("b.facility_id = $%d", argIdx))
		args = append(args, filter.FacilityID)
		argIdx++
	}

	if filter.Status != "" {
		conditions = append(conditions, fmt.Sprintf("b.status = $%d", argIdx))
		args = append(args, filter.Status)
		argIdx++
	}

	if filter.Search != "" {
		searchTerm := "%" + filter.Search + "%"
		conditions = append(conditions, fmt.Sprintf("(b.bill_number LIKE $%d OR b.vendor_name LIKE $%d)", argIdx, argIdx))
		args = append(args, searchTerm)
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM part_purchase_bills b %s", whereClause)
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
		b.id, b.tenant_id, b.bill_number, b.vendor_name, b.vendor_gstin, b.facility_id,
		b.bill_date, b.total_amount, b.tax_amount, b.status, b.received_date, b.received_by,
		b.notes, b.created_at, b.updated_at,
		f.name as facility_name
	FROM part_purchase_bills b
	JOIN facilities f ON f.id = b.facility_id AND f.tenant_id = b.tenant_id
	%s
	ORDER BY b.bill_date DESC, b.created_at DESC
	LIMIT %d OFFSET %d`, whereClause, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var result []PartPurchaseBill
	for rows.Next() {
		var b PartPurchaseBill
		var statusStr string
		var recDate sql.NullTime
		var recBy sql.NullString

		if err := rows.Scan(
			&b.ID, &b.TenantID, &b.BillNumber, &b.VendorName, &b.VendorGSTIN, &b.FacilityID,
			&b.BillDate, &b.TotalAmount, &b.TaxAmount, &statusStr, &recDate, &recBy,
			&b.Notes, &b.CreatedAt, &b.UpdatedAt,
			&b.FacilityName,
		); err != nil {
			return nil, 0, err
		}
		b.Status = BillStatus(statusStr)
		if recDate.Valid {
			b.ReceivedDate = &recDate.Time
		}
		if recBy.Valid {
			b.ReceivedBy = recBy.String
		}
		result = append(result, b)
	}
	return result, total, rows.Err()
}

func (r *SQLRepository) ReceivePurchaseBillTx(ctx context.Context, tenantID, billID, receivedBy string, acts []*PartTransaction) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()

	// Update bill status to RECEIVED
	billUpdateQuery := `UPDATE part_purchase_bills SET
		status = 'RECEIVED', received_date = $1, received_by = $2, updated_at = $3
	WHERE tenant_id = $4 AND id = $5 AND status = 'DRAFT'`

	res, err := tx.ExecContext(ctx, billUpdateQuery, now, receivedBy, now, tenantID, billID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrBillAlreadyProcessed
	}

	// Process each item: update stock and insert transaction
	for _, act := range acts {
		// Upsert part_stocks
		stockUpsertQuery := `INSERT INTO part_stocks (
			id, tenant_id, part_id, facility_id, quantity_on_hand, quantity_reserved, bin_location, updated_at
		) VALUES ($1, $2, $3, $4, $5, 0.0, '', $6)
		ON CONFLICT(tenant_id, part_id, facility_id) DO UPDATE SET
			quantity_on_hand = part_stocks.quantity_on_hand + $5,
			updated_at = $6`

		stockID := fmt.Sprintf("stk-%s-%s", act.PartID, act.FacilityID)
		_, err := tx.ExecContext(ctx, stockUpsertQuery,
			stockID, tenantID, act.PartID, act.FacilityID, act.Quantity, now,
		)
		if err != nil {
			return err
		}

		// Read back balance after
		var currentStock float64
		err = tx.QueryRowContext(ctx,
			`SELECT quantity_on_hand FROM part_stocks WHERE tenant_id = $1 AND part_id = $2 AND facility_id = $3`,
			tenantID, act.PartID, act.FacilityID,
		).Scan(&currentStock)
		if err != nil {
			return err
		}
		act.BalanceAfter = currentStock

		// Insert transaction ledger
		if err := r.insertTxRecord(ctx, tx, act); err != nil {
			return err
		}

		// Update part cost_price (weighted average cost or latest cost)
		_, _ = tx.ExecContext(ctx,
			`UPDATE parts SET cost_price = $1, updated_at = $2 WHERE tenant_id = $3 AND id = $4`,
			act.UnitCost, now, tenantID, act.PartID,
		)
	}

	return tx.Commit()
}

func (r *SQLRepository) IssuePartTx(ctx context.Context, tenantID string, act *PartTransaction) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()

	// 1. Decrement part_stocks checking that quantity_on_hand >= quantity
	query := `UPDATE part_stocks SET
		quantity_on_hand = quantity_on_hand - $1,
		updated_at = $2
	WHERE tenant_id = $3 AND part_id = $4 AND facility_id = $5 AND quantity_on_hand >= $1`

	res, err := tx.ExecContext(ctx, query, act.Quantity, now, tenantID, act.PartID, act.FacilityID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInsufficientStock
	}

	// 2. Read balance after
	var currentStock float64
	err = tx.QueryRowContext(ctx,
		`SELECT quantity_on_hand FROM part_stocks WHERE tenant_id = $1 AND part_id = $2 AND facility_id = $3`,
		tenantID, act.PartID, act.FacilityID,
	).Scan(&currentStock)
	if err != nil {
		return err
	}
	act.BalanceAfter = currentStock

	// 3. Record transaction
	if err := r.insertTxRecord(ctx, tx, act); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *SQLRepository) ReturnPartTx(ctx context.Context, tenantID string, act *PartTransaction) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()

	// Increment part_stocks
	stockUpsertQuery := `INSERT INTO part_stocks (
		id, tenant_id, part_id, facility_id, quantity_on_hand, quantity_reserved, bin_location, updated_at
	) VALUES ($1, $2, $3, $4, $5, 0.0, '', $6)
	ON CONFLICT(tenant_id, part_id, facility_id) DO UPDATE SET
		quantity_on_hand = part_stocks.quantity_on_hand + $5,
		updated_at = $6`

	stockID := fmt.Sprintf("stk-%s-%s", act.PartID, act.FacilityID)
	_, err = tx.ExecContext(ctx, stockUpsertQuery, stockID, tenantID, act.PartID, act.FacilityID, act.Quantity, now)
	if err != nil {
		return err
	}

	var currentStock float64
	err = tx.QueryRowContext(ctx,
		`SELECT quantity_on_hand FROM part_stocks WHERE tenant_id = $1 AND part_id = $2 AND facility_id = $3`,
		tenantID, act.PartID, act.FacilityID,
	).Scan(&currentStock)
	if err != nil {
		return err
	}
	act.BalanceAfter = currentStock

	if err := r.insertTxRecord(ctx, tx, act); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *SQLRepository) AdjustStockTx(ctx context.Context, tenantID string, act *PartTransaction, newQty float64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()

	// Update part_stocks directly to newQty
	stockUpsertQuery := `INSERT INTO part_stocks (
		id, tenant_id, part_id, facility_id, quantity_on_hand, quantity_reserved, bin_location, updated_at
	) VALUES ($1, $2, $3, $4, $5, 0.0, '', $6)
	ON CONFLICT(tenant_id, part_id, facility_id) DO UPDATE SET
		quantity_on_hand = $5,
		updated_at = $6`

	stockID := fmt.Sprintf("stk-%s-%s", act.PartID, act.FacilityID)
	_, err = tx.ExecContext(ctx, stockUpsertQuery, stockID, tenantID, act.PartID, act.FacilityID, newQty, now)
	if err != nil {
		return err
	}

	act.BalanceAfter = newQty
	if err := r.insertTxRecord(ctx, tx, act); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *SQLRepository) TransferStockTx(ctx context.Context, tenantID string, outAct, inAct *PartTransaction) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()

	// 1. Decrement from origin
	originQuery := `UPDATE part_stocks SET
		quantity_on_hand = quantity_on_hand - $1,
		updated_at = $2
	WHERE tenant_id = $3 AND part_id = $4 AND facility_id = $5 AND quantity_on_hand >= $1`

	res, err := tx.ExecContext(ctx, originQuery, outAct.Quantity, now, tenantID, outAct.PartID, outAct.FacilityID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInsufficientStock
	}

	var originBal float64
	err = tx.QueryRowContext(ctx,
		`SELECT quantity_on_hand FROM part_stocks WHERE tenant_id = $1 AND part_id = $2 AND facility_id = $3`,
		tenantID, outAct.PartID, outAct.FacilityID,
	).Scan(&originBal)
	if err != nil {
		return err
	}
	outAct.BalanceAfter = originBal

	if err := r.insertTxRecord(ctx, tx, outAct); err != nil {
		return err
	}

	// 2. Increment at destination
	destQuery := `INSERT INTO part_stocks (
		id, tenant_id, part_id, facility_id, quantity_on_hand, quantity_reserved, bin_location, updated_at
	) VALUES ($1, $2, $3, $4, $5, 0.0, '', $6)
	ON CONFLICT(tenant_id, part_id, facility_id) DO UPDATE SET
		quantity_on_hand = part_stocks.quantity_on_hand + $5,
		updated_at = $6`

	stockID := fmt.Sprintf("stk-%s-%s", inAct.PartID, inAct.FacilityID)
	_, err = tx.ExecContext(ctx, destQuery, stockID, tenantID, inAct.PartID, inAct.FacilityID, inAct.Quantity, now)
	if err != nil {
		return err
	}

	var destBal float64
	err = tx.QueryRowContext(ctx,
		`SELECT quantity_on_hand FROM part_stocks WHERE tenant_id = $1 AND part_id = $2 AND facility_id = $3`,
		tenantID, inAct.PartID, inAct.FacilityID,
	).Scan(&destBal)
	if err != nil {
		return err
	}
	inAct.BalanceAfter = destBal

	if err := r.insertTxRecord(ctx, tx, inAct); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *SQLRepository) insertTxRecord(ctx context.Context, tx *sql.Tx, act *PartTransaction) error {
	query := `INSERT INTO part_transactions (
		id, tenant_id, part_id, facility_id, transaction_type, quantity, unit_cost, total_cost,
		balance_after, reference_type, reference_id, vehicle_id, work_order_id, notes, performed_by, recorded_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`

	if act.RecordedAt.IsZero() {
		act.RecordedAt = time.Now().UTC()
	}

	_, err := tx.ExecContext(ctx, query,
		act.ID, act.TenantID, act.PartID, act.FacilityID, string(act.TransactionType),
		act.Quantity, act.UnitCost, act.TotalCost, act.BalanceAfter,
		act.ReferenceType, act.ReferenceID, act.VehicleID, act.WorkOrderID,
		act.Notes, act.PerformedBy, act.RecordedAt,
	)
	return err
}

func (r *SQLRepository) ListTransactions(ctx context.Context, filter TransactionFilter) ([]PartTransaction, int, error) {
	var conditions []string
	var args []interface{}
	argIdx := 1

	conditions = append(conditions, fmt.Sprintf("pt.tenant_id = $%d", argIdx))
	args = append(args, filter.TenantID)
	argIdx++

	if filter.PartID != "" {
		conditions = append(conditions, fmt.Sprintf("pt.part_id = $%d", argIdx))
		args = append(args, filter.PartID)
		argIdx++
	}

	if filter.FacilityID != "" {
		conditions = append(conditions, fmt.Sprintf("pt.facility_id = $%d", argIdx))
		args = append(args, filter.FacilityID)
		argIdx++
	}

	if filter.TransactionType != "" {
		conditions = append(conditions, fmt.Sprintf("pt.transaction_type = $%d", argIdx))
		args = append(args, filter.TransactionType)
		argIdx++
	}

	if filter.WorkOrderID != "" {
		conditions = append(conditions, fmt.Sprintf("pt.work_order_id = $%d", argIdx))
		args = append(args, filter.WorkOrderID)
		argIdx++
	}

	if filter.VehicleID != "" {
		conditions = append(conditions, fmt.Sprintf("pt.vehicle_id = $%d", argIdx))
		args = append(args, filter.VehicleID)
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM part_transactions pt %s", whereClause)
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
		pt.id, pt.tenant_id, pt.part_id, pt.facility_id, pt.transaction_type, pt.quantity,
		pt.unit_cost, pt.total_cost, pt.balance_after, pt.reference_type, pt.reference_id,
		pt.vehicle_id, pt.work_order_id, pt.notes, pt.performed_by, pt.recorded_at,
		p.part_number, p.name as part_name,
		f.name as facility_name
	FROM part_transactions pt
	JOIN parts p ON p.id = pt.part_id AND p.tenant_id = pt.tenant_id
	JOIN facilities f ON f.id = pt.facility_id AND f.tenant_id = pt.tenant_id
	%s
	ORDER BY pt.recorded_at DESC
	LIMIT %d OFFSET %d`, whereClause, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var result []PartTransaction
	for rows.Next() {
		var txRec PartTransaction
		var txType string
		var vehID, woID sql.NullString

		if err := rows.Scan(
			&txRec.ID, &txRec.TenantID, &txRec.PartID, &txRec.FacilityID, &txType, &txRec.Quantity,
			&txRec.UnitCost, &txRec.TotalCost, &txRec.BalanceAfter, &txRec.ReferenceType, &txRec.ReferenceID,
			&vehID, &woID, &txRec.Notes, &txRec.PerformedBy, &txRec.RecordedAt,
			&txRec.PartNumber, &txRec.PartName,
			&txRec.FacilityName,
		); err != nil {
			return nil, 0, err
		}
		txRec.TransactionType = TransactionType(txType)
		if vehID.Valid {
			txRec.VehicleID = &vehID.String
		}
		if woID.Valid {
			txRec.WorkOrderID = &woID.String
		}
		result = append(result, txRec)
	}
	return result, total, rows.Err()
}

func (r *SQLRepository) GetSummary(ctx context.Context, tenantID string) (*InventorySummary, error) {
	query := `SELECT
		COUNT(DISTINCT p.id),
		COALESCE(SUM(ps.quantity_on_hand * p.cost_price), 0.0),
		COALESCE(SUM(ps.quantity_on_hand), 0.0),
		COUNT(DISTINCT ps.facility_id)
	FROM parts p
	LEFT JOIN part_stocks ps ON ps.part_id = p.id AND ps.tenant_id = p.tenant_id
	WHERE p.tenant_id = $1 AND p.is_active = 1`

	var s InventorySummary
	err := r.db.QueryRowContext(ctx, query, tenantID).Scan(
		&s.TotalParts,
		&s.TotalValuation,
		&s.TotalStockedUnits,
		&s.ActiveFacilityCount,
	)
	if err != nil {
		return nil, err
	}
	s.TotalValuation = math.Round(s.TotalValuation*100) / 100

	// Count low stock parts
	lowStockQuery := `SELECT COUNT(*) FROM parts p
		WHERE p.tenant_id = $1 AND p.is_active = 1
		AND COALESCE((SELECT SUM(quantity_on_hand) FROM part_stocks WHERE tenant_id = p.tenant_id AND part_id = p.id), 0.0) <= p.min_stock_level`

	_ = r.db.QueryRowContext(ctx, lowStockQuery, tenantID).Scan(&s.LowStockCount)

	return &s, nil
}

type scannable interface {
	Scan(dest ...interface{}) error
}

func (r *SQLRepository) scanPart(s scannable) (*Part, error) {
	var p Part
	var catStr, uomStr string
	var isActiveInt int
	var totalStock sql.NullFloat64

	err := s.Scan(
		&p.ID, &p.TenantID, &p.PartNumber, &p.Name, &catStr, &p.Description, &uomStr,
		&p.MinStockLevel, &p.ReorderQuantity, &p.CostPrice, &p.SellingPrice,
		&p.OEMBrand, &p.CompatibleModels, &isActiveInt, &p.CreatedAt, &p.UpdatedAt,
		&totalStock,
	)
	if err != nil {
		return nil, err
	}

	p.Category = PartCategory(catStr)
	p.UOM = PartUOM(uomStr)
	p.IsActive = isActiveInt == 1
	if totalStock.Valid {
		p.TotalStockOnHand = totalStock.Float64
	}
	p.IsLowStock = p.TotalStockOnHand <= p.MinStockLevel

	return &p, nil
}
