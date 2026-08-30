package sql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"transport-app/internal/driver/domain"
	"transport-app/internal/repository"
)

type queryExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type DriverLifecycleRepository struct {
	db *sql.DB
}

func NewDriverLifecycleRepository(db *sql.DB) *DriverLifecycleRepository {
	return &DriverLifecycleRepository{db: db}
}

func (r *DriverLifecycleRepository) exec(ctx context.Context) queryExecutor {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx
	}
	return r.db
}

// ─── DRIVER LICENSE REPOSITORY ──────────────────────────────────────────────

func (r *DriverLifecycleRepository) SaveLicense(ctx context.Context, tenantID, driverID string, lic domain.DriverLicenseRecord, classes []string) error {
	ex := r.exec(ctx)

	// Supersede existing current licenses for this driver
	_, err := ex.ExecContext(ctx, `
		UPDATE driver_licenses
		SET is_current = 0, superseded_at = CURRENT_TIMESTAMP
		WHERE tenant_id = ? AND driver_id = ? AND is_current = 1`,
		tenantID, driverID)
	if err != nil {
		return err
	}

	_, err = ex.ExecContext(ctx, `
		INSERT INTO driver_licenses (id, tenant_id, driver_id, license_number, issuing_authority, issued_on, expires_on, is_current, verification_status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, CURRENT_TIMESTAMP)`,
		lic.ID, tenantID, driverID, lic.LicenseNumber, lic.IssuingAuthority, lic.IssuedOn, lic.ExpiresOn, lic.VerificationStatus)
	if err != nil {
		return err
	}

	for _, c := range classes {
		_, err = ex.ExecContext(ctx, `
			INSERT INTO driver_license_classes (id, license_id, tenant_id, class_code, created_at)
			VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(license_id, class_code) DO NOTHING`,
			lic.ID+"-"+c, lic.ID, tenantID, c)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *DriverLifecycleRepository) GetCurrentLicense(ctx context.Context, tenantID, driverID string) (*domain.DriverLicenseRecord, []string, error) {
	ex := r.exec(ctx)
	row := ex.QueryRowContext(ctx, `
		SELECT id, tenant_id, driver_id, license_number, COALESCE(issuing_authority, ''), issued_on, expires_on, is_current, verification_status, verified_at, created_at
		FROM driver_licenses
		WHERE tenant_id = ? AND driver_id = ? AND is_current = 1
		LIMIT 1`, tenantID, driverID)

	var lic domain.DriverLicenseRecord
	var issuedOn sql.NullTime
	var verifiedAt sql.NullTime
	var isCurrent int
	err := row.Scan(&lic.ID, &lic.TenantID, &lic.DriverID, &lic.LicenseNumber, &lic.IssuingAuthority, &issuedOn, &lic.ExpiresOn, &isCurrent, &lic.VerificationStatus, &verifiedAt, &lic.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	lic.IsCurrent = isCurrent == 1
	if issuedOn.Valid {
		lic.IssuedOn = &issuedOn.Time
	}
	if verifiedAt.Valid {
		lic.VerifiedAt = &verifiedAt.Time
	}

	rows, err := ex.QueryContext(ctx, `
		SELECT class_code FROM driver_license_classes
		WHERE tenant_id = ? AND license_id = ?`, tenantID, lic.ID)
	if err != nil {
		return &lic, nil, nil
	}
	defer func() { _ = rows.Close() }()

	var classes []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err == nil {
			classes = append(classes, c)
		}
	}
	return &lic, classes, nil
}

func (r *DriverLifecycleRepository) VerifyLicense(ctx context.Context, tenantID, licenseID string, status string, verifiedBy string) error {
	ex := r.exec(ctx)
	_, err := ex.ExecContext(ctx, `
		UPDATE driver_licenses
		SET verification_status = ?, verified_at = CURRENT_TIMESTAMP
		WHERE tenant_id = ? AND id = ?`, status, tenantID, licenseID)
	return err
}

// ─── DRIVER VEHICLE ASSIGNMENT REPOSITORY ────────────────────────────────────

func (r *DriverLifecycleRepository) CreateAssignment(ctx context.Context, tenantID string, asg domain.DriverVehicleAssignmentRecord) error {
	ex := r.exec(ctx)

	// Enforce single active assignment per driver and vehicle
	var activeCount int
	err := ex.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM driver_vehicle_assignments
		WHERE tenant_id = ? AND (driver_id = ? OR vehicle_id = ?) AND status = 'active'`,
		tenantID, asg.DriverID, asg.VehicleID).Scan(&activeCount)
	if err != nil {
		return err
	}
	if activeCount > 0 {
		return errors.New("conflicting active assignment exists for driver or vehicle")
	}

	_, err = ex.ExecContext(ctx, `
		INSERT INTO driver_vehicle_assignments (id, tenant_id, driver_id, vehicle_id, assignment_type, status, started_at, assigned_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		asg.ID, tenantID, asg.DriverID, asg.VehicleID, asg.AssignmentType, asg.Status, asg.StartedAt, asg.AssignedBy)
	return err
}

func (r *DriverLifecycleRepository) GetActiveAssignmentForDriver(ctx context.Context, tenantID, driverID string) (*domain.DriverVehicleAssignmentRecord, error) {
	ex := r.exec(ctx)
	row := ex.QueryRowContext(ctx, `
		SELECT id, tenant_id, driver_id, vehicle_id, assignment_type, status, started_at, ended_at, assigned_by, accepted_at, created_at, updated_at
		FROM driver_vehicle_assignments
		WHERE tenant_id = ? AND driver_id = ? AND status = 'active'
		LIMIT 1`, tenantID, driverID)

	var asg domain.DriverVehicleAssignmentRecord
	var startedAt, endedAt, acceptedAt sql.NullTime
	var assignedBy sql.NullString
	err := row.Scan(&asg.ID, &asg.TenantID, &asg.DriverID, &asg.VehicleID, &asg.AssignmentType, &asg.Status, &startedAt, &endedAt, &assignedBy, &acceptedAt, &asg.CreatedAt, &asg.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if startedAt.Valid {
		asg.StartedAt = &startedAt.Time
	}
	if endedAt.Valid {
		asg.EndedAt = &endedAt.Time
	}
	if acceptedAt.Valid {
		asg.AcceptedAt = &acceptedAt.Time
	}
	if assignedBy.Valid {
		asg.AssignedBy = &assignedBy.String
	}
	return &asg, nil
}

func (r *DriverLifecycleRepository) GetActiveAssignmentForVehicle(ctx context.Context, tenantID, vehicleID string) (*domain.DriverVehicleAssignmentRecord, error) {
	ex := r.exec(ctx)
	row := ex.QueryRowContext(ctx, `
		SELECT id, tenant_id, driver_id, vehicle_id, assignment_type, status, started_at, ended_at, assigned_by, accepted_at, created_at, updated_at
		FROM driver_vehicle_assignments
		WHERE tenant_id = ? AND vehicle_id = ? AND status = 'active'
		LIMIT 1`, tenantID, vehicleID)

	var asg domain.DriverVehicleAssignmentRecord
	var startedAt, endedAt, acceptedAt sql.NullTime
	var assignedBy sql.NullString
	err := row.Scan(&asg.ID, &asg.TenantID, &asg.DriverID, &asg.VehicleID, &asg.AssignmentType, &asg.Status, &startedAt, &endedAt, &assignedBy, &acceptedAt, &asg.CreatedAt, &asg.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if startedAt.Valid {
		asg.StartedAt = &startedAt.Time
	}
	if endedAt.Valid {
		asg.EndedAt = &endedAt.Time
	}
	if acceptedAt.Valid {
		asg.AcceptedAt = &acceptedAt.Time
	}
	if assignedBy.Valid {
		asg.AssignedBy = &assignedBy.String
	}
	return &asg, nil
}

func (r *DriverLifecycleRepository) EndAssignment(ctx context.Context, tenantID, assignmentID string, endedAt time.Time) error {
	ex := r.exec(ctx)
	_, err := ex.ExecContext(ctx, `
		UPDATE driver_vehicle_assignments
		SET status = 'ended', ended_at = ?, updated_at = CURRENT_TIMESTAMP
		WHERE tenant_id = ? AND id = ?`, endedAt, tenantID, assignmentID)
	return err
}

// ─── VEHICLE COMPLIANCE REPOSITORY ───────────────────────────────────────────

func (r *DriverLifecycleRepository) SaveComplianceDoc(ctx context.Context, tenantID, vehicleID string, doc domain.VehicleComplianceDocRecord) error {
	ex := r.exec(ctx)

	// Supersede existing doc of this type
	_, err := ex.ExecContext(ctx, `
		UPDATE vehicle_compliance_documents
		SET is_current = 0, superseded_at = CURRENT_TIMESTAMP
		WHERE tenant_id = ? AND vehicle_id = ? AND document_type = ? AND is_current = 1`,
		tenantID, vehicleID, doc.DocumentType)
	if err != nil {
		return err
	}

	_, err = ex.ExecContext(ctx, `
		INSERT INTO vehicle_compliance_documents (id, tenant_id, vehicle_id, document_type, document_number, storage_key, issued_on, expires_on, is_current, verification_status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, CURRENT_TIMESTAMP)`,
		doc.ID, tenantID, vehicleID, doc.DocumentType, doc.DocumentNumber, doc.StorageKey, doc.IssuedOn, doc.ExpiresOn, doc.VerificationStatus)
	return err
}

func (r *DriverLifecycleRepository) GetActiveComplianceDocs(ctx context.Context, tenantID, vehicleID string) ([]domain.VehicleComplianceDocRecord, error) {
	ex := r.exec(ctx)
	rows, err := ex.QueryContext(ctx, `
		SELECT id, tenant_id, vehicle_id, document_type, document_number, COALESCE(storage_key, ''), issued_on, expires_on, is_current, verification_status, verified_by, verified_at, rejection_reason, created_at
		FROM vehicle_compliance_documents
		WHERE tenant_id = ? AND vehicle_id = ? AND is_current = 1`,
		tenantID, vehicleID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var docs []domain.VehicleComplianceDocRecord
	for rows.Next() {
		var d domain.VehicleComplianceDocRecord
		var issuedOn, verifiedAt sql.NullTime
		var verifiedBy, rejectionReason sql.NullString
		var isCurrent int
		if err := rows.Scan(&d.ID, &d.TenantID, &d.VehicleID, &d.DocumentType, &d.DocumentNumber, &d.StorageKey, &issuedOn, &d.ExpiresOn, &isCurrent, &d.VerificationStatus, &verifiedBy, &verifiedAt, &rejectionReason, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.IsCurrent = isCurrent == 1
		if issuedOn.Valid {
			d.IssuedOn = &issuedOn.Time
		}
		if verifiedAt.Valid {
			d.VerifiedAt = &verifiedAt.Time
		}
		if verifiedBy.Valid {
			d.VerifiedBy = &verifiedBy.String
		}
		if rejectionReason.Valid {
			d.RejectionReason = &rejectionReason.String
		}
		docs = append(docs, d)
	}
	return docs, nil
}

func (r *DriverLifecycleRepository) VerifyComplianceDoc(ctx context.Context, tenantID, docID string, status string, verifiedBy string, rejectionReason string) error {
	ex := r.exec(ctx)
	_, err := ex.ExecContext(ctx, `
		UPDATE vehicle_compliance_documents
		SET verification_status = ?, verified_by = ?, verified_at = CURRENT_TIMESTAMP, rejection_reason = ?
		WHERE tenant_id = ? AND id = ?`, status, verifiedBy, rejectionReason, tenantID, docID)
	return err
}

// ─── VEHICLE OWNERSHIP REPOSITORY ───────────────────────────────────────────

func (r *DriverLifecycleRepository) CreateClaim(ctx context.Context, tenantID string, claim domain.VehicleClaimRecord) error {
	ex := r.exec(ctx)
	_, err := ex.ExecContext(ctx, `
		INSERT INTO vehicle_claims (id, tenant_id, driver_id, registration_number, rc_document_id, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		claim.ID, tenantID, claim.DriverID, claim.RegistrationNumber, claim.RCDocumentID, claim.Status)
	return err
}

func (r *DriverLifecycleRepository) ReviewClaim(ctx context.Context, tenantID, claimID string, status string, reviewedBy string, rejectionReason string) error {
	ex := r.exec(ctx)
	_, err := ex.ExecContext(ctx, `
		UPDATE vehicle_claims
		SET status = ?, reviewed_by = ?, reviewed_at = CURRENT_TIMESTAMP, rejection_reason = ?, updated_at = CURRENT_TIMESTAMP
		WHERE tenant_id = ? AND id = ?`, status, reviewedBy, rejectionReason, tenantID, claimID)
	return err
}

func (r *DriverLifecycleRepository) GetActiveOwnership(ctx context.Context, tenantID, vehicleID string) (*domain.VehicleOwnershipRecord, error) {
	ex := r.exec(ctx)
	row := ex.QueryRowContext(ctx, `
		SELECT id, tenant_id, vehicle_id, owner_party_type, owner_party_id, valid_from, valid_until, created_at
		FROM vehicle_ownership
		WHERE tenant_id = ? AND vehicle_id = ?
		LIMIT 1`, tenantID, vehicleID)

	var o domain.VehicleOwnershipRecord
	var validUntil sql.NullTime
	err := row.Scan(&o.ID, &o.TenantID, &o.VehicleID, &o.OwnerPartyType, &o.OwnerPartyID, &o.ValidFrom, &validUntil, &o.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if validUntil.Valid {
		o.ValidUntil = &validUntil.Time
	}
	return &o, nil
}

// ─── TELEMETRY SESSION REPOSITORY ───────────────────────────────────────────

func (r *DriverLifecycleRepository) RegisterInstallation(ctx context.Context, tenantID string, inst domain.TelemetryInstallationRecord) error {
	ex := r.exec(ctx)
	_, err := ex.ExecContext(ctx, `
		INSERT INTO telemetry_installations (id, tenant_id, app_installation_id, platform, app_version, device_model, os_version, status, last_seen_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(app_installation_id) DO UPDATE SET
			app_version = excluded.app_version,
			os_version = excluded.os_version,
			last_seen_at = CURRENT_TIMESTAMP,
			status = 'active'`,
		inst.ID, tenantID, inst.AppInstallationID, inst.Platform, inst.AppVersion, inst.DeviceModel, inst.OSVersion)
	return err
}

func (r *DriverLifecycleRepository) StartSession(ctx context.Context, tenantID string, sess domain.TelemetrySessionRecord) error {
	ex := r.exec(ctx)
	_, err := ex.ExecContext(ctx, `
		INSERT INTO telemetry_sessions (id, tenant_id, installation_id, driver_id, vehicle_id, trip_id, session_type, status, start_reason, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
		sess.ID, tenantID, sess.InstallationID, sess.DriverID, sess.VehicleID, sess.TripID, sess.SessionType, sess.StartReason, sess.StartedAt)
	return err
}

func (r *DriverLifecycleRepository) GetActiveSession(ctx context.Context, tenantID, driverID string) (*domain.TelemetrySessionRecord, error) {
	ex := r.exec(ctx)
	row := ex.QueryRowContext(ctx, `
		SELECT id, tenant_id, installation_id, driver_id, vehicle_id, trip_id, session_type, status, start_reason, end_reason, started_at, ended_at, total_distance_km, positions_count
		FROM telemetry_sessions
		WHERE tenant_id = ? AND driver_id = ? AND status = 'active'
		LIMIT 1`, tenantID, driverID)

	var s domain.TelemetrySessionRecord
	var vehicleID, tripID, endReason sql.NullString
	var endedAt sql.NullTime
	err := row.Scan(&s.ID, &s.TenantID, &s.InstallationID, &s.DriverID, &vehicleID, &tripID, &s.SessionType, &s.Status, &s.StartReason, &endReason, &s.StartedAt, &endedAt, &s.TotalDistanceKm, &s.PositionsCount)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if vehicleID.Valid {
		s.VehicleID = &vehicleID.String
	}
	if tripID.Valid {
		s.TripID = &tripID.String
	}
	if endReason.Valid {
		s.EndReason = &endReason.String
	}
	if endedAt.Valid {
		s.EndedAt = &endedAt.Time
	}
	return &s, nil
}

func (r *DriverLifecycleRepository) EndSession(ctx context.Context, tenantID, sessionID string, endReason string, endedAt time.Time) error {
	ex := r.exec(ctx)
	_, err := ex.ExecContext(ctx, `
		UPDATE telemetry_sessions
		SET status = 'closed', end_reason = ?, ended_at = ?
		WHERE tenant_id = ? AND id = ?`, endReason, endedAt, tenantID, sessionID)
	return err
}

func (r *DriverLifecycleRepository) IngestEvent(ctx context.Context, tenantID string, evt domain.TelemetryEventRecord) error {
	ex := r.exec(ctx)
	_, err := ex.ExecContext(ctx, `
		INSERT INTO telemetry_events (id, tenant_id, session_id, client_event_id, occurred_at, received_at, latitude, longitude, speed, accuracy, heading, altitude, raw_payload)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, session_id, client_event_id) DO NOTHING`,
		evt.ID, tenantID, evt.SessionID, evt.ClientEventID, evt.OccurredAt, evt.Latitude, evt.Longitude, evt.Speed, evt.Accuracy, evt.Heading, evt.Altitude, evt.RawPayload)
	return err
}

func (r *DriverLifecycleRepository) UpsertLatestPosition(ctx context.Context, tenantID string, pos domain.VehicleLatestPositionRecord) error {
	ex := r.exec(ctx)
	_, err := ex.ExecContext(ctx, `
		INSERT INTO driver_vehicle_latest_positions (tenant_id, vehicle_id, session_id, driver_id, latitude, longitude, accuracy, speed, heading, occurred_at, received_at, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?)
		ON CONFLICT(tenant_id, vehicle_id) DO UPDATE SET
			session_id = excluded.session_id,
			driver_id = excluded.driver_id,
			latitude = excluded.latitude,
			longitude = excluded.longitude,
			accuracy = excluded.accuracy,
			speed = excluded.speed,
			heading = excluded.heading,
			occurred_at = excluded.occurred_at,
			received_at = CURRENT_TIMESTAMP,
			source = excluded.source`,
		tenantID, pos.VehicleID, pos.SessionID, pos.DriverID, pos.Latitude, pos.Longitude, pos.Accuracy, pos.Speed, pos.Heading, pos.OccurredAt, pos.Source)
	return err
}

func (r *DriverLifecycleRepository) GetLatestPosition(ctx context.Context, tenantID, vehicleID string) (*domain.VehicleLatestPositionRecord, error) {
	ex := r.exec(ctx)
	row := ex.QueryRowContext(ctx, `
		SELECT tenant_id, vehicle_id, session_id, driver_id, latitude, longitude, accuracy, speed, heading, occurred_at, received_at, source
		FROM driver_vehicle_latest_positions
		WHERE tenant_id = ? AND vehicle_id = ?`, tenantID, vehicleID)

	var p domain.VehicleLatestPositionRecord
	var acc, head sql.NullFloat64
	err := row.Scan(&p.TenantID, &p.VehicleID, &p.SessionID, &p.DriverID, &p.Latitude, &p.Longitude, &acc, &p.Speed, &head, &p.OccurredAt, &p.ReceivedAt, &p.Source)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if acc.Valid {
		p.Accuracy = &acc.Float64
	}
	if head.Valid {
		p.Heading = &head.Float64
	}
	return &p, nil
}

// ─── AUDIT REPOSITORY ───────────────────────────────────────────────────────

func (r *DriverLifecycleRepository) RecordAuditEvent(ctx context.Context, tenantID string, evt domain.AuditEventRecord) error {
	ex := r.exec(ctx)
	_, err := ex.ExecContext(ctx, `
		INSERT INTO audit_events (id, tenant_id, actor_user_id, entity_type, entity_id, action, old_state, new_state, reason, request_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		evt.ID, tenantID, evt.ActorUserID, evt.EntityType, evt.EntityID, evt.Action, evt.OldState, evt.NewState, evt.Reason, evt.RequestID)
	return err
}

func (r *DriverLifecycleRepository) RecordVerificationAttempt(ctx context.Context, tenantID string, attempt domain.VerificationAttemptRecord) error {
	ex := r.exec(ctx)
	_, err := ex.ExecContext(ctx, `
		INSERT INTO verification_attempts (id, tenant_id, entity_type, entity_id, provider, provider_reference, status, requested_at, completed_at, failure_code, failure_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		attempt.ID, tenantID, attempt.EntityType, attempt.EntityID, attempt.Provider, attempt.ProviderReference, attempt.Status, attempt.RequestedAt, attempt.CompletedAt, attempt.FailureCode, attempt.FailureReason)
	return err
}

// ─── DRIVER ONBOARDING REPOSITORY ───────────────────────────────────────────

func (r *DriverLifecycleRepository) GetOnboardingState(ctx context.Context, tenantID, driverID string) (*domain.DriverOnboardingRecord, error) {
	ex := r.exec(ctx)
	row := ex.QueryRowContext(ctx, `
		SELECT driver_id, tenant_id, current_step, identity_status, license_status, vehicle_status, bank_status, overall_status, started_at, completed_at
		FROM driver_onboarding
		WHERE tenant_id = ? AND driver_id = ?`, tenantID, driverID)

	var o domain.DriverOnboardingRecord
	var completedAt sql.NullTime
	err := row.Scan(&o.DriverID, &o.TenantID, &o.CurrentStep, &o.IdentityStatus, &o.LicenseStatus, &o.VehicleStatus, &o.BankStatus, &o.OverallStatus, &o.StartedAt, &completedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if completedAt.Valid {
		o.CompletedAt = &completedAt.Time
	}
	return &o, nil
}

func (r *DriverLifecycleRepository) UpdateOnboardingStep(ctx context.Context, tenantID, driverID string, step string, overallStatus string) error {
	ex := r.exec(ctx)
	_, err := ex.ExecContext(ctx, `
		INSERT INTO driver_onboarding (driver_id, tenant_id, current_step, overall_status, started_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(driver_id) DO UPDATE SET
			current_step = excluded.current_step,
			overall_status = excluded.overall_status`,
		driverID, tenantID, step, overallStatus)
	return err
}
