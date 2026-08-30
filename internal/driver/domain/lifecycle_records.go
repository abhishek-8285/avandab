package domain

import "time"

type DriverLicenseRecord struct {
	ID                 string
	TenantID           string
	DriverID           string
	LicenseNumber      string
	IssuingAuthority   string
	IssuedOn           *time.Time
	ExpiresOn          time.Time
	IsCurrent          bool
	SupersededAt       *time.Time
	VerificationStatus string // unverified, pending, verified, rejected, expired
	VerifiedAt         *time.Time
	CreatedAt          time.Time
}

type DriverVehicleAssignmentRecord struct {
	ID             string
	TenantID       string
	DriverID       string
	VehicleID      string
	AssignmentType string // company_assigned, owner_operator_claim, temporary_relief
	Status         string // pending, active, ended, rejected
	StartedAt      *time.Time
	EndedAt        *time.Time
	AssignedBy     *string
	AcceptedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type VehicleComplianceDocRecord struct {
	ID                 string
	TenantID           string
	VehicleID          string
	DocumentType       string // rc, insurance, fitness, permit, puc, road_tax
	DocumentNumber     string
	StorageKey         string
	IssuedOn           *time.Time
	ExpiresOn          time.Time
	IsCurrent          bool
	SupersededAt       *time.Time
	VerificationStatus string // unverified, pending, verified, rejected, expired
	VerifiedBy         *string
	VerifiedAt         *time.Time
	RejectionReason    *string
	CreatedAt          time.Time
}

type VehicleClaimRecord struct {
	ID                 string
	TenantID           string
	DriverID           string
	RegistrationNumber string
	RCDocumentID       *string
	Status             string // submitted, under_review, approved, rejected, disputed
	ReviewedBy         *string
	ReviewedAt         *time.Time
	RejectionReason    *string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type VehicleOwnershipRecord struct {
	ID             string
	TenantID       string
	VehicleID      string
	OwnerPartyType string // driver, company, transporter_partner, leasing_company
	OwnerPartyID   string
	ValidFrom      time.Time
	ValidUntil     *time.Time
	CreatedAt      time.Time
}

type TelemetryInstallationRecord struct {
	ID                string
	TenantID          string
	AppInstallationID string
	Platform          string // android, ios, hardware_gps, obd_dongle
	AppVersion        string
	DeviceModel       string
	OSVersion         string
	Status            string // active, revoked
	LastSeenAt        time.Time
	CreatedAt         time.Time
}

type TelemetrySessionRecord struct {
	ID              string
	TenantID        string
	InstallationID  string
	DriverID        string
	VehicleID       *string
	TripID          *string
	SessionType     string // on_duty, trip_active, relief_standby
	Status          string // active, closed, interrupted
	StartReason     string // APP_AVAILABLE, TRIP_STARTED, MANUAL_START, SYSTEM_RECOVERY
	EndReason       *string
	StartedAt       time.Time
	EndedAt         *time.Time
	TotalDistanceKm float64
	PositionsCount  int
}

type TelemetryEventRecord struct {
	ID            string
	TenantID      string
	SessionID     string
	ClientEventID string
	OccurredAt    time.Time
	ReceivedAt    time.Time
	Latitude      float64
	Longitude     float64
	Speed         float64
	Accuracy      *float64
	Heading       *float64
	Altitude      *float64
	RawPayload    string
}

type VehicleLatestPositionRecord struct {
	TenantID   string
	VehicleID  string
	SessionID  string
	DriverID   string
	Latitude   float64
	Longitude  float64
	Accuracy   *float64
	Speed      float64
	Heading    *float64
	OccurredAt time.Time
	ReceivedAt time.Time
	Source     string
}

type AuditEventRecord struct {
	ID          string
	TenantID    string
	ActorUserID *string
	EntityType  string
	EntityID    string
	Action      string
	OldState    *string
	NewState    *string
	Reason      *string
	RequestID   *string
	CreatedAt   time.Time
}

type VerificationAttemptRecord struct {
	ID                string
	TenantID          string
	EntityType        string
	EntityID          string
	Provider          string
	ProviderReference *string
	Status            string // initiated, success, failed, timeout
	RequestedAt       time.Time
	CompletedAt       *time.Time
	FailureCode       *string
	FailureReason     *string
}

type DriverOnboardingRecord struct {
	DriverID       string
	TenantID       string
	CurrentStep    string
	IdentityStatus string
	LicenseStatus  string
	VehicleStatus  string
	BankStatus     string
	OverallStatus  string
	StartedAt      time.Time
	CompletedAt    *time.Time
}
