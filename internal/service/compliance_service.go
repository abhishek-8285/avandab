package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/domain"
	"transport-app/internal/events"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
)

// localMidnight returns midnight of today in the server's local timezone.
// Expiry dates are stored/parsed as calendar dates (local); comparing them
// against UTC-truncated time.Now() misaligns the two calendars between
// 00:00 and 05:30 IST and silently unblocks expired documents.
func localMidnight() time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location())
}

// ComplianceService enforces legal and operational compliance across drivers and vehicles (Spec 05 §5).
type ComplianceService struct {
	baseService
	opsAlerts *OpsAlertService
}

// NewComplianceService constructs a new ComplianceService.
func NewComplianceService(bs baseService) *ComplianceService {
	return &ComplianceService{baseService: bs}
}

// ComplianceCheckResult represents the outcome of a compliance check.
type ComplianceCheckResult struct {
	Valid   bool     `json:"valid"`
	Blocked bool     `json:"blocked"`
	Reason  string   `json:"reason"`
	Alerts  []string `json:"alerts"`
}

// ComplianceExemption represents a temporary exemption granted for a document (Spec 05 §5).
type ComplianceExemption struct {
	ID          string    `json:"id"`
	EntityType  string    `json:"entity_type"`
	EntityID    string    `json:"entity_id"`
	DocType     string    `json:"doc_type"`
	Reason      string    `json:"reason"`
	ExemptUntil time.Time `json:"exempt_until"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// DocStatus represents individual document verification status.
type DocStatus struct {
	DocType    string     `json:"doc_type"`
	Expiry     *time.Time `json:"expiry,omitempty"`
	Status     string     `json:"status"` // valid, warning, expired, missing, exempt
	HasFile    bool       `json:"has_file"`
	IsExempt   bool       `json:"is_exempt"`
	ExemptNote string     `json:"exempt_note,omitempty"`
}

// CheckDispatchCompliance is the master 5-doc dispatch compliance gate (Spec 05 §5).
// It verifies driver license and vehicle RC, Fitness, Insurance, and PUC.
func (s *ComplianceService) CheckDispatchCompliance(ctx context.Context, driverID, vehicleID string) (ComplianceCheckResult, error) {
	result := ComplianceCheckResult{Valid: true, Blocked: false, Alerts: []string{}}

	// 1. Driver Compliance Gate
	if driverID != "" {
		driverRes, err := s.ValidateDriverCompliance(ctx, domain.DriverID(driverID))
		if err != nil {
			return ComplianceCheckResult{Valid: false, Blocked: true, Reason: err.Error()}, err
		}
		if !driverRes.Valid || driverRes.Blocked {
			return driverRes, fmt.Errorf("Dispatch blocked: %s (compliance)", driverRes.Reason)
		}
		result.Alerts = append(result.Alerts, driverRes.Alerts...)
	}

	// 2. Vehicle Compliance Gate
	if vehicleID != "" {
		vehRes, err := s.ValidateVehicleCompliance(ctx, domain.VehicleID(vehicleID))
		if err != nil {
			return ComplianceCheckResult{Valid: false, Blocked: true, Reason: err.Error()}, err
		}
		if !vehRes.Valid || vehRes.Blocked {
			return vehRes, fmt.Errorf("Dispatch blocked: %s (compliance)", vehRes.Reason)
		}
		result.Alerts = append(result.Alerts, vehRes.Alerts...)
	}

	return result, nil
}

// ValidateDriverCompliance checks driver license expiry, status, and exemptions.
func (s *ComplianceService) ValidateDriverCompliance(ctx context.Context, driverID domain.DriverID) (ComplianceCheckResult, error) {
	if s.store == nil {
		return ComplianceCheckResult{Valid: false, Reason: "store uninitialized"}, errors.New("store uninitialized")
	}
	driver, err := s.store.GetDriverByID(ctx, driverID)
	if err != nil {
		return ComplianceCheckResult{Valid: false, Reason: "driver not found"}, err
	}

	result := ComplianceCheckResult{Valid: true, Blocked: false, Alerts: []string{}}

	// Check manual block flag or blocked status
	if driver.Blocked || driver.Status == domain.DriverBlocked {
		reason := "driver status is blocked"
		if driver.BlockedReason != nil && *driver.BlockedReason != "" {
			reason = *driver.BlockedReason
		}
		_ = s.RecordCheck(ctx, "driver", string(driverID), "status", "blocked", reason)
		s.publishBlockedEvent(ctx, "driver", string(driverID), "", reason)
		return ComplianceCheckResult{Valid: false, Blocked: true, Reason: reason}, nil
	}

	if driver.Status == domain.DriverInactive || driver.Status == domain.DriverLeave {
		reason := fmt.Sprintf("driver is not available (status: %s)", driver.Status)
		_ = s.RecordCheck(ctx, "driver", string(driverID), "status", "blocked", reason)
		s.publishBlockedEvent(ctx, "driver", string(driverID), "", reason)
		return ComplianceCheckResult{Valid: false, Blocked: true, Reason: reason}, nil
	}

	now := localMidnight()

	// Check License Expiry
	if !driver.LicenseExpiry.IsZero() {
		if driver.LicenseExpiry.Before(now) {
			// Check exemption
			exempt, ex, _ := s.IsExempt(ctx, "driver", string(driverID), "license")
			if exempt && ex != nil {
				note := fmt.Sprintf("License expired on %s; active exemption until %s (%s)", driver.LicenseExpiry.Format("2006-01-02"), ex.ExemptUntil.Format("2006-01-02"), ex.Reason)
				_ = s.RecordCheck(ctx, "driver", string(driverID), "license", "warning", note)
				s.logAudit(ctx, nil, "compliance_exemption_bypass", "drivers", string(driverID), nil, &note)
				result.Alerts = append(result.Alerts, fmt.Sprintf("Driver license expired but bypassed via active exemption until %s", ex.ExemptUntil.Format("2006-01-02")))
			} else {
				reason := "driver license expired"
				_ = s.RecordCheck(ctx, "driver", string(driverID), "license", "expired", reason)
				s.publishBlockedEvent(ctx, "driver", string(driverID), "", reason)
				return ComplianceCheckResult{Valid: false, Blocked: true, Reason: reason}, nil
			}
		} else if driver.LicenseExpiry.Before(now.Add(7 * 24 * time.Hour)) {
			// 7-day warning window
			days := int(time.Until(driver.LicenseExpiry).Hours() / 24)
			if days < 0 {
				days = 0
			}
			msg := fmt.Sprintf("driver license expires in %d days (%s)", days, driver.LicenseExpiry.Format("2006-01-02"))
			_ = s.RecordCheck(ctx, "driver", string(driverID), "license", "warning", msg)
			result.Alerts = append(result.Alerts, msg)
		} else {
			_ = s.RecordCheck(ctx, "driver", string(driverID), "license", "valid", "license valid")
		}
	}

	return result, nil
}

// ValidateVehicleCompliance checks vehicle RC, Insurance, Fitness, and PUC expiry and exemptions.
func (s *ComplianceService) ValidateVehicleCompliance(ctx context.Context, vehicleID domain.VehicleID) (ComplianceCheckResult, error) {
	if s.store == nil {
		return ComplianceCheckResult{Valid: false, Reason: "store uninitialized"}, errors.New("store uninitialized")
	}
	vehicle, err := s.store.GetVehicleByID(ctx, vehicleID)
	if err != nil {
		return ComplianceCheckResult{Valid: false, Reason: "vehicle not found"}, err
	}

	result := ComplianceCheckResult{Valid: true, Blocked: false, Alerts: []string{}}

	if vehicle.Blocked || vehicle.Status == domain.VehicleBlocked {
		reason := "vehicle status is blocked"
		if vehicle.BlockedReason != nil && *vehicle.BlockedReason != "" {
			reason = *vehicle.BlockedReason
		}
		_ = s.RecordCheck(ctx, "vehicle", string(vehicleID), "status", "blocked", reason)
		s.publishBlockedEvent(ctx, "vehicle", "", string(vehicleID), reason)
		return ComplianceCheckResult{Valid: false, Blocked: true, Reason: reason}, nil
	}

	if vehicle.Status == domain.VehicleInactive || vehicle.Status == domain.VehicleMaintenance {
		reason := fmt.Sprintf("vehicle is not available (status: %s)", vehicle.Status)
		_ = s.RecordCheck(ctx, "vehicle", string(vehicleID), "status", "blocked", reason)
		s.publishBlockedEvent(ctx, "vehicle", "", string(vehicleID), reason)
		return ComplianceCheckResult{Valid: false, Blocked: true, Reason: reason}, nil
	}

	now := localMidnight()

	// Documents list to evaluate: RC/Permit, Fitness, Insurance, PUC
	type docCheck struct {
		docType string
		name    string
		expiry  time.Time
	}

	checks := []docCheck{
		{docType: "rc", name: "vehicle RC/permit", expiry: vehicle.PermitExpiry},
		{docType: "fitness", name: "vehicle fitness", expiry: vehicle.FitnessExpiry},
		{docType: "insurance", name: "vehicle insurance", expiry: vehicle.InsuranceExpiry},
	}

	// Fetch PUC expiry from vehicles table or domain entity
	var pucTime time.Time
	if vehicle.PUCExpiry != nil && !vehicle.PUCExpiry.IsZero() {
		pucTime = *vehicle.PUCExpiry
	} else if dbGetter, ok := s.store.(repository.DBGetter); ok {
		var pucStr sql.NullString
		_ = dbGetter.DB().QueryRowContext(ctx, `SELECT puc_expiry FROM vehicles WHERE id = $1`, string(vehicleID)).Scan(&pucStr)
		if pucStr.Valid && pucStr.String != "" {
			if t, err := time.Parse("2006-01-02", pucStr.String); err == nil {
				pucTime = t
			} else if t, err := time.Parse(time.RFC3339, pucStr.String); err == nil {
				pucTime = t
			}
		}
	}
	if !pucTime.IsZero() {
		checks = append(checks, docCheck{docType: "puc", name: "vehicle PUC", expiry: pucTime})
	}

	for _, dc := range checks {
		if dc.expiry.IsZero() {
			continue
		}

		if dc.expiry.Before(now) {
			// Check exemption
			exempt, ex, _ := s.IsExempt(ctx, "vehicle", string(vehicleID), dc.docType)
			if exempt && ex != nil {
				note := fmt.Sprintf("%s expired on %s; active exemption until %s (%s)", dc.name, dc.expiry.Format("2006-01-02"), ex.ExemptUntil.Format("2006-01-02"), ex.Reason)
				_ = s.RecordCheck(ctx, "vehicle", string(vehicleID), dc.docType, "warning", note)
				s.logAudit(ctx, nil, "compliance_exemption_bypass", "vehicles", string(vehicleID), nil, &note)
				result.Alerts = append(result.Alerts, fmt.Sprintf("%s expired but bypassed via active exemption until %s", dc.name, ex.ExemptUntil.Format("2006-01-02")))
			} else {
				reason := fmt.Sprintf("%s expired", dc.name)
				_ = s.RecordCheck(ctx, "vehicle", string(vehicleID), dc.docType, "expired", reason)
				s.publishBlockedEvent(ctx, "vehicle", "", string(vehicleID), reason)
				return ComplianceCheckResult{Valid: false, Blocked: true, Reason: reason}, nil
			}
		} else if dc.expiry.Before(now.Add(7 * 24 * time.Hour)) {
			days := int(time.Until(dc.expiry).Hours() / 24)
			if days < 0 {
				days = 0
			}
			msg := fmt.Sprintf("%s expires in %d days (%s)", dc.name, days, dc.expiry.Format("2006-01-02"))
			_ = s.RecordCheck(ctx, "vehicle", string(vehicleID), dc.docType, "warning", msg)
			result.Alerts = append(result.Alerts, msg)
		} else {
			_ = s.RecordCheck(ctx, "vehicle", string(vehicleID), dc.docType, "valid", dc.name+" valid")
		}
	}

	return result, nil
}

// EnforceDispatchCompliance verifies driver and vehicle compliance before assignment.
func (s *ComplianceService) EnforceDispatchCompliance(ctx context.Context, driverID *domain.DriverID, vehicleID *domain.VehicleID) error {
	dID := ""
	if driverID != nil {
		dID = string(*driverID)
	}
	vID := ""
	if vehicleID != nil {
		vID = string(*vehicleID)
	}
	_, err := s.CheckDispatchCompliance(ctx, dID, vID)
	return err
}

// RecordCheck writes an audit entry to the compliance_checks table (Spec 05 §5).
func (s *ComplianceService) RecordCheck(ctx context.Context, entityType, entityID, checkType, status, details string) error {
	dbGetter, ok := s.store.(repository.DBGetter)
	if !ok || dbGetter.DB() == nil {
		return nil
	}

	id := uuid.NewString()
	_, err := dbGetter.DB().ExecContext(ctx, `
		INSERT INTO compliance_checks (id, entity_type, entity_id, check_type, status, details, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, CURRENT_TIMESTAMP)`,
		id, entityType, entityID, checkType, status, details)
	return err
}

// CreateExemption grants a temporary exemption for a compliance document (Spec 05 §5).
func (s *ComplianceService) CreateExemption(ctx context.Context, ex ComplianceExemption) error {
	dbGetter, ok := s.store.(repository.DBGetter)
	if !ok || dbGetter.DB() == nil {
		return errors.New("database unavailable")
	}

	if ex.ID == "" {
		ex.ID = uuid.NewString()
	}
	if ex.CreatedAt.IsZero() {
		ex.CreatedAt = time.Now().UTC()
	}

	_, err := dbGetter.DB().ExecContext(ctx, `
		INSERT INTO compliance_exemptions (id, entity_type, entity_id, doc_type, reason, exempt_until, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		ex.ID, ex.EntityType, ex.EntityID, ex.DocType, ex.Reason, ex.ExemptUntil, ex.CreatedBy, ex.CreatedAt)
	if err != nil {
		return err
	}

	reasonStr := fmt.Sprintf("Exemption granted for %s %s (%s) until %s: %s", ex.EntityType, ex.EntityID, ex.DocType, ex.ExemptUntil.Format("2006-01-02"), ex.Reason)
	s.logAudit(ctx, nil, "create_compliance_exemption", ex.EntityType+"s", ex.EntityID, nil, &reasonStr)
	return nil
}

// IsExempt checks if an active exemption exists for the given entity and document type.
func (s *ComplianceService) IsExempt(ctx context.Context, entityType, entityID, docType string) (bool, *ComplianceExemption, error) {
	dbGetter, ok := s.store.(repository.DBGetter)
	if !ok || dbGetter.DB() == nil {
		return false, nil, nil
	}

	now := time.Now().UTC()
	row := dbGetter.DB().QueryRowContext(ctx, `
		SELECT id, entity_type, entity_id, doc_type, reason, exempt_until, created_by, created_at
		FROM compliance_exemptions
		WHERE entity_type = $1 AND entity_id = $2 AND (doc_type = $3 OR doc_type = 'all')
		  AND exempt_until > $4
		ORDER BY exempt_until DESC
		LIMIT 1`, entityType, entityID, docType, now)

	var ex ComplianceExemption
	err := row.Scan(&ex.ID, &ex.EntityType, &ex.EntityID, &ex.DocType, &ex.Reason, &ex.ExemptUntil, &ex.CreatedBy, &ex.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil, nil
		}
		return false, nil, err
	}

	return true, &ex, nil
}

// ListExemptions returns all exemptions for a specific entity.
func (s *ComplianceService) ListExemptions(ctx context.Context, entityType, entityID string) ([]ComplianceExemption, error) {
	dbGetter, ok := s.store.(repository.DBGetter)
	if !ok || dbGetter.DB() == nil {
		return nil, nil
	}

	rows, err := dbGetter.DB().QueryContext(ctx, `
		SELECT id, entity_type, entity_id, doc_type, reason, exempt_until, created_by, created_at
		FROM compliance_exemptions
		WHERE entity_type = $1 AND entity_id = $2
		ORDER BY created_at DESC`, entityType, entityID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []ComplianceExemption
	for rows.Next() {
		var ex ComplianceExemption
		if err := rows.Scan(&ex.ID, &ex.EntityType, &ex.EntityID, &ex.DocType, &ex.Reason, &ex.ExemptUntil, &ex.CreatedBy, &ex.CreatedAt); err == nil {
			results = append(results, ex)
		}
	}
	return results, nil
}

// publishBlockedEvent emits a ComplianceBlocked event to notify the alert pipeline (Spec 05 §1, §5).
func (s *ComplianceService) publishBlockedEvent(ctx context.Context, entityType, driverID, vehicleID, reason string) {
	if s.events == nil {
		return
	}

	entityID := driverID
	if entityType == "vehicle" {
		entityID = vehicleID
	}

	s.events.Publish(ctx, events.Event{
		Type: "ComplianceBlocked",
		Payload: map[string]interface{}{
			"source":      "compliance",
			"alert_type":  "compliance_blocked",
			"severity":    "blocker",
			"entity_type": entityType,
			"entity_id":   entityID,
			"driver_id":   driverID,
			"vehicle_id":  vehicleID,
			"title":       fmt.Sprintf("Dispatch Compliance Block: %s", reason),
			"details":     fmt.Sprintf("Dispatch blocked: %s (compliance)", reason),
		},
	})

	if s.opsAlerts != nil {
		// Fail closed: never file another org's block under tenant 1.
		if tenantID := string(shared.TenantIDFromContext(ctx)); tenantID != "" {
			_, _ = s.opsAlerts.CreateAlert(ctx, OpsAlert{
				TenantID:    tenantID,
				AlertType:   OpsAlertComplianceBreach,
				Severity:    OpsAlertSeverityCritical,
				Title:       "Dispatch blocked by compliance",
				Description: fmt.Sprintf("%s %s blocked: %s", entityType, entityID, reason),
				EntityType:  strPtr(entityType),
				EntityID:    &entityID,
			})
		}
	}
}
