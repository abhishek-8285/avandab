package tyre

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrTyreNotFound       = errors.New("tyre not found")
	ErrDuplicateSerial    = errors.New("tyre with this serial number already exists")
	ErrInvalidStatus      = errors.New("invalid tyre status for this operation")
	ErrAxleOccupied       = errors.New("axle position is already occupied by another mounted tyre")
	ErrMaxRetreadsReached = errors.New("tyre has reached its maximum allowable retreads")
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateTyre(ctx context.Context, tenantID string, in CreateTyreInput) (*Tyre, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	existing, err := s.repo.GetBySerialNumber(ctx, tenantID, in.SerialNumber)
	if err == nil && existing != nil {
		return nil, ErrDuplicateSerial
	}

	initialTread := 15.0
	if in.InitialTreadDepthMM != nil && *in.InitialTreadDepthMM > 0 {
		initialTread = *in.InitialTreadDepthMM
	}
	minTread := 3.0
	if in.MinTreadDepthMM != nil && *in.MinTreadDepthMM > 0 {
		minTread = *in.MinTreadDepthMM
	}
	maxRetreads := 3
	if in.MaxRetreads != nil && *in.MaxRetreads >= 0 {
		maxRetreads = *in.MaxRetreads
	}

	now := time.Now().UTC()
	t := &Tyre{
		ID:                  "tyre-" + uuid.NewString(),
		TenantID:            tenantID,
		SerialNumber:        in.SerialNumber,
		Brand:               in.Brand,
		Model:               in.Model,
		Size:                in.Size,
		TyreType:            in.TyreType,
		Status:              TyreStatusInStock,
		InitialTreadDepthMM: initialTread,
		CurrentTreadDepthMM: initialTread,
		MinTreadDepthMM:     minTread,
		PurchaseDate:        in.PurchaseDate,
		PurchaseCost:        in.PurchaseCost,
		VendorName:          in.VendorName,
		InvoiceNumber:       in.InvoiceNumber,
		RetreadCount:        0,
		MaxRetreads:         maxRetreads,
		TotalRetreadCost:    0.0,
		TotalKMRun:          0.0,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	if err := s.repo.Create(ctx, t); err != nil {
		return nil, err
	}

	metrics := t.ComputeMetrics(nil)
	t.Metrics = &metrics
	return t, nil
}

func (s *Service) GetTyre(ctx context.Context, tenantID, id string) (*Tyre, error) {
	t, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, ErrTyreNotFound
	}

	var vehicleOdo *float64
	if t.VehicleID != nil {
		vehicleOdo, _ = s.repo.GetVehicleLatestOdometer(ctx, tenantID, *t.VehicleID)
	}

	metrics := t.ComputeMetrics(vehicleOdo)
	t.Metrics = &metrics
	return t, nil
}

func (s *Service) ListTyres(ctx context.Context, filter TyreFilter) ([]Tyre, int, error) {
	tyres, total, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	for i := range tyres {
		var vehicleOdo *float64
		if tyres[i].VehicleID != nil {
			vehicleOdo, _ = s.repo.GetVehicleLatestOdometer(ctx, filter.TenantID, *tyres[i].VehicleID)
		}
		m := tyres[i].ComputeMetrics(vehicleOdo)
		tyres[i].Metrics = &m
	}

	return tyres, total, nil
}

func (s *Service) UpdateTyre(ctx context.Context, tenantID, id string, in UpdateTyreInput) (*Tyre, error) {
	t, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, ErrTyreNotFound
	}

	if in.Brand != nil && strings.TrimSpace(*in.Brand) != "" {
		t.Brand = strings.TrimSpace(*in.Brand)
	}
	if in.Model != nil {
		t.Model = strings.TrimSpace(*in.Model)
	}
	if in.Size != nil && strings.TrimSpace(*in.Size) != "" {
		t.Size = strings.TrimSpace(*in.Size)
	}
	if in.VendorName != nil {
		t.VendorName = strings.TrimSpace(*in.VendorName)
	}
	if in.InvoiceNumber != nil {
		t.InvoiceNumber = strings.TrimSpace(*in.InvoiceNumber)
	}
	if in.PurchaseCost != nil && *in.PurchaseCost >= 0 {
		t.PurchaseCost = *in.PurchaseCost
	}
	if in.PurchaseDate != nil {
		t.PurchaseDate = in.PurchaseDate
	}

	if err := s.repo.Update(ctx, t); err != nil {
		return nil, err
	}

	var vehicleOdo *float64
	if t.VehicleID != nil {
		vehicleOdo, _ = s.repo.GetVehicleLatestOdometer(ctx, tenantID, *t.VehicleID)
	}
	m := t.ComputeMetrics(vehicleOdo)
	t.Metrics = &m
	return t, nil
}

func (s *Service) MountTyre(ctx context.Context, tenantID, id string, in MountTyreInput) (*Tyre, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	t, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, ErrTyreNotFound
	}
	if t.Status != TyreStatusInStock {
		return nil, fmt.Errorf("%w: tyre status is %s (must be IN_STOCK to mount)", ErrInvalidStatus, t.Status)
	}

	// Verify axle position is not already taken by another mounted tyre
	mountedTyres, err := s.repo.GetVehicleMountedTyres(ctx, tenantID, in.VehicleID)
	if err != nil {
		return nil, err
	}
	for _, mt := range mountedTyres {
		if mt.AxlePosition != nil && strings.EqualFold(*mt.AxlePosition, in.AxlePosition) {
			return nil, fmt.Errorf("%w: vehicle %s already has tyre %s at %s", ErrAxleOccupied, in.VehicleID, mt.SerialNumber, in.AxlePosition)
		}
	}

	now := time.Now().UTC()
	mountDate := now
	if in.MountDate != nil {
		mountDate = *in.MountDate
	}

	mountOdo := in.MountOdometerKM
	if mountOdo <= 0 {
		if odo, _ := s.repo.GetVehicleLatestOdometer(ctx, tenantID, in.VehicleID); odo != nil {
			mountOdo = *odo
		}
	}

	t.Status = TyreStatusMounted
	t.VehicleID = &in.VehicleID
	t.AxlePosition = &in.AxlePosition
	t.MountOdometerKM = &mountOdo
	t.MountDate = &mountDate

	if err := s.repo.Update(ctx, t); err != nil {
		return nil, err
	}

	act := &TyreActivity{
		ID:           "act-" + uuid.NewString(),
		TenantID:     tenantID,
		TyreID:       t.ID,
		ActivityType: ActivityMount,
		VehicleID:    &in.VehicleID,
		ToPosition:   &in.AxlePosition,
		OdometerKM:   mountOdo,
		TreadDepthMM: &t.CurrentTreadDepthMM,
		Notes:        in.Notes,
		PerformedBy:  in.PerformedBy,
		RecordedAt:   now,
	}
	_ = s.repo.RecordActivity(ctx, act)

	m := t.ComputeMetrics(&mountOdo)
	t.Metrics = &m
	return t, nil
}

func (s *Service) DismountTyre(ctx context.Context, tenantID, id string, in DismountTyreInput) (*Tyre, error) {
	t, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, ErrTyreNotFound
	}
	if t.Status != TyreStatusMounted {
		return nil, fmt.Errorf("%w: tyre status is %s (must be MOUNTED to dismount)", ErrInvalidStatus, t.Status)
	}

	dismountOdo := in.DismountOdometerKM
	if dismountOdo <= 0 && t.VehicleID != nil {
		if odo, _ := s.repo.GetVehicleLatestOdometer(ctx, tenantID, *t.VehicleID); odo != nil {
			dismountOdo = *odo
		}
	}

	var runKM float64
	if t.MountOdometerKM != nil && dismountOdo > *t.MountOdometerKM {
		runKM = dismountOdo - *t.MountOdometerKM
	}

	t.TotalKMRun += runKM
	oldVehID := t.VehicleID
	oldPos := t.AxlePosition

	t.Status = TyreStatusInStock
	t.VehicleID = nil
	t.AxlePosition = nil
	t.MountOdometerKM = nil
	t.MountDate = nil

	if err := s.repo.Update(ctx, t); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	act := &TyreActivity{
		ID:           "act-" + uuid.NewString(),
		TenantID:     tenantID,
		TyreID:       t.ID,
		ActivityType: ActivityDismount,
		VehicleID:    oldVehID,
		FromPosition: oldPos,
		OdometerKM:   dismountOdo,
		TreadDepthMM: &t.CurrentTreadDepthMM,
		Notes:        in.Notes,
		PerformedBy:  in.PerformedBy,
		RecordedAt:   now,
	}
	_ = s.repo.RecordActivity(ctx, act)

	m := t.ComputeMetrics(nil)
	t.Metrics = &m
	return t, nil
}

func (s *Service) RotateTyres(ctx context.Context, tenantID string, in RotateTyresInput) error {
	in.VehicleID = strings.TrimSpace(in.VehicleID)
	if in.VehicleID == "" {
		return errors.New("vehicle_id is required")
	}
	if len(in.Rotations) == 0 {
		return errors.New("rotations list cannot be empty")
	}

	odo := in.OdometerKM
	if odo <= 0 {
		if latestOdo, _ := s.repo.GetVehicleLatestOdometer(ctx, tenantID, in.VehicleID); latestOdo != nil {
			odo = *latestOdo
		}
	}

	now := time.Now().UTC()
	targetPositions := make(map[string]bool)
	var activities []*TyreActivity

	for _, rot := range in.Rotations {
		toPos := strings.ToUpper(strings.TrimSpace(rot.ToPosition))
		if toPos == "" {
			return errors.New("target position cannot be empty")
		}
		if targetPositions[toPos] {
			return fmt.Errorf("duplicate target position %s in rotation request", toPos)
		}
		targetPositions[toPos] = true

		t, err := s.repo.GetByID(ctx, tenantID, rot.TyreID)
		if err != nil {
			return fmt.Errorf("tyre %s not found: %w", rot.TyreID, err)
		}
		if t.Status != TyreStatusMounted {
			return fmt.Errorf("tyre %s is not mounted", t.SerialNumber)
		}
		if t.VehicleID == nil || *t.VehicleID != in.VehicleID {
			return fmt.Errorf("tyre %s is not mounted on vehicle %s", t.SerialNumber, in.VehicleID)
		}

		fromPos := ""
		if t.AxlePosition != nil {
			fromPos = *t.AxlePosition
		}

		act := &TyreActivity{
			ID:           "act-" + uuid.NewString(),
			TenantID:     tenantID,
			TyreID:       t.ID,
			ActivityType: ActivityRotation,
			VehicleID:    &in.VehicleID,
			FromPosition: &fromPos,
			ToPosition:   &toPos,
			OdometerKM:   odo,
			TreadDepthMM: &t.CurrentTreadDepthMM,
			Notes:        in.Notes,
			PerformedBy:  in.PerformedBy,
			RecordedAt:   now,
		}
		activities = append(activities, act)
	}

	return s.repo.RotateTyresTx(ctx, tenantID, in.VehicleID, in.Rotations, activities)
}

func (s *Service) RecordInspection(ctx context.Context, tenantID, id string, in RecordInspectionInput) (*TyreInspection, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	t, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, ErrTyreNotFound
	}

	odo := in.OdometerKM
	if odo <= 0 && t.VehicleID != nil {
		if latestOdo, _ := s.repo.GetVehicleLatestOdometer(ctx, tenantID, *t.VehicleID); latestOdo != nil {
			odo = *latestOdo
		}
	}

	now := time.Now().UTC()
	inspDate := now
	if in.InspectionDate != nil {
		inspDate = *in.InspectionDate
	}

	insp := &TyreInspection{
		ID:             "insp-" + uuid.NewString(),
		TenantID:       tenantID,
		TyreID:         t.ID,
		VehicleID:      t.VehicleID,
		InspectionDate: inspDate,
		OdometerKM:     odo,
		TreadDepthMM:   in.TreadDepthMM,
		PressurePSI:    in.PressurePSI,
		Condition:      in.Condition,
		ActionTaken:    in.ActionTaken,
		InspectorName:  in.InspectorName,
		Notes:          in.Notes,
		CreatedAt:      now,
	}

	if err := s.repo.RecordInspection(ctx, insp); err != nil {
		return nil, err
	}

	// Update tyre current tread depth
	t.CurrentTreadDepthMM = in.TreadDepthMM
	_ = s.repo.Update(ctx, t)

	// Record activity log
	act := &TyreActivity{
		ID:           "act-" + uuid.NewString(),
		TenantID:     tenantID,
		TyreID:       t.ID,
		ActivityType: ActivityInspection,
		VehicleID:    t.VehicleID,
		FromPosition: t.AxlePosition,
		ToPosition:   t.AxlePosition,
		OdometerKM:   odo,
		TreadDepthMM: &in.TreadDepthMM,
		PressurePSI:  in.PressurePSI,
		Notes:        fmt.Sprintf("Inspection: %s, Action: %s", in.Condition, in.ActionTaken),
		PerformedBy:  in.InspectorName,
		RecordedAt:   now,
	}
	_ = s.repo.RecordActivity(ctx, act)

	return insp, nil
}

func (s *Service) RetreadTyre(ctx context.Context, tenantID, id string, in RetreadTyreInput) (*Tyre, error) {
	t, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, ErrTyreNotFound
	}
	if t.Status == TyreStatusScrapped {
		return nil, fmt.Errorf("%w: scrapped tyres cannot be retreaded", ErrInvalidStatus)
	}

	action := strings.ToUpper(strings.TrimSpace(in.Action))
	now := time.Now().UTC()

	switch action {
	case "SEND":
		if t.RetreadCount >= t.MaxRetreads {
			return nil, ErrMaxRetreadsReached
		}
		if t.Status == TyreStatusMounted {
			// Dismount first
			if err := s.dismountInternal(ctx, tenantID, t, in.PerformedBy, "Sent for retreading"); err != nil {
				return nil, err
			}
		}

		t.Status = TyreStatusUnderRetread
		if err := s.repo.Update(ctx, t); err != nil {
			return nil, err
		}

		act := &TyreActivity{
			ID:           "act-" + uuid.NewString(),
			TenantID:     tenantID,
			TyreID:       t.ID,
			ActivityType: ActivityRetreadSend,
			Cost:         in.Cost,
			Notes:        fmt.Sprintf("Vendor: %s, %s", in.VendorName, in.Notes),
			PerformedBy:  in.PerformedBy,
			RecordedAt:   now,
		}
		_ = s.repo.RecordActivity(ctx, act)

	case "RECEIVE":
		if t.Status != TyreStatusUnderRetread {
			return nil, fmt.Errorf("%w: tyre is %s (must be UNDER_RETREAD to receive)", ErrInvalidStatus, t.Status)
		}

		newTread := 14.0
		if in.NewTreadDepthMM != nil && *in.NewTreadDepthMM > 0 {
			newTread = *in.NewTreadDepthMM
		}

		t.Status = TyreStatusInStock
		t.RetreadCount++
		t.TotalRetreadCost += in.Cost
		t.CurrentTreadDepthMM = newTread

		if err := s.repo.Update(ctx, t); err != nil {
			return nil, err
		}

		act := &TyreActivity{
			ID:           "act-" + uuid.NewString(),
			TenantID:     tenantID,
			TyreID:       t.ID,
			ActivityType: ActivityRetreadReceive,
			Cost:         in.Cost,
			TreadDepthMM: &newTread,
			Notes:        fmt.Sprintf("Retread #%d complete. Vendor: %s, %s", t.RetreadCount, in.VendorName, in.Notes),
			PerformedBy:  in.PerformedBy,
			RecordedAt:   now,
		}
		_ = s.repo.RecordActivity(ctx, act)

	default:
		return nil, errors.New("action must be 'SEND' or 'RECEIVE'")
	}

	m := t.ComputeMetrics(nil)
	t.Metrics = &m
	return t, nil
}

func (s *Service) ScrapTyre(ctx context.Context, tenantID, id string, in ScrapTyreInput) (*Tyre, error) {
	t, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, ErrTyreNotFound
	}
	if t.Status == TyreStatusScrapped {
		return t, nil
	}

	if t.Status == TyreStatusMounted {
		if err := s.dismountInternal(ctx, tenantID, t, in.PerformedBy, "Scrapped: "+in.Reason); err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	t.Status = TyreStatusScrapped
	t.ScrapDate = &now
	t.ScrapReason = in.Reason
	t.ScrapRecoveryAmount = in.RecoveryAmount

	if err := s.repo.Update(ctx, t); err != nil {
		return nil, err
	}

	act := &TyreActivity{
		ID:           "act-" + uuid.NewString(),
		TenantID:     tenantID,
		TyreID:       t.ID,
		ActivityType: ActivityScrap,
		Cost:         -in.RecoveryAmount,
		Notes:        fmt.Sprintf("Reason: %s, Recovery: ₹%.2f, %s", in.Reason, in.RecoveryAmount, in.Notes),
		PerformedBy:  in.PerformedBy,
		RecordedAt:   now,
	}
	_ = s.repo.RecordActivity(ctx, act)

	m := t.ComputeMetrics(nil)
	t.Metrics = &m
	return t, nil
}

func (s *Service) GetVehicleTyres(ctx context.Context, tenantID, vehicleID string) ([]Tyre, error) {
	tyres, err := s.repo.GetVehicleMountedTyres(ctx, tenantID, vehicleID)
	if err != nil {
		return nil, err
	}

	odo, _ := s.repo.GetVehicleLatestOdometer(ctx, tenantID, vehicleID)
	for i := range tyres {
		m := tyres[i].ComputeMetrics(odo)
		tyres[i].Metrics = &m
	}
	return tyres, nil
}

func (s *Service) GetFleetSummary(ctx context.Context, tenantID string) (*FleetTyreSummary, error) {
	return s.repo.GetFleetSummary(ctx, tenantID)
}

func (s *Service) ListActivities(ctx context.Context, tenantID, tyreID string) ([]TyreActivity, error) {
	return s.repo.ListActivities(ctx, tenantID, tyreID)
}

func (s *Service) ListInspections(ctx context.Context, tenantID, tyreID string) ([]TyreInspection, error) {
	return s.repo.ListInspections(ctx, tenantID, tyreID)
}

func (s *Service) dismountInternal(ctx context.Context, tenantID string, t *Tyre, performedBy, reason string) error {
	var odoKM float64
	if t.VehicleID != nil {
		if odo, _ := s.repo.GetVehicleLatestOdometer(ctx, tenantID, *t.VehicleID); odo != nil {
			odoKM = *odo
		}
	}
	if t.MountOdometerKM != nil && odoKM > *t.MountOdometerKM {
		t.TotalKMRun += (odoKM - *t.MountOdometerKM)
	}

	oldVeh := t.VehicleID
	oldPos := t.AxlePosition

	t.Status = TyreStatusInStock
	t.VehicleID = nil
	t.AxlePosition = nil
	t.MountOdometerKM = nil
	t.MountDate = nil

	act := &TyreActivity{
		ID:           "act-" + uuid.NewString(),
		TenantID:     tenantID,
		TyreID:       t.ID,
		ActivityType: ActivityDismount,
		VehicleID:    oldVeh,
		FromPosition: oldPos,
		OdometerKM:   odoKM,
		TreadDepthMM: &t.CurrentTreadDepthMM,
		Notes:        reason,
		PerformedBy:  performedBy,
		RecordedAt:   time.Now().UTC(),
	}
	return s.repo.RecordActivity(ctx, act)
}
