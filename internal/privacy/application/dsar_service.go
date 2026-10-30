package application

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"transport-app/internal/privacy"
	"transport-app/internal/repository"
	"transport-app/internal/shared/ports"
)

// DPDP Data Principal Rights (Digital Personal Data Protection Act, 2023 §11–15):
// - §11: Right to access information about personal data (DSAR Export)
// - §12: Right to correction and erasure of personal data (Erasure / Right to be Forgotten)
// - §13: Right of grievance redressal
// - §14: Right to nominate
// - §8(7): Retention schedules & erasure upon purpose fulfilment, with statutory exemptions
//   (e.g., Section 36 CGST Act & Income Tax Act 8-year retention for tax invoices & fiscal ledgers).

// RetentionPolicy defines the retention and disposal policy for a category of data.
type RetentionPolicy struct {
	Category        string `json:"category"`
	RetentionPeriod string `json:"retention_period"`
	StatutoryBasis  string `json:"statutory_basis"`
	DisposalMethod  string `json:"disposal_method"`
	Description     string `json:"description"`
}

// SubProcessor describes a third-party data processor engaged under DPDP Act §8(1).
type SubProcessor struct {
	Name       string `json:"name"`
	Purpose    string `json:"purpose"`
	DataShared string `json:"data_shared"`
	Location   string `json:"location"`
	Compliance string `json:"compliance"`
}

// DSARRequestRecord is one entry in the privacy_dsar_requests table (00179).
type DSARRequestRecord struct {
	ID          string       `json:"id"`
	TenantID    string       `json:"tenant_id"`
	UserID      string       `json:"user_id"`
	RequestType string       `json:"request_type"`
	Status      string       `json:"status"`
	RequestedBy string       `json:"requested_by"`
	Details     string       `json:"details"`
	CreatedAt   time.Time    `json:"created_at"`
	CompletedAt sql.NullTime `json:"completed_at,omitempty"`
}

// DSARUser contains exported personal user identity data.
type DSARUser struct {
	ID              string       `json:"id"`
	TenantID        string       `json:"tenant_id"`
	Name            string       `json:"name"`
	Email           string       `json:"email"`
	Phone           string       `json:"phone,omitempty"`
	RoleID          int          `json:"role_id"`
	Status          string       `json:"status"`
	Timezone        string       `json:"timezone"`
	ThemePreference string       `json:"theme_preference"`
	AuthProvider    string       `json:"auth_provider"`
	LastLoginAt     sql.NullTime `json:"last_login_at,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

// DSARConsent represents a platform consent entry.
type DSARConsent struct {
	ID            string       `json:"id"`
	TenantID      string       `json:"tenant_id"`
	UserID        string       `json:"user_id"`
	Purpose       string       `json:"purpose"`
	NoticeVersion string       `json:"notice_version"`
	GrantedAt     time.Time    `json:"granted_at"`
	WithdrawnAt   sql.NullTime `json:"withdrawn_at,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
}

// DSARDriver contains driver profile data (masked for sensitive identifiers).
type DSARDriver struct {
	ID                    string       `json:"id"`
	DriverID              string       `json:"driver_id"`
	FirstName             string       `json:"first_name"`
	LastName              string       `json:"last_name"`
	Phone                 string       `json:"phone"`
	Email                 string       `json:"email,omitempty"`
	Address               string       `json:"address,omitempty"`
	LicenseNumber         string       `json:"license_number,omitempty"`
	LicenseExpiry         sql.NullTime `json:"license_expiry,omitempty"`
	ExperienceYears       int          `json:"experience_years"`
	Status                string       `json:"status"`
	EmergencyContactName  string       `json:"emergency_contact_name,omitempty"`
	EmergencyContactPhone string       `json:"emergency_contact_phone,omitempty"`
	AadhaarMasked         string       `json:"aadhaar_masked,omitempty"`
	PANMasked             string       `json:"pan_masked,omitempty"`
	BankDetailsMasked     string       `json:"bank_details_masked,omitempty"`
}

// DSARAssignment represents a driver-vehicle assignment.
type DSARAssignment struct {
	DriverID     string       `json:"driver_id"`
	VehicleID    string       `json:"vehicle_id"`
	IsPrimary    bool         `json:"is_primary"`
	AssignedAt   time.Time    `json:"assigned_at"`
	UnassignedAt sql.NullTime `json:"unassigned_at,omitempty"`
}

// DSARExpense represents an operational expense claim filed by the user/driver.
type DSARExpense struct {
	ID                string    `json:"id"`
	TripID            string    `json:"trip_id,omitempty"`
	DriverID          string    `json:"driver_id"`
	Category          string    `json:"category"`
	Amount            float64   `json:"amount"`
	Description       string    `json:"description,omitempty"`
	VerificationState string    `json:"verification_state"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
}

// DSARAdvance represents an advance request.
type DSARAdvance struct {
	ID          string       `json:"id"`
	TripID      string       `json:"trip_id,omitempty"`
	DriverID    string       `json:"driver_id"`
	Amount      float64      `json:"amount"`
	Reason      string       `json:"reason,omitempty"`
	Status      string       `json:"status"`
	RequestedAt time.Time    `json:"requested_at"`
	DecidedAt   sql.NullTime `json:"decided_at,omitempty"`
}

// DSARAuditRecord represents an action or event associated with the user.
type DSARAuditRecord struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	TableName string    `json:"table_name"`
	RecordID  string    `json:"record_id"`
	CreatedAt time.Time `json:"created_at"`
}

// DSARBundle is the complete export payload returned to the Data Principal under DPDP §11.
type DSARBundle struct {
	TenantID          string              `json:"tenant_id"`
	ExportedAt        time.Time           `json:"exported_at"`
	RequestedBy       string              `json:"requested_by"`
	User              DSARUser            `json:"user"`
	Consents          []DSARConsent       `json:"consents"`
	Driver            *DSARDriver         `json:"driver,omitempty"`
	Assignments       []DSARAssignment    `json:"driver_assignments,omitempty"`
	Expenses          []DSARExpense       `json:"driver_expenses,omitempty"`
	Advances          []DSARAdvance       `json:"driver_advances,omitempty"`
	AuditHistory      []DSARAuditRecord   `json:"audit_records,omitempty"`
	PriorRequests     []DSARRequestRecord `json:"prior_dsar_requests,omitempty"`
	RetentionSchedule []RetentionPolicy   `json:"retention_schedule"`
	SubProcessors     []SubProcessor      `json:"sub_processors"`
}

// ErasureResult contains confirmation of erasure under DPDP §12.
type ErasureResult struct {
	Success               bool      `json:"success"`
	UserID                string    `json:"user_id"`
	RequestID             string    `json:"request_id"`
	CompletedAt           time.Time `json:"completed_at"`
	ScrubbedFields        []string  `json:"scrubbed_fields"`
	RetainedRecordsNotice string    `json:"retained_records_notice"`
}

// DSARService implements DPDP Act Section 11–15 data principal rights.
type DSARService struct {
	db    *sql.DB
	idGen ports.IDGenerator
}

// NewDSARService wires the DSAR engine.
func NewDSARService(db *sql.DB, idGen ports.IDGenerator) *DSARService {
	return &DSARService{db: db, idGen: idGen}
}

// DefaultRetentionSchedule returns statutory retention policies.
func (s *DSARService) DefaultRetentionSchedule() []RetentionPolicy {
	return []RetentionPolicy{
		{
			Category:        "Telemetry Positions & Raw Frames",
			RetentionPeriod: "30 Days",
			StatutoryBasis:  "Motor Vehicles Act, 1988 & AIS-140 standard",
			DisposalMethod:  "Chunked automated indexed purge (5,000-row batches)",
			Description:     "High-frequency GPS positions, speed, ignition, and sensor telemetry.",
		},
		{
			Category:        "User Authentication Sessions",
			RetentionPeriod: "7 Days after expiry",
			StatutoryBasis:  "Information Technology Act, 2000",
			DisposalMethod:  "Scheduled session sweeper",
			Description:     "Session tokens, IP address, and user-agent metadata.",
		},
		{
			Category:        "Tax Invoices, E-Way Bills & Financial Ledgers",
			RetentionPeriod: "8 Years",
			StatutoryBasis:  "Section 36 Central Goods and Services Tax (CGST) Act, 2017 & Section 44AB Income Tax Act, 1961",
			DisposalMethod:  "Statutory immutable archive",
			Description:     "B2B transport invoices, GST tax breakdowns, E-way bill records, and payment settlements.",
		},
		{
			Category:        "Driver KYC & Licences",
			RetentionPeriod: "Employment Duration + 3 Years",
			StatutoryBasis:  "Motor Vehicles Act, 1988 §134 & DPDP Act, 2023 §8(7)",
			DisposalMethod:  "Anonymized via DSAR erasure or post-tenure purge",
			Description:     "Driver licenses, Aadhaar/PAN metadata, emergency contacts, and DL validity records.",
		},
		{
			Category:        "Video Telematics & AI Dashcam Incidents",
			RetentionPeriod: "90 Days",
			StatutoryBasis:  "Fleet Safety & Insurance Adjudication Policy",
			DisposalMethod:  "Automated storage lifecycle expiration",
			Description:     "DMS/ADAS edge safety events, coaching debrief notes, and incident video clips.",
		},
		{
			Category:        "Data Breach Incidents & Audit Logs",
			RetentionPeriod: "3 Years",
			StatutoryBasis:  "Digital Personal Data Protection Act, 2023 §8(6) & CERT-In Cyber Directions",
			DisposalMethod:  "Append-only compliance ledger",
			Description:     "72-hour breach notices, access certification logs, and administrative audit trails.",
		},
		{
			Category:        "Consents & DSAR Requests",
			RetentionPeriod: "5 Years",
			StatutoryBasis:  "DPDP Act, 2023 §6 & §11–15",
			DisposalMethod:  "Append-only immutable privacy ledger",
			Description:     "Consent grant/withdrawal timestamps, notice versions, and DSAR export/erasure proofs.",
		},
	}
}

// DefaultSubProcessors returns vetted sub-processors per DPDP §8(1).
func (s *DSARService) DefaultSubProcessors() []SubProcessor {
	return []SubProcessor{
		{
			Name:       "Cloudflare, Inc.",
			Purpose:    "Global Edge Network, DDoS Protection, Turnstile Bot Detection, R2 Off-host Backups",
			DataShared: "Edge request metadata, client IP address, encrypted database backups",
			Location:   "India (Mumbai/Chennai/Delhi PoPs) & United States",
			Compliance: "SOC 2 Type II, ISO 27001, DPDP & GDPR DPA",
		},
		{
			Name:       "Razorpay Software Pvt. Ltd.",
			Purpose:    "Payment Gateway, Customer Invoicing, Vendor Payouts & FASTag UPI Collect",
			DataShared: "Customer name, email, phone, invoice amount, bank account / VPA (tokenized)",
			Location:   "India",
			Compliance: "RBI Licensed Payment Aggregator, PCI-DSS Level 1, ISO 27001",
		},
		{
			Name:       "Walkover Technologies Pvt. Ltd. (MSG91)",
			Purpose:    "SMS OTP Delivery & Critical Fleet Alerts",
			DataShared: "Mobile phone number, SMS OTP message content",
			Location:   "India",
			Compliance: "TRAI DLT Registered, ISO 27001",
		},
		{
			Name:       "Twilio Inc.",
			Purpose:    "Global SMS & Messaging Gateway",
			DataShared: "Mobile phone number, dispatch notification SMS",
			Location:   "United States & European Union",
			Compliance: "ISO 27001, SOC 2, HIPAA, DPDP Standard Contractual Clauses",
		},
		{
			Name:       "Fast2SMS (Fast2SMS Telecommunications)",
			Purpose:    "Indian Mobile SMS Dispatch",
			DataShared: "Indian 10-digit mobile number, transactional notification message",
			Location:   "India",
			Compliance: "TRAI DLT Compliant, Indian Telecom Regulatory Authority",
		},
		{
			Name:       "Resend Inc. / Brevo SAS",
			Purpose:    "Transactional Email Delivery (Invoices, Consents, Alerts)",
			DataShared: "Recipient name, email address, notification HTML/PDF attachments",
			Location:   "United States & European Union",
			Compliance: "GDPR Compliant, SOC 2 Type II",
		},
		{
			Name:       "OpenStreetMap / Project OSRM",
			Purpose:    "Map Tiles, Geocoding, & Route Distance Calculation",
			DataShared: "Origin/Destination GPS coordinates (no personal identity)",
			Location:   "European Union",
			Compliance: "Open Database License (ODbL), No PII Stored",
		},
		{
			Name:       "National Informatics Centre (NIC) / GSTN",
			Purpose:    "E-Way Bill Generation & IRP E-Invoicing Verification",
			DataShared: "Consignor/Consignee GSTIN, vehicle registration number, invoice amount, HSN code",
			Location:   "India (Govt. Cloud)",
			Compliance: "Government of India Statutory Regulator",
		},
	}
}

// ExportDataPrincipal exports all personal data associated with targetUserID in tenantID.
func (s *DSARService) ExportDataPrincipal(ctx context.Context, tenantID, targetUserID, requestedBy string) (*DSARBundle, error) {
	if tenantID == "" || targetUserID == "" {
		return nil, errors.New("tenant_id and user_id are required")
	}

	// 1. Fetch User Profile
	var u DSARUser
	var phone sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, email, phone, role_id, status,
		       timezone, theme_preference, auth_provider, last_login_at, created_at, updated_at
		FROM users
		WHERE tenant_id = $1 AND id = $2`, tenantID, targetUserID).Scan(
		&u.ID, &u.TenantID, &u.Name, &u.Email, &phone, &u.RoleID, &u.Status,
		&u.Timezone, &u.ThemePreference, &u.AuthProvider, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if phone.Valid {
		u.Phone = phone.String
	}

	// 2. Fetch Consents
	cRows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, user_id, purpose, notice_version, granted_at, withdrawn_at, created_at
		FROM user_consents
		WHERE tenant_id = $1 AND user_id = $2
		ORDER BY created_at DESC`, tenantID, targetUserID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cRows.Close() }()

	var consents []DSARConsent
	for cRows.Next() {
		var c DSARConsent
		if err := cRows.Scan(&c.ID, &c.TenantID, &c.UserID, &c.Purpose, &c.NoticeVersion, &c.GrantedAt, &c.WithdrawnAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		consents = append(consents, c)
	}

	// 3. Fetch Driver Profile if linked
	var d *DSARDriver
	var dRow struct {
		id, driverID, firstName, lastName, phone, email, address, licenseNumber string
		licenseExpiry                                                           sql.NullTime
		experienceYears                                                         int
		status, emName, emPhone, aadhaar, pan, bank                             string
	}
	driverErr := s.db.QueryRowContext(ctx, `
		SELECT id, driver_id, first_name, last_name, phone,
		       COALESCE(email, ''), COALESCE(address, ''), COALESCE(license_number, ''),
		       license_expiry, experience_years, status,
		       COALESCE(emergency_contact_name, ''), COALESCE(emergency_contact_phone, ''),
		       COALESCE(aadhaar, ''), COALESCE(pan, ''), COALESCE(bank_details, '')
		FROM drivers
		WHERE tenant_id = $1 AND (id = $2 OR driver_id = $2 OR (email != '' AND email = $3) OR (phone != '' AND phone = $4))
		LIMIT 1`, tenantID, targetUserID, u.Email, u.Phone).Scan(
		&dRow.id, &dRow.driverID, &dRow.firstName, &dRow.lastName, &dRow.phone,
		&dRow.email, &dRow.address, &dRow.licenseNumber, &dRow.licenseExpiry,
		&dRow.experienceYears, &dRow.status, &dRow.emName, &dRow.emPhone,
		&dRow.aadhaar, &dRow.pan, &dRow.bank,
	)
	if driverErr == nil {
		d = &DSARDriver{
			ID:                    dRow.id,
			DriverID:              dRow.driverID,
			FirstName:             dRow.firstName,
			LastName:              dRow.lastName,
			Phone:                 dRow.phone,
			Email:                 dRow.email,
			Address:               dRow.address,
			LicenseNumber:         dRow.licenseNumber,
			LicenseExpiry:         dRow.licenseExpiry,
			ExperienceYears:       dRow.experienceYears,
			Status:                dRow.status,
			EmergencyContactName:  dRow.emName,
			EmergencyContactPhone: dRow.emPhone,
			AadhaarMasked:         privacy.MaskAadhaar(dRow.aadhaar),
			PANMasked:             privacy.MaskPAN(dRow.pan),
			BankDetailsMasked:     privacy.MaskBankAccount(dRow.bank),
		}
	}

	// 4. Fetch Driver Vehicle Assignments if driver exists
	var assignments []DSARAssignment
	if d != nil {
		aRows, err := s.db.QueryContext(ctx, `
			SELECT driver_id, vehicle_id, is_primary, assigned_at, unassigned_at
			FROM driver_preferred_vehicles
			WHERE tenant_id = $1 AND driver_id = $2
			ORDER BY assigned_at DESC`, tenantID, d.ID)
		if err == nil {
			defer func() { _ = aRows.Close() }()
			for aRows.Next() {
				var a DSARAssignment
				var isPrim int
				if err := aRows.Scan(&a.DriverID, &a.VehicleID, &isPrim, &a.AssignedAt, &a.UnassignedAt); err == nil {
					a.IsPrimary = (isPrim == 1)
					assignments = append(assignments, a)
				}
			}
		}
	}

	// 5. Fetch Driver Expenses if driver exists
	var expenses []DSARExpense
	if d != nil {
		eRows, err := s.db.QueryContext(ctx, `
			SELECT id, COALESCE(trip_id, ''), driver_id, category, amount, COALESCE(description, ''), verification_state, status, created_at
			FROM driver_expenses
			WHERE tenant_id = $1 AND driver_id = $2
			ORDER BY created_at DESC LIMIT 100`, tenantID, d.ID)
		if err == nil {
			defer func() { _ = eRows.Close() }()
			for eRows.Next() {
				var e DSARExpense
				if err := eRows.Scan(&e.ID, &e.TripID, &e.DriverID, &e.Category, &e.Amount, &e.Description, &e.VerificationState, &e.Status, &e.CreatedAt); err == nil {
					expenses = append(expenses, e)
				}
			}
		}
	}

	// 6. Fetch Driver Advances if driver exists
	var advances []DSARAdvance
	if d != nil {
		advRows, err := s.db.QueryContext(ctx, `
			SELECT id, COALESCE(trip_id, ''), driver_id, amount, reason, status, requested_at, decided_at
			FROM driver_advance_requests
			WHERE tenant_id = $1 AND driver_id = $2
			ORDER BY requested_at DESC LIMIT 100`, tenantID, d.ID)
		if err == nil {
			defer func() { _ = advRows.Close() }()
			for advRows.Next() {
				var adv DSARAdvance
				if err := advRows.Scan(&adv.ID, &adv.TripID, &adv.DriverID, &adv.Amount, &adv.Reason, &adv.Status, &adv.RequestedAt, &adv.DecidedAt); err == nil {
					advances = append(advances, adv)
				}
			}
		}
	}

	// 7. Fetch Audit Log History for user
	var auditRecords []DSARAuditRecord
	audRows, err := s.db.QueryContext(ctx, `
		SELECT id, action, table_name, COALESCE(record_id, ''), created_at
		FROM audit_logs
		WHERE tenant_id = $1 AND user_id = $2
		ORDER BY created_at DESC LIMIT 100`, tenantID, targetUserID)
	if err == nil {
		defer func() { _ = audRows.Close() }()
		for audRows.Next() {
			var rec DSARAuditRecord
			if err := audRows.Scan(&rec.ID, &rec.Action, &rec.TableName, &rec.RecordID, &rec.CreatedAt); err == nil {
				auditRecords = append(auditRecords, rec)
			}
		}
	}

	// 8. Fetch Prior DSAR Requests
	var priorReqs []DSARRequestRecord
	reqRows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, user_id, request_type, status, requested_by, details, created_at, completed_at
		FROM privacy_dsar_requests
		WHERE tenant_id = $1 AND user_id = $2
		ORDER BY created_at DESC LIMIT 20`, tenantID, targetUserID)
	if err == nil {
		defer func() { _ = reqRows.Close() }()
		for reqRows.Next() {
			var r DSARRequestRecord
			if err := reqRows.Scan(&r.ID, &r.TenantID, &r.UserID, &r.RequestType, &r.Status, &r.RequestedBy, &r.Details, &r.CreatedAt, &r.CompletedAt); err == nil {
				priorReqs = append(priorReqs, r)
			}
		}
	}

	// 9. Record this export event in the DSAR ledger
	exportID := "dsar_" + s.idGen.GenerateUUID()
	_, _ = repository.ExecTx(ctx, s.db, `
		INSERT INTO privacy_dsar_requests (id, tenant_id, user_id, request_type, status, requested_by, details, created_at, completed_at)
		VALUES ($1, $2, $3, 'export', 'completed', $4, 'Self-service or admin DPDP §11 export bundle generated', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		exportID, tenantID, targetUserID, requestedBy)

	return &DSARBundle{
		TenantID:          tenantID,
		ExportedAt:        time.Now().UTC(),
		RequestedBy:       requestedBy,
		User:              u,
		Consents:          consents,
		Driver:            d,
		Assignments:       assignments,
		Expenses:          expenses,
		Advances:          advances,
		AuditHistory:      auditRecords,
		PriorRequests:     priorReqs,
		RetentionSchedule: s.DefaultRetentionSchedule(),
		SubProcessors:     s.DefaultSubProcessors(),
	}, nil
}

// EraseDataPrincipal anonymizes and erases personal data under DPDP Act §12 while
// strictly preserving statutory business and fiscal records (CGST Act §36).
func (s *DSARService) EraseDataPrincipal(ctx context.Context, tenantID, targetUserID, requestedBy string) (*ErasureResult, error) {
	if tenantID == "" || targetUserID == "" {
		return nil, errors.New("tenant_id and user_id are required")
	}

	// 1. Verify user exists in tenant (fail-closed)
	var userEmail string
	var userPhone sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT email, phone FROM users WHERE tenant_id = $1 AND id = $2`,
		tenantID, targetUserID).Scan(&userEmail, &userPhone)
	if err != nil {
		return nil, err
	}

	cleanPhone := ""
	if userPhone.Valid {
		cleanPhone = userPhone.String
	}

	pseudoID := s.idGen.GenerateUUID()
	shortID := pseudoID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	erasedEmail := fmt.Sprintf("erased-%s@redacted.local", shortID)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// 2. Anonymize user record
	_, err = tx.ExecContext(ctx, `
		UPDATE users SET
			name = '[ERASED DATA PRINCIPAL]',
			email = $3,
			phone = NULL,
			password_hash = '*ERASED*',
			status = 'inactive',
			google_sub = NULL,
			updated_at = CURRENT_TIMESTAMP
		WHERE tenant_id = $1 AND id = $2`, tenantID, targetUserID, erasedEmail)
	if err != nil {
		return nil, fmt.Errorf("could not anonymize user: %w", err)
	}

	// 3. Invalidate active sessions
	_, _ = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, targetUserID)

	// 4. Mark all consents withdrawn
	_, _ = tx.ExecContext(ctx, `
		UPDATE user_consents SET withdrawn_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE tenant_id = $1 AND user_id = $2`, tenantID, targetUserID)

	// 5. If driver record exists, scrub driver PII
	_, err = tx.ExecContext(ctx, `
		UPDATE drivers SET
			first_name = '[ERASED]',
			last_name = '',
			phone = '',
			email = NULL,
			address = NULL,
			license_number = '[ERASED]',
			emergency_contact_name = NULL,
			emergency_contact_phone = NULL,
			aadhaar = NULL,
			pan = NULL,
			bank_details = NULL,
			notes = 'Personal data erased pursuant to DPDP Act §12',
			status = 'inactive',
			updated_at = CURRENT_TIMESTAMP
		WHERE tenant_id = $1 AND (id = $2 OR driver_id = $2 OR (email != '' AND email = $3) OR (phone != '' AND phone = $4))`,
		tenantID, targetUserID, userEmail, cleanPhone)
	if err != nil {
		return nil, fmt.Errorf("could not scrub driver PII: %w", err)
	}

	// 6. Deactivate driver vehicle assignments
	_, _ = tx.ExecContext(ctx, `
		UPDATE driver_preferred_vehicles
		SET unassigned_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE tenant_id = $1 AND driver_id IN (
			SELECT id FROM drivers WHERE tenant_id = $1 AND (id = $2 OR driver_id = $2 OR (email != '' AND email = $3) OR (phone != '' AND phone = $4))
		)`, tenantID, targetUserID, userEmail, cleanPhone)

	// 7. Record erasure in DSAR ledger
	dsarID := "dsar_" + s.idGen.GenerateUUID()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO privacy_dsar_requests (id, tenant_id, user_id, request_type, status, requested_by, details, created_at, completed_at)
		VALUES ($1, $2, $3, 'erasure', 'completed', $4, 'DPDP Act §12: PII anonymized, sessions invalidated, consents withdrawn, statutory tax records preserved', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		dsarID, tenantID, targetUserID, requestedBy)
	if err != nil {
		return nil, fmt.Errorf("could not record dsar erasure: %w", err)
	}

	// 8. Record audit log
	auditID := "aud_" + s.idGen.GenerateUUID()
	_, _ = tx.ExecContext(ctx, `
		INSERT INTO audit_logs (id, tenant_id, user_id, action, table_name, record_id, new_values, created_at)
		VALUES ($1, $2, $3, 'privacy.dsar.erasure', 'users', $4, '{"status":"erased","dpdp_section":"12"}', CURRENT_TIMESTAMP)`,
		auditID, tenantID, targetUserID, targetUserID)

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &ErasureResult{
		Success:     true,
		UserID:      targetUserID,
		RequestID:   dsarID,
		CompletedAt: time.Now().UTC(),
		ScrubbedFields: []string{
			"name", "email", "phone", "password_hash", "address",
			"emergency_contacts", "license_number", "aadhaar", "pan", "bank_details", "sessions",
		},
		RetainedRecordsNotice: "Statutory transaction and tax ledgers (CGST Act Section 36 / Income Tax Act 8-year retention rule) are retained in an immutable, anonymized state for fiscal compliance.",
	}, nil
}

// ListDSARRequests returns the tenant's DSAR audit ledger history.
func (s *DSARService) ListDSARRequests(ctx context.Context, tenantID string) ([]DSARRequestRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, user_id, request_type, status, requested_by, details, created_at, completed_at
		FROM privacy_dsar_requests
		WHERE tenant_id = $1
		ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []DSARRequestRecord
	for rows.Next() {
		var r DSARRequestRecord
		if err := rows.Scan(&r.ID, &r.TenantID, &r.UserID, &r.RequestType, &r.Status, &r.RequestedBy, &r.Details, &r.CreatedAt, &r.CompletedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
