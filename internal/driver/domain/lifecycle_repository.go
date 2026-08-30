package domain

import (
	"context"
	"time"
)

type DriverLicenseRepository interface {
	SaveLicense(ctx context.Context, tenantID, driverID string, lic DriverLicenseRecord, classes []string) error
	GetCurrentLicense(ctx context.Context, tenantID, driverID string) (*DriverLicenseRecord, []string, error)
	VerifyLicense(ctx context.Context, tenantID, licenseID string, status string, verifiedBy string) error
}

type DriverVehicleAssignmentRepository interface {
	CreateAssignment(ctx context.Context, tenantID string, asg DriverVehicleAssignmentRecord) error
	GetActiveAssignmentForDriver(ctx context.Context, tenantID, driverID string) (*DriverVehicleAssignmentRecord, error)
	GetActiveAssignmentForVehicle(ctx context.Context, tenantID, vehicleID string) (*DriverVehicleAssignmentRecord, error)
	EndAssignment(ctx context.Context, tenantID, assignmentID string, endedAt time.Time) error
}

type VehicleComplianceRepository interface {
	SaveComplianceDoc(ctx context.Context, tenantID, vehicleID string, doc VehicleComplianceDocRecord) error
	GetActiveComplianceDocs(ctx context.Context, tenantID, vehicleID string) ([]VehicleComplianceDocRecord, error)
	VerifyComplianceDoc(ctx context.Context, tenantID, docID string, status string, verifiedBy string, rejectionReason string) error
}

type VehicleOwnershipRepository interface {
	CreateClaim(ctx context.Context, tenantID string, claim VehicleClaimRecord) error
	ReviewClaim(ctx context.Context, tenantID, claimID string, status string, reviewedBy string, rejectionReason string) error
	GetActiveOwnership(ctx context.Context, tenantID, vehicleID string) (*VehicleOwnershipRecord, error)
}

type TelemetrySessionRepository interface {
	RegisterInstallation(ctx context.Context, tenantID string, inst TelemetryInstallationRecord) error
	StartSession(ctx context.Context, tenantID string, sess TelemetrySessionRecord) error
	GetActiveSession(ctx context.Context, tenantID, driverID string) (*TelemetrySessionRecord, error)
	EndSession(ctx context.Context, tenantID, sessionID string, endReason string, endedAt time.Time) error
	IngestEvent(ctx context.Context, tenantID string, evt TelemetryEventRecord) error
	UpsertLatestPosition(ctx context.Context, tenantID string, pos VehicleLatestPositionRecord) error
	GetLatestPosition(ctx context.Context, tenantID, vehicleID string) (*VehicleLatestPositionRecord, error)
}

type AuditRepository interface {
	RecordAuditEvent(ctx context.Context, tenantID string, evt AuditEventRecord) error
	RecordVerificationAttempt(ctx context.Context, tenantID string, attempt VerificationAttemptRecord) error
}

type DriverOnboardingRepository interface {
	GetOnboardingState(ctx context.Context, tenantID, driverID string) (*DriverOnboardingRecord, error)
	UpdateOnboardingStep(ctx context.Context, tenantID, driverID string, step string, overallStatus string) error
}
