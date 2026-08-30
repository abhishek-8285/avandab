package eligibility

import "time"

type BlockerCode string

const (
	BlockerDriverSuspended      BlockerCode = "DRIVER_SUSPENDED"
	BlockerDriverNotOnDuty      BlockerCode = "DRIVER_NOT_ON_DUTY"
	BlockerIdentityPending      BlockerCode = "IDENTITY_VERIFICATION_PENDING"
	BlockerLicenseMissing       BlockerCode = "LICENSE_MISSING"
	BlockerLicenseExpired       BlockerCode = "LICENSE_EXPIRED"
	BlockerLicenseUnverified    BlockerCode = "LICENSE_UNVERIFIED"
	BlockerLicenseClassMismatch BlockerCode = "LICENSE_CLASS_MISMATCH"
	BlockerNoActiveAssignment   BlockerCode = "NO_ACTIVE_ASSIGNMENT"
	BlockerVehicleMissing       BlockerCode = "VEHICLE_MISSING"
	BlockerVehicleBlocked       BlockerCode = "VEHICLE_BLOCKED"

	// Granular Compliance Blockers
	BlockerRCMissing           BlockerCode = "RC_MISSING"
	BlockerRCUnverified        BlockerCode = "RC_UNVERIFIED"
	BlockerRCExpired           BlockerCode = "RC_EXPIRED"
	BlockerInsuranceMissing    BlockerCode = "INSURANCE_MISSING"
	BlockerInsuranceUnverified BlockerCode = "INSURANCE_UNVERIFIED"
	BlockerInsuranceExpired    BlockerCode = "INSURANCE_EXPIRED"
	BlockerFitnessMissing      BlockerCode = "FITNESS_MISSING"
	BlockerFitnessUnverified   BlockerCode = "FITNESS_UNVERIFIED"
	BlockerFitnessExpired      BlockerCode = "FITNESS_EXPIRED"
	BlockerPermitMissing       BlockerCode = "PERMIT_MISSING"
	BlockerPermitUnverified    BlockerCode = "PERMIT_UNVERIFIED"
	BlockerPermitExpired       BlockerCode = "PERMIT_EXPIRED"
	BlockerPUCMissing          BlockerCode = "PUC_MISSING"
	BlockerPUCUnverified       BlockerCode = "PUC_UNVERIFIED"
	BlockerPUCExpired          BlockerCode = "PUC_EXPIRED"

	// Settlement Blockers
	BlockerBankMissing    BlockerCode = "BANK_MISSING"
	BlockerBankUnverified BlockerCode = "BANK_UNVERIFIED"
	BlockerPayoutOnHold   BlockerCode = "PAYOUT_ON_HOLD"
)

type Blocker struct {
	Code     BlockerCode `json:"code"`
	Severity string      `json:"severity"` // "blocking" | "warning"
	Entity   string      `json:"entity"`   // "driver" | "license" | "vehicle" | "payout"
	Message  string      `json:"message"`
}

type EligibilityContext struct {
	EvaluationTime time.Time
	TenantPolicy   TenantPolicy
	Driver         DriverData
	License        *LicenseData
	Assignment     *AssignmentData
	Vehicle        *VehicleData
	ComplianceDocs []ComplianceDocData
	PayoutAccount  *PayoutAccountData
}

type TenantPolicy struct {
	RequireIdentityForDispatch   bool
	RequireVerifiedRCForDispatch bool
	AllowWarningForExpiredPUC    bool
	AllowWarningForMissingPUC    bool
	RequiredClassesByVehicleType map[string][]string
}

type DriverData struct {
	ID               string
	Status           string // "available", "on_duty", "off_duty", "leave"
	IsSuspended      bool
	IdentityVerified bool
}

type LicenseData struct {
	Number             string
	ExpiresOn          time.Time
	VerificationStatus string // "verified", "pending", "rejected", "expired"
	Classes            []string
}

type AssignmentData struct {
	ID        string
	VehicleID string
	Status    string // "active", "pending", "ended"
}

type VehicleData struct {
	ID        string
	Plate     string
	Type      string // "truck", "tempo", "van", "hmv", "lmv"
	IsBlocked bool
}

type ComplianceDocData struct {
	Type               string // "rc", "insurance", "fitness", "permit", "puc"
	ExpiresOn          time.Time
	VerificationStatus string // "verified", "pending", "rejected", "expired"
}

type PayoutAccountData struct {
	VerificationStatus string // "verified", "penny_drop_pending", "rejected"
	HoldPayouts        bool
}

type DispatchEligibilityResult struct {
	IsEligible  bool      `json:"is_eligible"`
	EvaluatedAt time.Time `json:"evaluated_at"`
	Blockers    []Blocker `json:"blockers"`
}

type SettlementEligibilityResult struct {
	IsEligible  bool      `json:"is_eligible"`
	EvaluatedAt time.Time `json:"evaluated_at"`
	Blockers    []Blocker `json:"blockers"`
}

type EligibilityEngine struct{}

func NewEligibilityEngine() *EligibilityEngine {
	return &EligibilityEngine{}
}

func (e *EligibilityEngine) EvaluateDispatch(ctx EligibilityContext) DispatchEligibilityResult {
	now := ctx.EvaluationTime
	if now.IsZero() {
		now = time.Now()
	}

	var blockers []Blocker

	// 1. Driver Account Rules
	if ctx.Driver.IsSuspended {
		blockers = append(blockers, Blocker{
			Code:     BlockerDriverSuspended,
			Severity: "blocking",
			Entity:   "driver",
			Message:  "Driver account is administratively suspended",
		})
	}
	if !ctx.Driver.IsSuspended && ctx.Driver.Status != "available" && ctx.Driver.Status != "on_duty" {
		blockers = append(blockers, Blocker{
			Code:     BlockerDriverNotOnDuty,
			Severity: "blocking",
			Entity:   "driver",
			Message:  "Driver is not on duty or available",
		})
	}
	if !ctx.Driver.IdentityVerified {
		sev := "warning"
		if ctx.TenantPolicy.RequireIdentityForDispatch {
			sev = "blocking"
		}
		blockers = append(blockers, Blocker{
			Code:     BlockerIdentityPending,
			Severity: sev,
			Entity:   "driver",
			Message:  "Driver identity verification is pending",
		})
	}

	// 2. License Rules
	if ctx.License == nil {
		blockers = append(blockers, Blocker{
			Code:     BlockerLicenseMissing,
			Severity: "blocking",
			Entity:   "license",
			Message:  "No driving license on file",
		})
	} else {
		if ctx.License.ExpiresOn.Before(now) {
			blockers = append(blockers, Blocker{
				Code:     BlockerLicenseExpired,
				Severity: "blocking",
				Entity:   "license",
				Message:  "Driving license has expired",
			})
		}
		if ctx.License.VerificationStatus != "verified" {
			blockers = append(blockers, Blocker{
				Code:     BlockerLicenseUnverified,
				Severity: "blocking",
				Entity:   "license",
				Message:  "Driving license verification is pending or rejected",
			})
		}
	}

	// 3. Assignment Rules
	if ctx.Assignment == nil || ctx.Assignment.Status != "active" || ctx.Assignment.VehicleID == "" {
		blockers = append(blockers, Blocker{
			Code:     BlockerNoActiveAssignment,
			Severity: "blocking",
			Entity:   "assignment",
			Message:  "No active vehicle assignment",
		})
	}

	// 4. Vehicle & Compliance Rules
	if ctx.Vehicle == nil {
		if ctx.Assignment != nil && ctx.Assignment.VehicleID != "" {
			blockers = append(blockers, Blocker{
				Code:     BlockerVehicleMissing,
				Severity: "blocking",
				Entity:   "vehicle",
				Message:  "Assigned vehicle record not found",
			})
		}
	} else {
		if ctx.Vehicle.IsBlocked {
			blockers = append(blockers, Blocker{
				Code:     BlockerVehicleBlocked,
				Severity: "blocking",
				Entity:   "vehicle",
				Message:  "Vehicle is administratively blocked",
			})
		}

		// License Class Mismatch Check
		if ctx.License != nil && ctx.Vehicle.Type != "" {
			reqClasses := ctx.TenantPolicy.RequiredClassesByVehicleType[ctx.Vehicle.Type]
			if len(reqClasses) == 0 {
				// Compiled default fallback
				if ctx.Vehicle.Type == "truck" || ctx.Vehicle.Type == "hmv" {
					reqClasses = []string{"HMV", "TRANS"}
				} else {
					reqClasses = []string{"LMV"}
				}
			}
			hasRequiredClass := false
			for _, rc := range reqClasses {
				for _, lc := range ctx.License.Classes {
					if lc == rc {
						hasRequiredClass = true
						break
					}
				}
				if hasRequiredClass {
					break
				}
			}
			if !hasRequiredClass {
				blockers = append(blockers, Blocker{
					Code:     BlockerLicenseClassMismatch,
					Severity: "blocking",
					Entity:   "license",
					Message:  "Driver license class endorsement does not match vehicle type",
				})
			}
		}

		docMap := make(map[string]ComplianceDocData)
		for _, doc := range ctx.ComplianceDocs {
			docMap[doc.Type] = doc
		}

		// RC Check
		if rc, ok := docMap["rc"]; !ok {
			if ctx.TenantPolicy.RequireVerifiedRCForDispatch {
				blockers = append(blockers, Blocker{
					Code:     BlockerRCMissing,
					Severity: "blocking",
					Entity:   "vehicle",
					Message:  "Vehicle registration certificate (RC) is missing",
				})
			}
		} else {
			if rc.VerificationStatus != "verified" {
				blockers = append(blockers, Blocker{
					Code:     BlockerRCUnverified,
					Severity: "blocking",
					Entity:   "vehicle",
					Message:  "Vehicle registration certificate (RC) is not verified",
				})
			}
			if !rc.ExpiresOn.IsZero() && rc.ExpiresOn.Before(now) {
				blockers = append(blockers, Blocker{
					Code:     BlockerRCExpired,
					Severity: "blocking",
					Entity:   "vehicle",
					Message:  "Vehicle registration certificate (RC) has expired",
				})
			}
		}

		// Insurance Check
		if ins, ok := docMap["insurance"]; !ok {
			blockers = append(blockers, Blocker{
				Code:     BlockerInsuranceMissing,
				Severity: "blocking",
				Entity:   "vehicle",
				Message:  "Vehicle insurance policy is missing",
			})
		} else {
			if ins.VerificationStatus != "verified" {
				blockers = append(blockers, Blocker{
					Code:     BlockerInsuranceUnverified,
					Severity: "blocking",
					Entity:   "vehicle",
					Message:  "Vehicle insurance policy is unverified",
				})
			}
			if ins.ExpiresOn.Before(now) {
				blockers = append(blockers, Blocker{
					Code:     BlockerInsuranceExpired,
					Severity: "blocking",
					Entity:   "vehicle",
					Message:  "Vehicle insurance policy has expired",
				})
			}
		}

		// Fitness Check
		if fit, ok := docMap["fitness"]; !ok {
			blockers = append(blockers, Blocker{
				Code:     BlockerFitnessMissing,
				Severity: "blocking",
				Entity:   "vehicle",
				Message:  "Vehicle fitness certificate is missing",
			})
		} else {
			if fit.VerificationStatus != "verified" {
				blockers = append(blockers, Blocker{
					Code:     BlockerFitnessUnverified,
					Severity: "blocking",
					Entity:   "vehicle",
					Message:  "Vehicle fitness certificate is unverified",
				})
			}
			if fit.ExpiresOn.Before(now) {
				blockers = append(blockers, Blocker{
					Code:     BlockerFitnessExpired,
					Severity: "blocking",
					Entity:   "vehicle",
					Message:  "Vehicle fitness certificate has expired",
				})
			}
		}

		// Permit Check
		if per, ok := docMap["permit"]; !ok {
			blockers = append(blockers, Blocker{
				Code:     BlockerPermitMissing,
				Severity: "blocking",
				Entity:   "vehicle",
				Message:  "Vehicle commercial permit is missing",
			})
		} else {
			if per.VerificationStatus != "verified" {
				blockers = append(blockers, Blocker{
					Code:     BlockerPermitUnverified,
					Severity: "blocking",
					Entity:   "vehicle",
					Message:  "Vehicle commercial permit is unverified",
				})
			}
			if per.ExpiresOn.Before(now) {
				blockers = append(blockers, Blocker{
					Code:     BlockerPermitExpired,
					Severity: "blocking",
					Entity:   "vehicle",
					Message:  "Vehicle commercial permit has expired",
				})
			}
		}

		// PUC Check
		if puc, ok := docMap["puc"]; !ok {
			sev := "blocking"
			if ctx.TenantPolicy.AllowWarningForMissingPUC {
				sev = "warning"
			}
			blockers = append(blockers, Blocker{
				Code:     BlockerPUCMissing,
				Severity: sev,
				Entity:   "vehicle",
				Message:  "Vehicle pollution under control (PUC) certificate is missing",
			})
		} else {
			if puc.VerificationStatus != "verified" {
				blockers = append(blockers, Blocker{
					Code:     BlockerPUCUnverified,
					Severity: "blocking",
					Entity:   "vehicle",
					Message:  "Vehicle pollution under control (PUC) certificate is unverified",
				})
			}
			if puc.ExpiresOn.Before(now) {
				sev := "blocking"
				if ctx.TenantPolicy.AllowWarningForExpiredPUC {
					sev = "warning"
				}
				blockers = append(blockers, Blocker{
					Code:     BlockerPUCExpired,
					Severity: sev,
					Entity:   "vehicle",
					Message:  "Vehicle pollution under control (PUC) certificate has expired",
				})
			}
		}
	}

	isEligible := true
	for _, b := range blockers {
		if b.Severity == "blocking" {
			isEligible = false
			break
		}
	}

	return DispatchEligibilityResult{
		IsEligible:  isEligible,
		EvaluatedAt: now,
		Blockers:    blockers,
	}
}

func (e *EligibilityEngine) EvaluateSettlement(ctx EligibilityContext) SettlementEligibilityResult {
	now := ctx.EvaluationTime
	if now.IsZero() {
		now = time.Now()
	}

	var blockers []Blocker

	if ctx.PayoutAccount == nil {
		blockers = append(blockers, Blocker{
			Code:     BlockerBankMissing,
			Severity: "blocking",
			Entity:   "payout",
			Message:  "No payout bank account on file",
		})
	} else {
		if ctx.PayoutAccount.VerificationStatus != "verified" {
			blockers = append(blockers, Blocker{
				Code:     BlockerBankUnverified,
				Severity: "blocking",
				Entity:   "payout",
				Message:  "Primary payout bank account is unverified",
			})
		}
		if ctx.PayoutAccount.HoldPayouts {
			blockers = append(blockers, Blocker{
				Code:     BlockerPayoutOnHold,
				Severity: "blocking",
				Entity:   "payout",
				Message:  "Settlement payouts are administratively on hold",
			})
		}
	}

	return SettlementEligibilityResult{
		IsEligible:  len(blockers) == 0,
		EvaluatedAt: now,
		Blockers:    blockers,
	}
}
