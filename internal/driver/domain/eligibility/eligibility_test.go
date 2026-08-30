package eligibility_test

import (
	"testing"
	"time"

	"transport-app/internal/driver/domain/eligibility"
)

func TestEvaluateDispatch_FullyEligible(t *testing.T) {
	engine := eligibility.NewEligibilityEngine()
	now := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)

	ctx := eligibility.EligibilityContext{
		EvaluationTime: now,
		TenantPolicy: eligibility.TenantPolicy{
			RequireIdentityForDispatch:   true,
			RequireVerifiedRCForDispatch: true,
		},
		Driver: eligibility.DriverData{
			ID:               "drv-1",
			Status:           "available",
			IsSuspended:      false,
			IdentityVerified: true,
		},
		License: &eligibility.LicenseData{
			Number:             "DL-12345",
			ExpiresOn:          now.Add(365 * 24 * time.Hour),
			VerificationStatus: "verified",
			Classes:            []string{"HMV", "TRANS"},
		},
		Assignment: &eligibility.AssignmentData{
			ID:        "asg-1",
			VehicleID: "veh-1",
			Status:    "active",
		},
		Vehicle: &eligibility.VehicleData{
			ID:        "veh-1",
			Plate:     "DL1LN9999",
			Type:      "truck",
			IsBlocked: false,
		},
		ComplianceDocs: []eligibility.ComplianceDocData{
			{Type: "rc", ExpiresOn: now.Add(365 * 24 * time.Hour), VerificationStatus: "verified"},
			{Type: "insurance", ExpiresOn: now.Add(180 * 24 * time.Hour), VerificationStatus: "verified"},
			{Type: "fitness", ExpiresOn: now.Add(90 * 24 * time.Hour), VerificationStatus: "verified"},
			{Type: "permit", ExpiresOn: now.Add(120 * 24 * time.Hour), VerificationStatus: "verified"},
			{Type: "puc", ExpiresOn: now.Add(60 * 24 * time.Hour), VerificationStatus: "verified"},
		},
	}

	res := engine.EvaluateDispatch(ctx)
	if !res.IsEligible {
		t.Fatalf("expected driver to be eligible, got blockers: %+v", res.Blockers)
	}
	if len(res.Blockers) != 0 {
		t.Fatalf("expected 0 blockers, got %d", len(res.Blockers))
	}
}

func TestEvaluateDispatch_LicenseClassMismatchAndNotOnDuty(t *testing.T) {
	engine := eligibility.NewEligibilityEngine()
	now := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)

	ctx := eligibility.EligibilityContext{
		EvaluationTime: now,
		Driver: eligibility.DriverData{
			ID:          "drv-1",
			Status:      "off_duty",
			IsSuspended: false,
		},
		License: &eligibility.LicenseData{
			Number:             "DL-12345",
			ExpiresOn:          now.Add(365 * 24 * time.Hour),
			VerificationStatus: "verified",
			Classes:            []string{"LMV"}, // Driver only has LMV
		},
		Assignment: &eligibility.AssignmentData{
			ID:        "asg-1",
			VehicleID: "veh-1",
			Status:    "active",
		},
		Vehicle: &eligibility.VehicleData{
			ID:    "veh-1",
			Plate: "DL1LN9999",
			Type:  "truck", // Vehicle requires HMV/TRANS
		},
		ComplianceDocs: []eligibility.ComplianceDocData{
			{Type: "insurance", ExpiresOn: now.Add(180 * 24 * time.Hour), VerificationStatus: "verified"},
			{Type: "fitness", ExpiresOn: now.Add(90 * 24 * time.Hour), VerificationStatus: "verified"},
			{Type: "permit", ExpiresOn: now.Add(120 * 24 * time.Hour), VerificationStatus: "verified"},
			{Type: "puc", ExpiresOn: now.Add(60 * 24 * time.Hour), VerificationStatus: "verified"},
		},
	}

	res := engine.EvaluateDispatch(ctx)
	if res.IsEligible {
		t.Fatal("expected driver to be ineligible due to off_duty and license class mismatch")
	}

	foundOffDuty := false
	foundMismatch := false
	for _, b := range res.Blockers {
		if b.Code == eligibility.BlockerDriverNotOnDuty {
			foundOffDuty = true
		}
		if b.Code == eligibility.BlockerLicenseClassMismatch {
			foundMismatch = true
		}
	}
	if !foundOffDuty {
		t.Error("missing BlockerDriverNotOnDuty blocker")
	}
	if !foundMismatch {
		t.Error("missing BlockerLicenseClassMismatch blocker")
	}
}

func TestEvaluateDispatch_UnverifiedComplianceBlocks(t *testing.T) {
	engine := eligibility.NewEligibilityEngine()
	now := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)

	ctx := eligibility.EligibilityContext{
		EvaluationTime: now,
		Driver: eligibility.DriverData{
			ID:     "drv-1",
			Status: "available",
		},
		License: &eligibility.LicenseData{
			Number:             "DL-12345",
			ExpiresOn:          now.Add(365 * 24 * time.Hour),
			VerificationStatus: "verified",
			Classes:            []string{"LMV"},
		},
		Assignment: &eligibility.AssignmentData{
			ID:        "asg-1",
			VehicleID: "veh-1",
			Status:    "active",
		},
		Vehicle: &eligibility.VehicleData{
			ID:    "veh-1",
			Plate: "DL1LN9999",
			Type:  "van",
		},
		ComplianceDocs: []eligibility.ComplianceDocData{
			{Type: "insurance", ExpiresOn: now.Add(180 * 24 * time.Hour), VerificationStatus: "rejected"},
			{Type: "fitness", ExpiresOn: now.Add(-10 * 24 * time.Hour), VerificationStatus: "verified"}, // Expired
		},
	}

	res := engine.EvaluateDispatch(ctx)
	if res.IsEligible {
		t.Fatal("expected driver to be ineligible due to rejected insurance and expired fitness")
	}

	foundInsUnverified := false
	foundFitExpired := false
	foundPermitMissing := false
	for _, b := range res.Blockers {
		if b.Code == eligibility.BlockerInsuranceUnverified {
			foundInsUnverified = true
		}
		if b.Code == eligibility.BlockerFitnessExpired {
			foundFitExpired = true
		}
		if b.Code == eligibility.BlockerPermitMissing {
			foundPermitMissing = true
		}
	}
	if !foundInsUnverified {
		t.Error("missing BlockerInsuranceUnverified blocker")
	}
	if !foundFitExpired {
		t.Error("missing BlockerFitnessExpired blocker")
	}
	if !foundPermitMissing {
		t.Error("missing BlockerPermitMissing blocker")
	}
}

func TestEvaluateSettlement_DecoupledFromDispatch(t *testing.T) {
	engine := eligibility.NewEligibilityEngine()

	// Dispatch eligible but settlement on hold
	ctx := eligibility.EligibilityContext{
		PayoutAccount: &eligibility.PayoutAccountData{
			VerificationStatus: "verified",
			HoldPayouts:        true,
		},
	}

	res := engine.EvaluateSettlement(ctx)
	if res.IsEligible {
		t.Fatal("expected settlement to be ineligible when payouts on hold")
	}
	if len(res.Blockers) != 1 || res.Blockers[0].Code != eligibility.BlockerPayoutOnHold {
		t.Fatalf("expected BlockerPayoutOnHold, got: %+v", res.Blockers)
	}
}
