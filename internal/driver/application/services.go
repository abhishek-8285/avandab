package application

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/driver/domain"
	"transport-app/internal/driver/domain/eligibility"
	driversql "transport-app/internal/driver/infrastructure/persistence/sql"
	"transport-app/internal/repository"
)

type queryExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type dbWrapper struct {
	db *sql.DB
}

func (w *dbWrapper) DB() *sql.DB {
	return w.db
}

type DriverAppService struct {
	db        *sql.DB
	txManager repository.TxManager
	repo      *driversql.DriverLifecycleRepository
	engine    *eligibility.EligibilityEngine
}

func NewDriverAppService(db *sql.DB) *DriverAppService {
	return &DriverAppService{
		db:        db,
		txManager: repository.NewTxManager(&dbWrapper{db: db}),
		repo:      driversql.NewDriverLifecycleRepository(db),
		engine:    eligibility.NewEligibilityEngine(),
	}
}

func (s *DriverAppService) exec(ctx context.Context) queryExecutor {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx
	}
	return s.db
}

// ─── 1. ONBOARDING SERVICE ──────────────────────────────────────────────────

type RequirementDTO struct {
	Code    string `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type OnboardingStatusDTO struct {
	DriverID        string           `json:"driver_id"`
	CurrentStep     string           `json:"current_step"`
	IdentityStatus  string           `json:"identity_status"`
	LicenseStatus   string           `json:"license_status"`
	VehicleStatus   string           `json:"vehicle_status"`
	BankStatus      string           `json:"bank_status"`
	OverallStatus   string           `json:"overall_status"`
	CompletedSteps  []string         `json:"completed_steps"`
	Requirements    []RequirementDTO `json:"requirements"`
	CanSubmit       bool             `json:"can_submit"`
	RejectionReason string           `json:"rejection_reason,omitempty"`
	IsEligible      bool             `json:"is_eligible"`
}

func (s *DriverAppService) RegisterDriver(ctx context.Context, tenantID, driverID, name, email, phone string) error {
	return s.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		names := strings.SplitN(name, " ", 2)
		firstName := names[0]
		lastName := ""
		if len(names) > 1 {
			lastName = names[1]
		}

		ex := s.exec(txCtx)
		_, err := ex.ExecContext(txCtx, `
			INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, status, tenant_id)
			VALUES (?, ?, ?, ?, ?, ?, 'DL-PENDING', date('now', '+5 years'), 'available', ?)
			ON CONFLICT(driver_id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP`,
			driverID, driverID, firstName, lastName, phone, email, tenantID)
		if err != nil {
			return err
		}

		err = s.repo.UpdateOnboardingStep(txCtx, tenantID, driverID, "profile", "in_progress")
		if err != nil {
			return err
		}

		return s.repo.RecordAuditEvent(txCtx, tenantID, domain.AuditEventRecord{
			ID:         uuid.NewString(),
			TenantID:   tenantID,
			EntityType: "driver",
			EntityID:   driverID,
			Action:     "registered",
		})
	})
}

func (s *DriverAppService) GetOnboardingState(ctx context.Context, tenantID, driverID string) (*OnboardingStatusDTO, error) {
	state, err := s.repo.GetOnboardingState(ctx, tenantID, driverID)
	if err != nil {
		return nil, err
	}

	var completed []string
	var reqs []RequirementDTO

	if state == nil {
		return &OnboardingStatusDTO{
			DriverID:       driverID,
			CurrentStep:    "profile",
			IdentityStatus: "pending",
			LicenseStatus:  "pending",
			VehicleStatus:  "pending",
			BankStatus:     "pending",
			OverallStatus:  "in_progress",
			CompletedSteps: []string{},
			Requirements: []RequirementDTO{
				{Code: "PROFILE_INCOMPLETE", Status: "pending", Message: "Complete personal profile"},
				{Code: "LICENSE_REQUIRED", Status: "pending", Message: "Submit Driving License"},
				{Code: "VEHICLE_BINDING_REQUIRED", Status: "pending", Message: "Select vehicle ownership or claim vehicle"},
				{Code: "BANK_DETAILS_REQUIRED", Status: "pending", Message: "Submit bank payout account"},
			},
			CanSubmit:  false,
			IsEligible: false,
		}, nil
	}

	// Calculate completed steps based on persisted state
	if state.CurrentStep != "profile" {
		completed = append(completed, "profile")
	}
	if state.LicenseStatus == "verified" || state.LicenseStatus == "pending" {
		completed = append(completed, "kyc_documents")
	}
	if state.VehicleStatus == "approved" || state.VehicleStatus == "pending_claim_review" {
		completed = append(completed, "ownership_choice", "vehicle_binding")
	}
	if state.BankStatus == "verified" || state.BankStatus == "pending" {
		completed = append(completed, "bank_details")
	}

	// Requirements
	if state.LicenseStatus != "verified" && state.LicenseStatus != "pending" {
		reqs = append(reqs, RequirementDTO{Code: "LICENSE_REQUIRED", Status: "pending", Message: "Submit Driving License"})
	}
	if state.VehicleStatus != "approved" && state.VehicleStatus != "pending_claim_review" {
		reqs = append(reqs, RequirementDTO{Code: "VEHICLE_REQUIRED", Status: "pending", Message: "Claim vehicle or request assignment"})
	}
	if state.BankStatus != "verified" && state.BankStatus != "pending" {
		reqs = append(reqs, RequirementDTO{Code: "BANK_REQUIRED", Status: "pending", Message: "Provide payout account"})
	}

	canSubmit := len(reqs) == 0 && state.OverallStatus != "submitted" && state.OverallStatus != "approved"
	el, _ := s.EvaluateDispatchEligibility(ctx, tenantID, driverID)

	return &OnboardingStatusDTO{
		DriverID:       state.DriverID,
		CurrentStep:    state.CurrentStep,
		IdentityStatus: state.IdentityStatus,
		LicenseStatus:  state.LicenseStatus,
		VehicleStatus:  state.VehicleStatus,
		BankStatus:     state.BankStatus,
		OverallStatus:  state.OverallStatus,
		CompletedSteps: completed,
		Requirements:   reqs,
		CanSubmit:      canSubmit,
		IsEligible:     el.IsEligible,
	}, nil
}

func (s *DriverAppService) SubmitLicense(ctx context.Context, tenantID, driverID, licenseNumber, issuingAuth string, issuedOn, expiresOn time.Time, classes []string) error {
	if strings.TrimSpace(licenseNumber) == "" {
		return errors.New("license number cannot be empty")
	}
	if expiresOn.Before(time.Now()) {
		return errors.New("license is already expired")
	}

	return s.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		licID := uuid.NewString()
		lic := domain.DriverLicenseRecord{
			ID:                 licID,
			TenantID:           tenantID,
			DriverID:           driverID,
			LicenseNumber:      strings.ToUpper(strings.TrimSpace(licenseNumber)),
			IssuingAuthority:   issuingAuth,
			IssuedOn:           &issuedOn,
			ExpiresOn:          expiresOn,
			VerificationStatus: "pending",
		}

		if err := s.repo.SaveLicense(txCtx, tenantID, driverID, lic, classes); err != nil {
			return err
		}

		ex := s.exec(txCtx)
		_, err := ex.ExecContext(txCtx, `
			UPDATE driver_onboarding
			SET license_status = 'pending', current_step = 'kyc_documents'
			WHERE tenant_id = ? AND driver_id = ?`, tenantID, driverID)
		if err != nil {
			return err
		}

		return s.repo.RecordAuditEvent(txCtx, tenantID, domain.AuditEventRecord{
			ID:         uuid.NewString(),
			TenantID:   tenantID,
			EntityType: "driver_license",
			EntityID:   licID,
			Action:     "submitted",
		})
	})
}

func (s *DriverAppService) SubmitDocument(ctx context.Context, tenantID, driverID, docType, storageKey, mimeType string, fileSize int64, docHash string) (string, error) {
	docID := uuid.NewString()
	err := s.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		ex := s.exec(txCtx)
		_, err := ex.ExecContext(txCtx, `
			INSERT INTO driver_compliance_documents (id, tenant_id, driver_id, document_type, storage_key, mime_type, file_size_bytes, document_hash, status, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'submitted', CURRENT_TIMESTAMP)`,
			docID, tenantID, driverID, docType, storageKey, mimeType, fileSize, docHash)
		if err != nil {
			return err
		}

		return s.repo.RecordAuditEvent(txCtx, tenantID, domain.AuditEventRecord{
			ID:         uuid.NewString(),
			TenantID:   tenantID,
			EntityType: "driver_document",
			EntityID:   docID,
			Action:     "submitted",
		})
	})
	return docID, err
}

func (s *DriverAppService) ClaimVehicle(ctx context.Context, tenantID, driverID, regNumber string, rcDocID *string) (string, error) {
	reg := strings.ToUpper(strings.TrimSpace(regNumber))
	if reg == "" {
		return "", errors.New("registration number required")
	}

	claimID := uuid.NewString()
	err := s.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		claim := domain.VehicleClaimRecord{
			ID:                 claimID,
			TenantID:           tenantID,
			DriverID:           driverID,
			RegistrationNumber: reg,
			RCDocumentID:       rcDocID,
			Status:             "submitted",
		}
		if err := s.repo.CreateClaim(txCtx, tenantID, claim); err != nil {
			return err
		}

		ex := s.exec(txCtx)
		_, err := ex.ExecContext(txCtx, `
			UPDATE driver_onboarding
			SET vehicle_status = 'pending_claim_review'
			WHERE tenant_id = ? AND driver_id = ?`, tenantID, driverID)
		if err != nil {
			return err
		}

		return s.repo.RecordAuditEvent(txCtx, tenantID, domain.AuditEventRecord{
			ID:         uuid.NewString(),
			TenantID:   tenantID,
			EntityType: "vehicle_claim",
			EntityID:   claimID,
			Action:     "claim_submitted",
		})
	})
	return claimID, err
}

func (s *DriverAppService) SubmitPayoutAccount(ctx context.Context, tenantID, driverID, holder, accNum, ifsc, bankName string) (string, error) {
	if len(accNum) < 4 {
		return "", errors.New("invalid account number")
	}
	masked := "******" + accNum[len(accNum)-4:]
	accID := uuid.NewString()

	err := s.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		ex := s.exec(txCtx)

		// Deactivate previous primary accounts
		_, err := ex.ExecContext(txCtx, `
			UPDATE driver_payout_accounts
			SET is_primary = 0, valid_until = CURRENT_TIMESTAMP
			WHERE tenant_id = ? AND driver_id = ? AND is_primary = 1`,
			tenantID, driverID)
		if err != nil {
			return err
		}

		_, err = ex.ExecContext(txCtx, `
			INSERT INTO driver_payout_accounts (id, tenant_id, driver_id, account_holder_name, account_number_encrypted, account_number_masked, ifsc_code, bank_name, is_primary, verification_status, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 'penny_drop_pending', CURRENT_TIMESTAMP)`,
			accID, tenantID, driverID, holder, accNum, masked, strings.ToUpper(strings.TrimSpace(ifsc)), bankName)
		if err != nil {
			return err
		}

		_, err = ex.ExecContext(txCtx, `
			UPDATE driver_onboarding
			SET bank_status = 'pending'
			WHERE tenant_id = ? AND driver_id = ?`, tenantID, driverID)
		if err != nil {
			return err
		}

		return s.repo.RecordAuditEvent(txCtx, tenantID, domain.AuditEventRecord{
			ID:         uuid.NewString(),
			TenantID:   tenantID,
			EntityType: "driver_payout_account",
			EntityID:   accID,
			Action:     "submitted",
		})
	})
	return accID, err
}

func (s *DriverAppService) SubmitForVerification(ctx context.Context, tenantID, driverID string) error {
	return s.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		lic, _, err := s.repo.GetCurrentLicense(txCtx, tenantID, driverID)
		if err != nil {
			return err
		}
		if lic == nil {
			return errors.New("cannot submit onboarding: driving license missing")
		}

		ex := s.exec(txCtx)
		_, err = ex.ExecContext(txCtx, `
			UPDATE driver_onboarding
			SET overall_status = 'submitted', current_step = 'pending_approval'
			WHERE tenant_id = ? AND driver_id = ?`, tenantID, driverID)
		if err != nil {
			return err
		}

		return s.repo.RecordAuditEvent(txCtx, tenantID, domain.AuditEventRecord{
			ID:         uuid.NewString(),
			TenantID:   tenantID,
			EntityType: "driver_onboarding",
			EntityID:   driverID,
			Action:     "submitted_for_verification",
		})
	})
}

// ─── 2. VERIFICATION SERVICE ────────────────────────────────────────────────

func (s *DriverAppService) ReviewDriverLicense(ctx context.Context, tenantID, licenseID, reviewerID string, approve bool, reason string) error {
	return s.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		status := "verified"
		action := "license_verified"
		if !approve {
			status = "rejected"
			action = "license_rejected"
		}

		if err := s.repo.VerifyLicense(txCtx, tenantID, licenseID, status, reviewerID); err != nil {
			return err
		}

		return s.repo.RecordAuditEvent(txCtx, tenantID, domain.AuditEventRecord{
			ID:          uuid.NewString(),
			TenantID:    tenantID,
			ActorUserID: &reviewerID,
			EntityType:  "driver_license",
			EntityID:    licenseID,
			Action:      action,
			Reason:      &reason,
		})
	})
}

func (s *DriverAppService) ReviewVehicleClaim(ctx context.Context, tenantID, claimID, reviewerID string, approve bool, reason string) error {
	return s.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		status := "approved"
		action := "vehicle_claim_approved"
		if !approve {
			status = "rejected"
			action = "vehicle_claim_rejected"
		}

		if err := s.repo.ReviewClaim(txCtx, tenantID, claimID, status, reviewerID, reason); err != nil {
			return err
		}

		if approve {
			ex := s.exec(txCtx)
			var driverID, regNum string
			err := ex.QueryRowContext(txCtx, `SELECT driver_id, registration_number FROM vehicle_claims WHERE tenant_id = ? AND id = ?`, tenantID, claimID).Scan(&driverID, &regNum)
			if err != nil {
				return err
			}

			var vehicleID string
			errV := ex.QueryRowContext(txCtx, `SELECT id FROM vehicles WHERE tenant_id = ? AND registration_number = ?`, tenantID, regNum).Scan(&vehicleID)
			if errV != nil {
				vehicleID = uuid.NewString()
				_, err = ex.ExecContext(txCtx, `
					INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id)
					VALUES (?, ?, ?, 'truck', 5000, 'diesel', date('now', '+1 year'), date('now', '+1 year'), date('now', '+1 year'), 'available', ?)`,
					vehicleID, regNum, regNum, tenantID)
				if err != nil {
					return err
				}
			}

			// Establish ownership
			_, err = ex.ExecContext(txCtx, `
				INSERT INTO vehicle_ownership (id, tenant_id, vehicle_id, owner_party_type, owner_party_id, valid_from, created_at)
				VALUES (?, ?, ?, 'driver', ?, CURRENT_DATE, CURRENT_TIMESTAMP)`,
				uuid.NewString(), tenantID, vehicleID, driverID)
			if err != nil {
				return err
			}

			// Assign vehicle to driver
			asgID := uuid.NewString()
			now := time.Now()
			asg := domain.DriverVehicleAssignmentRecord{
				ID:             asgID,
				TenantID:       tenantID,
				DriverID:       driverID,
				VehicleID:      vehicleID,
				AssignmentType: "owner_operator_claim",
				Status:         "active",
				StartedAt:      &now,
				AssignedBy:     &reviewerID,
			}
			if err := s.repo.CreateAssignment(txCtx, tenantID, asg); err != nil {
				return err
			}
		}

		return s.repo.RecordAuditEvent(txCtx, tenantID, domain.AuditEventRecord{
			ID:          uuid.NewString(),
			TenantID:    tenantID,
			ActorUserID: &reviewerID,
			EntityType:  "vehicle_claim",
			EntityID:    claimID,
			Action:      action,
			Reason:      &reason,
		})
	})
}

// ─── 3. VEHICLE ASSIGNMENT SERVICE ──────────────────────────────────────────

func (s *DriverAppService) AssignVehicleToDriver(ctx context.Context, tenantID, driverID, vehicleID, assignerID, asgType string) (string, error) {
	asgID := uuid.NewString()
	now := time.Now()

	err := s.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		// End any active assignment for the driver first (reassignment)
		prevAsg, err := s.repo.GetActiveAssignmentForDriver(txCtx, tenantID, driverID)
		if err != nil {
			return err
		}
		if prevAsg != nil {
			if err := s.repo.EndAssignment(txCtx, tenantID, prevAsg.ID, now); err != nil {
				return err
			}
		}

		asg := domain.DriverVehicleAssignmentRecord{
			ID:             asgID,
			TenantID:       tenantID,
			DriverID:       driverID,
			VehicleID:      vehicleID,
			AssignmentType: asgType,
			Status:         "active",
			StartedAt:      &now,
			AssignedBy:     &assignerID,
		}
		if err := s.repo.CreateAssignment(txCtx, tenantID, asg); err != nil {
			return err
		}

		return s.repo.RecordAuditEvent(txCtx, tenantID, domain.AuditEventRecord{
			ID:          uuid.NewString(),
			TenantID:    tenantID,
			ActorUserID: &assignerID,
			EntityType:  "driver_vehicle_assignment",
			EntityID:    asgID,
			Action:      "assigned",
		})
	})
	return asgID, err
}

func (s *DriverAppService) EndDriverAssignment(ctx context.Context, tenantID, driverID, assignmentID string) error {
	return s.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.EndAssignment(txCtx, tenantID, assignmentID, time.Now()); err != nil {
			return err
		}

		return s.repo.RecordAuditEvent(txCtx, tenantID, domain.AuditEventRecord{
			ID:         uuid.NewString(),
			TenantID:   tenantID,
			EntityType: "driver_vehicle_assignment",
			EntityID:   assignmentID,
			Action:     "ended",
		})
	})
}

// ─── 4. ELIGIBILITY EVALUATION SERVICE ──────────────────────────────────────

func (s *DriverAppService) EvaluateDispatchEligibility(ctx context.Context, tenantID, driverID string) (eligibility.DispatchEligibilityResult, error) {
	ex := s.exec(ctx)

	var dStatus string
	var dEmail, dPhone sql.NullString
	err := ex.QueryRowContext(ctx, `SELECT status, email, phone FROM drivers WHERE tenant_id = ? AND id = ?`, tenantID, driverID).Scan(&dStatus, &dEmail, &dPhone)
	if err != nil {
		return eligibility.DispatchEligibilityResult{IsEligible: false}, err
	}

	licRec, classes, err := s.repo.GetCurrentLicense(ctx, tenantID, driverID)
	if err != nil {
		return eligibility.DispatchEligibilityResult{IsEligible: false}, err
	}

	var licData *eligibility.LicenseData
	if licRec != nil {
		licData = &eligibility.LicenseData{
			Number:             licRec.LicenseNumber,
			ExpiresOn:          licRec.ExpiresOn,
			VerificationStatus: licRec.VerificationStatus,
			Classes:            classes,
		}
	}

	asgRec, err := s.repo.GetActiveAssignmentForDriver(ctx, tenantID, driverID)
	if err != nil {
		return eligibility.DispatchEligibilityResult{IsEligible: false}, err
	}

	var asgData *eligibility.AssignmentData
	var vehData *eligibility.VehicleData
	var compDocs []eligibility.ComplianceDocData

	if asgRec != nil {
		asgData = &eligibility.AssignmentData{
			ID:        asgRec.ID,
			VehicleID: asgRec.VehicleID,
			Status:    asgRec.Status,
		}

		var vType, vPlate, vStatus string
		errV := ex.QueryRowContext(ctx, `SELECT vehicle_type, registration_number, status FROM vehicles WHERE tenant_id = ? AND id = ?`, tenantID, asgRec.VehicleID).Scan(&vType, &vPlate, &vStatus)
		if errV == nil {
			vehData = &eligibility.VehicleData{
				ID:        asgRec.VehicleID,
				Plate:     vPlate,
				Type:      vType,
				IsBlocked: vStatus == "blocked" || vStatus == "maintenance",
			}
		}

		cDocs, errC := s.repo.GetActiveComplianceDocs(ctx, tenantID, asgRec.VehicleID)
		if errC == nil {
			for _, cd := range cDocs {
				compDocs = append(compDocs, eligibility.ComplianceDocData{
					Type:               cd.DocumentType,
					ExpiresOn:          cd.ExpiresOn,
					VerificationStatus: cd.VerificationStatus,
				})
			}
		}
	}

	evalCtx := eligibility.EligibilityContext{
		EvaluationTime: time.Now(),
		TenantPolicy: eligibility.TenantPolicy{
			RequireIdentityForDispatch:   false,
			RequireVerifiedRCForDispatch: false,
			AllowWarningForExpiredPUC:    true,
		},
		Driver: eligibility.DriverData{
			ID:               driverID,
			Status:           dStatus,
			IsSuspended:      dStatus == "inactive" || dStatus == "suspended",
			IdentityVerified: true,
		},
		License:        licData,
		Assignment:     asgData,
		Vehicle:        vehData,
		ComplianceDocs: compDocs,
	}

	return s.engine.EvaluateDispatch(evalCtx), nil
}

// ─── 8. PUSH TOKEN REGISTRATION ─────────────────────────────────────────────

func (s *DriverAppService) RegisterPushToken(ctx context.Context, tenantID, driverID, userID, deviceID, pushToken, platform string) error {
	if tenantID == "" || driverID == "" || deviceID == "" || pushToken == "" {
		return errors.New("missing required fields for push token registration")
	}
	if platform == "" {
		platform = "android"
	}
	id := uuid.New().String()
	now := time.Now().UTC()

	query := `
		INSERT INTO driver_push_tokens (id, tenant_id, driver_id, user_id, device_id, push_token, platform, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT (tenant_id, driver_id, device_id) DO UPDATE SET
			push_token = excluded.push_token,
			platform = excluded.platform,
			is_active = 1,
			updated_at = excluded.updated_at
	`
	_, err := s.exec(ctx).ExecContext(ctx, query, id, tenantID, driverID, userID, deviceID, pushToken, platform, now, now)
	return err
}
