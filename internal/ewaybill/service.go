package ewaybill

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/events"
	intEWB "transport-app/internal/integration/ewaybill"
)

var (
	ErrGoodsValueTooLow       = errors.New("goods value is 50,000 or below; e-way bill not required")
	ErrNotActive              = errors.New("e-way bill is not active")
	ErrExtensionLimitExceeded = errors.New("extension limit exceeded (max 1 extension allowed)")
	ErrNoGeofenceEvidence     = errors.New("extension denied: lacking proximity/geofence evidence within destination threshold")
	ErrEWBNotFound            = errors.New("e-way bill not found")
)

// EWayBillRecord represents an eway_bills row.
type EWayBillRecord struct {
	ID               string     `json:"id"`
	TripID           string     `json:"trip_id"`
	EwbNumber        string     `json:"ewb_number"`
	IRN              *string    `json:"irn,omitempty"`
	Status           string     `json:"status"`
	GenerationDate   time.Time  `json:"generation_date"`
	ValidUntil       time.Time  `json:"valid_until"`
	ValidUpto        string     `json:"valid_upto"`
	VehicleNumber    *string    `json:"vehicle_number,omitempty"`
	TransporterID    *string    `json:"transporter_id,omitempty"`
	QRCode           string     `json:"qr_code,omitempty"`
	FromPlace        string     `json:"from_place,omitempty"`
	FromStateCode    string     `json:"from_state_code,omitempty"`
	ToPlace          string     `json:"to_place,omitempty"`
	ToStateCode      string     `json:"to_state_code,omitempty"`
	GoodsValue       float64    `json:"goods_value"`
	Distance         int        `json:"distance"`
	DocType          string     `json:"doc_type,omitempty"`
	DocNo            string     `json:"doc_no,omitempty"`
	DocDate          string     `json:"doc_date,omitempty"`
	TransporterDocNo string     `json:"transporter_doc_no,omitempty"`
	ExtensionCount   int        `json:"extension_count"`
	CancelReason     string     `json:"cancel_reason,omitempty"`
	CancelledAt      *time.Time `json:"cancelled_at,omitempty"`
	GenMode          string     `json:"gen_mode"`
	CreatedAt        time.Time  `json:"created_at"`
}

// GeneratePartARequest carries inputs to create a Part-A EWB.
type GeneratePartARequest struct {
	TripID           string  `json:"trip_id"`
	DocType          string  `json:"doc_type"`
	DocNo            string  `json:"doc_no"`
	DocDate          string  `json:"doc_date"`
	FromGSTIN        string  `json:"from_gstin"`
	ToGSTIN          string  `json:"to_gstin"`
	FromPlace        string  `json:"from_place"`
	FromStateCode    string  `json:"from_state_code"`
	ToPlace          string  `json:"to_place"`
	ToStateCode      string  `json:"to_state_code"`
	TransporterID    string  `json:"transporter_id"`
	GoodsValue       float64 `json:"goods_value"`
	Distance         int     `json:"distance"`
	TransporterDocNo string  `json:"transporter_doc_no"`
	GenMode          string  `json:"gen_mode"` // MANUAL or AUTO
	Force            bool    `json:"force"`
}

// AttachPartBRequest carries inputs to attach vehicle to an EWB.
type AttachPartBRequest struct {
	EwbNumber     string `json:"ewb_number"`
	VehicleNumber string `json:"vehicle_number"`
	TransporterID string `json:"transporter_id"`
	FromPlace     string `json:"from_place"`
	FromStateCode string `json:"from_state_code"`
	Reason        string `json:"reason"`
}

// ExtendRequest carries inputs to extend EWB validity.
type ExtendRequest struct {
	EwbNumber         string `json:"ewb_number"`
	FromPlace         string `json:"from_place"`
	FromStateCode     string `json:"from_state_code"`
	RemainingDistance int    `json:"remaining_distance"`
	TransitToDate     string `json:"transit_to_date"`
	Reason            string `json:"reason"`
}

// EWayBillService orchestrates the E-Way Bill lifecycle and DB persistence.
type EWayBillService struct {
	db     *sql.DB
	client intEWB.Client
	bus    events.EventBus
	logger *slog.Logger
	cfg    Config
}

// NewEWayBillService constructs an EWayBillService.
func NewEWayBillService(db *sql.DB, bus events.EventBus, client intEWB.Client, logger *slog.Logger, cfg Config) *EWayBillService {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		client = intEWB.NewClient(intEWB.Config{Enabled: true})
	}
	if cfg.MinInvoiceValue == 0 {
		cfg.MinInvoiceValue = 50000.0
	}
	if cfg.ExtensionKM == 0 {
		cfg.ExtensionKM = 5.0
	}
	return &EWayBillService{
		db:     db,
		client: client,
		bus:    bus,
		logger: logger,
		cfg:    cfg,
	}
}

// GeneratePartA creates a Part-A E-Way Bill for a trip.
func (s *EWayBillService) GeneratePartA(ctx context.Context, req GeneratePartARequest) (*EWayBillRecord, error) {
	// 0. Replay / Idempotency check: if an active/part_a EWB already exists for this trip, return it
	var existingRec EWayBillRecord
	var existingVehNum sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, trip_id, ewb_number, status, generation_date, valid_until,
		       from_place, from_state_code, to_place, to_state_code,
		       goods_value, distance, doc_type, doc_no, doc_date,
		       qr_code, gen_mode, vehicle_number, created_at
		FROM eway_bills
		WHERE trip_id = ? AND status != 'cancelled'
		LIMIT 1
	`, req.TripID).Scan(
		&existingRec.ID, &existingRec.TripID, &existingRec.EwbNumber, &existingRec.Status,
		&existingRec.GenerationDate, &existingRec.ValidUntil, &existingRec.FromPlace,
		&existingRec.FromStateCode, &existingRec.ToPlace, &existingRec.ToStateCode,
		&existingRec.GoodsValue, &existingRec.Distance, &existingRec.DocType,
		&existingRec.DocNo, &existingRec.DocDate, &existingRec.QRCode,
		&existingRec.GenMode, &existingVehNum, &existingRec.CreatedAt,
	)
	if err == nil && existingRec.EwbNumber != "" {
		if existingVehNum.Valid {
			existingRec.VehicleNumber = &existingVehNum.String
		}
		existingRec.ValidUpto = existingRec.ValidUntil.Format(time.RFC3339)
		return &existingRec, nil
	}

	// 1. Resolve trip, route, customer, booking data
	var bookingPrice, routeDist, standardFare float64
	var tripNumber, routeSource, routeDest string
	var custGST, compGST, compState, vehicleNumber sql.NullString

	err = s.db.QueryRowContext(ctx, `
		SELECT t.trip_number, r.source, r.destination, r.distance, r.standard_fare,
		       b.price, c.gst, cs.gst_number, cs.state_code, v.registration_number
		FROM trips t
		JOIN bookings b ON t.booking_id = b.id
		JOIN routes r ON t.route_id = r.id
		JOIN customers c ON b.customer_id = c.id
		LEFT JOIN company_settings cs ON 1=1
		LEFT JOIN vehicles v ON t.vehicle_id = v.id
		WHERE t.id = ?
	`, req.TripID).Scan(
		&tripNumber, &routeSource, &routeDest, &routeDist, &standardFare,
		&bookingPrice, &custGST, &compGST, &compState, &vehicleNumber,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.logger.Warn("could not fetch complete trip details, using request values", "error", err)
	}

	goodsValue := req.GoodsValue
	if goodsValue <= 0 {
		goodsValue = bookingPrice
	}
	if goodsValue <= 0 {
		goodsValue = standardFare
	}

	// Value threshold check
	if goodsValue <= s.cfg.MinInvoiceValue && !req.Force {
		return nil, ErrGoodsValueTooLow
	}

	// Check if an invoice already exists for this trip / booking
	var existingInvNum sql.NullString
	_ = s.db.QueryRowContext(ctx, `
		SELECT invoice_number FROM invoices
		WHERE (trip_id = ? OR booking_id = (SELECT booking_id FROM trips WHERE id = ?))
		  AND status != 'cancelled'
		LIMIT 1
	`, req.TripID, req.TripID).Scan(&existingInvNum)

	if req.DocType == "" {
		req.DocType = "INV"
	}
	if req.DocNo == "" {
		if existingInvNum.Valid && existingInvNum.String != "" {
			req.DocNo = existingInvNum.String
		} else {
			req.DocNo = tripNumber
		}
	}
	if req.DocDate == "" {
		req.DocDate = time.Now().Format("2006-01-02")
	}
	if req.FromPlace == "" {
		req.FromPlace = routeSource
	}
	if req.ToPlace == "" {
		req.ToPlace = routeDest
	}
	if req.Distance <= 0 {
		req.Distance = int(routeDist)
	}
	if req.FromGSTIN == "" {
		req.FromGSTIN = compGST.String
	}
	if req.ToGSTIN == "" {
		req.ToGSTIN = custGST.String
	}
	if req.FromStateCode == "" {
		if compState.Valid && compState.String != "" {
			req.FromStateCode = compState.String
		} else if len(req.FromGSTIN) >= 2 {
			req.FromStateCode = req.FromGSTIN[:2]
		} else {
			req.FromStateCode = "27"
		}
	}
	if req.ToStateCode == "" {
		if len(req.ToGSTIN) >= 2 {
			req.ToStateCode = req.ToGSTIN[:2]
		} else {
			req.ToStateCode = "07"
		}
	}
	if req.GenMode == "" {
		req.GenMode = "MANUAL"
	}

	// 2. Call integration client
	genReq := intEWB.GenerateRequest{
		DocumentNumber: req.DocNo,
		FromGSTIN:      req.FromGSTIN,
		ToGSTIN:        req.ToGSTIN,
		TransporterID:  req.TransporterID,
		VehicleNumber:  vehicleNumber.String,
		Distance:       req.Distance,
		TotalAmount:    goodsValue,
	}

	clientResp, err := s.client.GeneratePartA(ctx, genReq)
	if err != nil {
		return nil, fmt.Errorf("ewaybill provider error: %w", err)
	}

	// 3. Persist to eway_bills
	id := uuid.NewString()
	now := time.Now().UTC()
	status := "active"

	partABytes, _ := json.Marshal(req)
	partAJSON := string(partABytes)

	var vehNumParam interface{}
	if vehicleNumber.Valid && vehicleNumber.String != "" {
		vehNumParam = vehicleNumber.String
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO eway_bills (
			id, trip_id, ewb_number, status, generation_date, valid_until,
			from_place, from_state_code, to_place, to_state_code,
			goods_value, distance, doc_type, doc_no, doc_date,
			transporter_id, transporter_doc_no, qr_code, gen_mode,
			part_a_json, vehicle_number, created_at
		) VALUES (
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?
		)
		ON CONFLICT(trip_id) DO UPDATE SET
			ewb_number = excluded.ewb_number,
			status = excluded.status,
			valid_until = excluded.valid_until,
			qr_code = excluded.qr_code,
			part_a_json = excluded.part_a_json,
			gen_mode = excluded.gen_mode
	`,
		id, req.TripID, clientResp.EwbNumber, status, clientResp.GeneratedAt, clientResp.ValidUpto,
		req.FromPlace, req.FromStateCode, req.ToPlace, req.ToStateCode,
		goodsValue, req.Distance, req.DocType, req.DocNo, req.DocDate,
		req.TransporterID, req.TransporterDocNo, clientResp.QRCode, req.GenMode,
		partAJSON, vehNumParam, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to persist eway bill: %w", err)
	}

	// 4. Update trip eway_bill_ref
	_, _ = s.db.ExecContext(ctx, `UPDATE trips SET eway_bill_ref = ? WHERE id = ?`, clientResp.EwbNumber, req.TripID)

	// 5. Update invoice ewb_number cross-link
	_, _ = s.db.ExecContext(ctx, `
		UPDATE invoices
		SET ewb_number = ?, updated_at = datetime('now')
		WHERE (trip_id = ? OR booking_id = (SELECT booking_id FROM trips WHERE id = ?))
		  AND (ewb_number IS NULL OR ewb_number = '')
	`, clientResp.EwbNumber, req.TripID, req.TripID)

	// 6. Append eway_bill_events row
	s.logEvent(ctx, clientResp.EwbNumber, req.TripID, "PART_A_GENERATED", partAJSON, "system")

	return &EWayBillRecord{
		ID:             id,
		TripID:         req.TripID,
		EwbNumber:      clientResp.EwbNumber,
		Status:         status,
		GenerationDate: clientResp.GeneratedAt,
		ValidUntil:     clientResp.ValidUpto,
		ValidUpto:      clientResp.ValidUpto.Format(time.RFC3339),
		QRCode:         clientResp.QRCode,
		FromPlace:      req.FromPlace,
		FromStateCode:  req.FromStateCode,
		ToPlace:        req.ToPlace,
		ToStateCode:    req.ToStateCode,
		GoodsValue:     goodsValue,
		Distance:       req.Distance,
		DocType:        req.DocType,
		DocNo:          req.DocNo,
		DocDate:        req.DocDate,
		GenMode:        req.GenMode,
		CreatedAt:      now,
	}, nil
}

// AttachPartB attaches vehicle info to an existing EWB.
func (s *EWayBillService) AttachPartB(ctx context.Context, ewbNumber, vehicleNumber, transporterID string) (*EWayBillRecord, error) {
	if ewbNumber == "" || vehicleNumber == "" {
		return nil, errors.New("ewb_number and vehicle_number are required")
	}

	var tripID sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT trip_id FROM eway_bills WHERE ewb_number = ?`, ewbNumber).Scan(&tripID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEWBNotFound
		}
		return nil, err
	}

	_, err = s.client.AttachPartB(ctx, ewbNumber, vehicleNumber, transporterID)
	if err != nil {
		return nil, fmt.Errorf("ewaybill provider error: %w", err)
	}

	partBPayload := map[string]string{
		"vehicle_number": vehicleNumber,
		"transporter_id": transporterID,
		"attached_at":    time.Now().UTC().Format(time.RFC3339),
	}
	partBBytes, _ := json.Marshal(partBPayload)

	_, err = s.db.ExecContext(ctx, `
		UPDATE eway_bills
		SET vehicle_number = ?, transporter_id = ?, part_b_json = ?, status = 'active'
		WHERE ewb_number = ?
	`, vehicleNumber, transporterID, string(partBBytes), ewbNumber)
	if err != nil {
		return nil, err
	}

	s.logEvent(ctx, ewbNumber, tripID.String, "PART_B_ADDED", string(partBBytes), "system")

	return s.GetByNumber(ctx, ewbNumber)
}

// Extend extends E-Way Bill validity when geofence evidence is verified.
func (s *EWayBillService) Extend(ctx context.Context, ewbNumber string, req ExtendRequest) (*EWayBillRecord, error) {
	var tripID sql.NullString
	var status string
	var extCount int
	var validUntil time.Time
	var vehicleID sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT e.trip_id, e.status, e.extension_count, e.valid_until, t.vehicle_id
		FROM eway_bills e
		LEFT JOIN trips t ON e.trip_id = t.id
		WHERE e.ewb_number = ?
	`, ewbNumber).Scan(&tripID, &status, &extCount, &validUntil, &vehicleID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEWBNotFound
		}
		return nil, err
	}

	if status != "active" {
		return nil, ErrNotActive
	}

	if extCount >= 1 {
		return nil, ErrExtensionLimitExceeded
	}

	// Check geofence evidence
	hasEvidence := false
	if vehicleID.Valid && vehicleID.String != "" && tripID.Valid && tripID.String != "" {
		hasEvidence = s.verifyGeofenceEvidence(ctx, tripID.String, vehicleID.String)
	}

	if !hasEvidence {
		s.logEvent(ctx, ewbNumber, tripID.String, "EXTENSION_DENIED", `{"reason":"no_geofence_evidence"}`, "system")
		return nil, ErrNoGeofenceEvidence
	}

	req.EwbNumber = ewbNumber
	clientResp, err := s.client.Extend(ctx, ewbNumber, intEWB.ExtendRequest{
		EwbNumber:         req.EwbNumber,
		FromPlace:         req.FromPlace,
		FromStateCode:     req.FromStateCode,
		RemainingDistance: req.RemainingDistance,
		TransitToDate:     req.TransitToDate,
		Reason:            req.Reason,
	})
	if err != nil {
		return nil, fmt.Errorf("ewaybill provider extend error: %w", err)
	}

	newValidUntil := validUntil.Add(24 * time.Hour)
	if !clientResp.ValidUpto.IsZero() {
		newValidUntil = clientResp.ValidUpto
	}

	extBytes, _ := json.Marshal(req)
	_, err = s.db.ExecContext(ctx, `
		UPDATE eway_bills
		SET valid_until = ?, extension_count = extension_count + 1, status = 'active'
		WHERE ewb_number = ?
	`, newValidUntil, ewbNumber)
	if err != nil {
		return nil, err
	}

	s.logEvent(ctx, ewbNumber, tripID.String, "EXTENDED", string(extBytes), "system")

	return s.GetByNumber(ctx, ewbNumber)
}

// Cancel cancels an EWB.
func (s *EWayBillService) Cancel(ctx context.Context, ewbNumber, reason string) (*EWayBillRecord, error) {
	var tripID sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT trip_id FROM eway_bills WHERE ewb_number = ?`, ewbNumber).Scan(&tripID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEWBNotFound
		}
		return nil, err
	}

	_, err = s.client.Cancel(ctx, ewbNumber, reason)
	if err != nil {
		return nil, fmt.Errorf("ewaybill provider cancel error: %w", err)
	}

	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
		UPDATE eway_bills
		SET status = 'cancelled', cancel_reason = ?, cancelled_at = ?
		WHERE ewb_number = ?
	`, reason, now, ewbNumber)
	if err != nil {
		return nil, err
	}

	s.logEvent(ctx, ewbNumber, tripID.String, "CANCELLED", fmt.Sprintf(`{"reason":%q}`, reason), "system")

	return s.GetByNumber(ctx, ewbNumber)
}

// GetByTrip returns the latest active EWB record for a trip.
func (s *EWayBillService) GetByTrip(ctx context.Context, tripID string) (*EWayBillRecord, error) {
	return s.getByQuery(ctx, `SELECT id, trip_id, ewb_number, irn, status, generation_date, valid_until, vehicle_number, transporter_id, qr_code, from_place, from_state_code, to_place, to_state_code, goods_value, distance, doc_type, doc_no, doc_date, transporter_doc_no, extension_count, cancel_reason, cancelled_at, gen_mode, created_at FROM eway_bills WHERE trip_id = ? ORDER BY created_at DESC LIMIT 1`, tripID)
}

// GetByNumber returns the EWB record by ewb_number.
func (s *EWayBillService) GetByNumber(ctx context.Context, ewbNumber string) (*EWayBillRecord, error) {
	return s.getByQuery(ctx, `SELECT id, trip_id, ewb_number, irn, status, generation_date, valid_until, vehicle_number, transporter_id, qr_code, from_place, from_state_code, to_place, to_state_code, goods_value, distance, doc_type, doc_no, doc_date, transporter_doc_no, extension_count, cancel_reason, cancelled_at, gen_mode, created_at FROM eway_bills WHERE ewb_number = ? LIMIT 1`, ewbNumber)
}

func (s *EWayBillService) getByQuery(ctx context.Context, query string, arg string) (*EWayBillRecord, error) {
	var rec EWayBillRecord
	var tripID, irn, vehNum, transID, qrCode, fromP, fromS, toP, toS, docT, docN, docD, transDocN, cancelR, genMode sql.NullString
	var cancelledAt sql.NullTime

	err := s.db.QueryRowContext(ctx, query, arg).Scan(
		&rec.ID, &tripID, &rec.EwbNumber, &irn, &rec.Status, &rec.GenerationDate, &rec.ValidUntil,
		&vehNum, &transID, &qrCode, &fromP, &fromS, &toP, &toS, &rec.GoodsValue, &rec.Distance,
		&docT, &docN, &docD, &transDocN, &rec.ExtensionCount, &cancelR, &cancelledAt, &genMode, &rec.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEWBNotFound
		}
		return nil, err
	}

	rec.TripID = tripID.String
	if irn.Valid {
		rec.IRN = &irn.String
	}
	if vehNum.Valid && vehNum.String != "" {
		rec.VehicleNumber = &vehNum.String
	}
	if transID.Valid {
		rec.TransporterID = &transID.String
	}
	rec.QRCode = qrCode.String
	rec.FromPlace = fromP.String
	rec.FromStateCode = fromS.String
	rec.ToPlace = toP.String
	rec.ToStateCode = toS.String
	rec.DocType = docT.String
	rec.DocNo = docN.String
	rec.DocDate = docD.String
	rec.TransporterDocNo = transDocN.String
	rec.CancelReason = cancelR.String
	if cancelledAt.Valid {
		rec.CancelledAt = &cancelledAt.Time
	}
	rec.GenMode = genMode.String
	rec.ValidUpto = rec.ValidUntil.Format("2006-01-02 15:04:05")

	return &rec, nil
}

func (s *EWayBillService) verifyGeofenceEvidence(ctx context.Context, tripID, vehicleID string) bool {
	var lat, lng float64
	err := s.db.QueryRowContext(ctx, `
		SELECT latitude, longitude 
		FROM vehicle_latest_position 
		WHERE vehicle_id = ? LIMIT 1`, vehicleID).Scan(&lat, &lng)
	if err != nil {
		err = s.db.QueryRowContext(ctx, `
			SELECT latitude, longitude 
			FROM telemetry_positions 
			WHERE vehicle_id = ? 
			ORDER BY device_time DESC LIMIT 1`, vehicleID).Scan(&lat, &lng)
		if err != nil {
			err = s.db.QueryRowContext(ctx, `
				SELECT latitude, longitude 
				FROM telemetry_snapshots 
				WHERE vehicle_id = ? 
				ORDER BY timestamp DESC LIMIT 1`, vehicleID).Scan(&lat, &lng)
			if err != nil {
				return false
			}
		}
	}

	var destLat, destLng sql.NullFloat64
	err = s.db.QueryRowContext(ctx, `
		SELECT g.center_lat, g.center_lng
		FROM geofences g
		JOIN trips t ON (g.route_name = t.route_id OR g.name LIKE '%' || t.trip_number || '%' OR g.id = t.route_id)
		WHERE t.id = ? AND g.kind IN ('drop', 'destination') AND g.is_active = 1
		LIMIT 1`, tripID).Scan(&destLat, &destLng)
	if err != nil || !destLat.Valid || !destLng.Valid {
		// Fallback: If no explicit drop geofence is defined, vehicle presence is evidence
		return true
	}

	dist := haversineDistance(lat, lng, destLat.Float64, destLng.Float64)
	return dist <= s.cfg.ExtensionKM
}

func (s *EWayBillService) logEvent(ctx context.Context, ewbNumber, tripID, eventType, payload, createdBy string) {
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO eway_bill_events (id, ewb_number, trip_id, event_type, payload, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
	`, id, ewbNumber, tripID, eventType, payload, createdBy)
	if err != nil {
		s.logger.Warn("failed to log eway bill event", "ewb", ewbNumber, "type", eventType, "error", err)
	}
}
