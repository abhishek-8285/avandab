package freightaudit

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateRateCard(ctx context.Context, tenantID string, in CreateRateCardInput) (*RateCard, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}

	now := time.Now().UTC()
	effectiveFrom := now
	if in.EffectiveFrom != nil {
		effectiveFrom = *in.EffectiveFrom
	}

	rc := &RateCard{
		ID:                   uuid.NewString(),
		TenantID:             tenantID,
		ContractNumber:       strings.ToUpper(strings.TrimSpace(in.ContractNumber)),
		Name:                 strings.TrimSpace(in.Name),
		VendorID:             in.VendorID,
		CustomerID:           in.CustomerID,
		RouteID:              in.RouteID,
		VehicleType:          strings.ToUpper(strings.TrimSpace(in.VehicleType)),
		BillingBasis:         in.BillingBasis,
		Rate:                 in.Rate,
		MinCharge:            in.MinCharge,
		FreeDetentionHours:   in.FreeDetentionHours,
		DetentionRatePerHour: in.DetentionRatePerHour,
		EffectiveFrom:        effectiveFrom,
		EffectiveTo:          in.EffectiveTo,
		IsActive:             true,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	if err := s.repo.CreateRateCard(ctx, rc); err != nil {
		return nil, err
	}
	return rc, nil
}

func (s *Service) GetRateCard(ctx context.Context, tenantID, id string) (*RateCard, error) {
	return s.repo.GetRateCardByID(ctx, tenantID, id)
}

func (s *Service) ListRateCards(ctx context.Context, tenantID string, filter RateCardFilter) ([]RateCard, int, error) {
	return s.repo.ListRateCards(ctx, tenantID, filter)
}

func (s *Service) UpdateRateCard(ctx context.Context, tenantID, id string, in UpdateRateCardInput) (*RateCard, error) {
	rc, err := s.repo.GetRateCardByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	if in.Name != nil {
		rc.Name = strings.TrimSpace(*in.Name)
	}
	if in.Rate != nil {
		if *in.Rate < 0.0 {
			return nil, fmt.Errorf("%w: rate must be non-negative", ErrInvalidInput)
		}
		rc.Rate = *in.Rate
	}
	if in.MinCharge != nil {
		if *in.MinCharge < 0.0 {
			return nil, fmt.Errorf("%w: min_charge must be non-negative", ErrInvalidInput)
		}
		rc.MinCharge = *in.MinCharge
	}
	if in.FreeDetentionHours != nil {
		rc.FreeDetentionHours = *in.FreeDetentionHours
	}
	if in.DetentionRatePerHour != nil {
		rc.DetentionRatePerHour = *in.DetentionRatePerHour
	}
	if in.EffectiveTo != nil {
		rc.EffectiveTo = in.EffectiveTo
	}
	if in.IsActive != nil {
		rc.IsActive = *in.IsActive
	}
	rc.UpdatedAt = time.Now().UTC()

	if err := s.repo.UpdateRateCard(ctx, rc); err != nil {
		return nil, err
	}
	return rc, nil
}

func (s *Service) CreateAudit(ctx context.Context, tenantID string, in CreateAuditInput) (*FreightAudit, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}

	now := time.Now().UTC()
	auditID := uuid.NewString()
	auditNumber := fmt.Sprintf("FA-%d-%s", now.Unix()%1000000, strings.ToUpper(uuid.NewString()[:6]))

	// Ground-truth gathering
	var tripData *TripAuditData
	if in.TripID != nil && *in.TripID != "" {
		td, err := s.repo.GetTripAuditData(ctx, tenantID, *in.TripID)
		if err == nil {
			tripData = td
		}
	}

	// Determine vehicle and vendor
	vehicleNum := in.VehicleNumber
	if vehicleNum == "" && tripData != nil {
		vehicleNum = tripData.VehicleNumber
	}
	vendorName := in.VendorName
	if vendorName == "" {
		vendorName = "Carrier Partner"
	}

	// Determine Rate Card & Tariff
	var rateCard *RateCard
	if in.RateCardID != nil && *in.RateCardID != "" {
		rc, err := s.repo.GetRateCardByID(ctx, tenantID, *in.RateCardID)
		if err == nil {
			rateCard = rc
		}
	} else if tripData != nil {
		rc, err := s.repo.FindMatchingRateCard(ctx, tenantID, tripData.RouteID, "")
		if err == nil {
			rateCard = rc
		}
	}

	// Determine Billing Basis
	billingBasis := BillingBasisPerKM
	if in.BillingBasis != nil && in.BillingBasis.IsValid() {
		billingBasis = *in.BillingBasis
	} else if rateCard != nil {
		billingBasis = rateCard.BillingBasis
	}

	// 1. Distance Ground Truth
	gpsDistance := 0.0
	if in.GPSDistanceKM != nil {
		gpsDistance = *in.GPSDistanceKM
	} else if tripData != nil {
		if tripData.StartOdometer != nil && tripData.CloseOdometer != nil && *tripData.CloseOdometer > *tripData.StartOdometer {
			gpsDistance = *tripData.CloseOdometer - *tripData.StartOdometer
		} else if tripData.RouteDistanceKM > 0 {
			gpsDistance = tripData.RouteDistanceKM
		}
	}
	if gpsDistance == 0.0 && in.BilledDistanceKM > 0.0 {
		gpsDistance = in.BilledDistanceKM
	}

	// 2. Weight Ground Truth
	actualWeight := 0.0
	if in.ActualWeightKG != nil {
		actualWeight = *in.ActualWeightKG
	} else if tripData != nil && tripData.CargoWeightKG > 0 {
		actualWeight = tripData.CargoWeightKG
	} else if in.BilledWeightKG > 0 {
		actualWeight = in.BilledWeightKG
	}

	// 3. Rate Ground Truth
	auditedRate := 0.0
	minCharge := 0.0
	freeDetentionHours := 0.0
	detentionRatePerHour := 0.0

	if rateCard != nil {
		auditedRate = rateCard.Rate
		minCharge = rateCard.MinCharge
		freeDetentionHours = rateCard.FreeDetentionHours
		detentionRatePerHour = rateCard.DetentionRatePerHour
	} else if tripData != nil && tripData.StandardFare > 0 {
		auditedRate = tripData.StandardFare
	}

	if in.AuditedRate != nil {
		auditedRate = *in.AuditedRate
	} else if auditedRate == 0.0 && in.BilledRate > 0 {
		auditedRate = in.BilledRate
	}

	// 4. Toll Ground Truth (from FASTag)
	auditedTolls := 0.0
	if in.AuditedTollCharges != nil {
		auditedTolls = *in.AuditedTollCharges
	} else if tripData != nil {
		auditedTolls = tripData.FASTagTolls
	} else {
		auditedTolls = in.BilledTollCharges
	}

	// 5. Detention Ground Truth
	auditedDetentionHours := 0.0
	if in.AuditedDetentionHours != nil {
		auditedDetentionHours = *in.AuditedDetentionHours
	}
	chargeableDetentionHours := math.Max(0.0, auditedDetentionHours-freeDetentionHours)
	auditedDetentionCharges := chargeableDetentionHours * detentionRatePerHour

	// 6. Base Freight Calculation
	auditedBaseFreight := 0.0
	switch billingBasis {
	case BillingBasisPerKM:
		auditedBaseFreight = auditedRate * gpsDistance
	case BillingBasisPerKG:
		auditedBaseFreight = auditedRate * actualWeight
	case BillingBasisPerTon:
		auditedBaseFreight = auditedRate * (actualWeight / 1000.0)
	case BillingBasisPerTonKM:
		auditedBaseFreight = auditedRate * (actualWeight / 1000.0) * gpsDistance
	case BillingBasisFlat:
		auditedBaseFreight = auditedRate
	}
	if auditedBaseFreight < minCharge {
		auditedBaseFreight = minCharge
	}

	auditedTotal := round2(auditedBaseFreight + auditedTolls + auditedDetentionCharges)

	// In case billed total wasn't provided, build from components
	billedBaseFreight := in.BilledBaseFreight
	if billedBaseFreight == 0.0 {
		switch billingBasis {
		case BillingBasisPerKM:
			billedBaseFreight = in.BilledRate * in.BilledDistanceKM
		case BillingBasisPerKG:
			billedBaseFreight = in.BilledRate * in.BilledWeightKG
		case BillingBasisPerTon:
			billedBaseFreight = in.BilledRate * (in.BilledWeightKG / 1000.0)
		case BillingBasisPerTonKM:
			billedBaseFreight = in.BilledRate * (in.BilledWeightKG / 1000.0) * in.BilledDistanceKM
		case BillingBasisFlat:
			billedBaseFreight = in.BilledRate
		}
	}
	billedTotal := in.BilledTotal
	if billedTotal == 0.0 {
		billedTotal = round2(billedBaseFreight + in.BilledTollCharges + in.BilledDetentionCharges)
	}

	// Variances
	distVar := math.Max(0.0, round2(in.BilledDistanceKM-gpsDistance))
	weightVar := math.Max(0.0, round2(in.BilledWeightKG-actualWeight))
	rateVar := math.Max(0.0, round2(in.BilledRate-auditedRate))
	tollVar := math.Max(0.0, round2(in.BilledTollCharges-auditedTolls))
	overbilling := math.Max(0.0, round2(billedTotal-auditedTotal))

	variancePct := 0.0
	if billedTotal > 0.0 {
		variancePct = round2((overbilling / billedTotal) * 100.0)
	}

	// Build Granular Deductions
	var deductions []FreightDeduction
	var flags []string

	if distVar > 1.0 && (billingBasis == BillingBasisPerKM || billingBasis == BillingBasisPerTonKM) {
		flags = append(flags, "DISTANCE_INFLATION")
		deductAmt := round2(distVar * auditedRate)
		if billingBasis == BillingBasisPerTonKM && actualWeight > 0 {
			deductAmt = round2(distVar * (actualWeight / 1000.0) * auditedRate)
		}
		deductions = append(deductions, FreightDeduction{
			ID:              uuid.NewString(),
			TenantID:        tenantID,
			AuditID:         auditID,
			Category:        DeductionCategoryDistanceInflation,
			Description:     fmt.Sprintf("GPS distance variance: billed %.1f km vs GPS %.1f km (%.1f km excess)", in.BilledDistanceKM, gpsDistance, distVar),
			ClaimedValue:    in.BilledDistanceKM,
			VerifiedValue:   gpsDistance,
			UnitRate:        auditedRate,
			DeductionAmount: deductAmt,
			CreatedAt:       now,
		})
	}

	if weightVar > 10.0 && (billingBasis == BillingBasisPerKG || billingBasis == BillingBasisPerTon || billingBasis == BillingBasisPerTonKM) {
		flags = append(flags, "WEIGHT_MISMATCH")
		deductAmt := 0.0
		if billingBasis == BillingBasisPerKG {
			deductAmt = round2(weightVar * auditedRate)
		} else if billingBasis == BillingBasisPerTon {
			deductAmt = round2((weightVar / 1000.0) * auditedRate)
		} else if billingBasis == BillingBasisPerTonKM {
			deductAmt = round2((weightVar / 1000.0) * gpsDistance * auditedRate)
		}
		deductions = append(deductions, FreightDeduction{
			ID:              uuid.NewString(),
			TenantID:        tenantID,
			AuditID:         auditID,
			Category:        DeductionCategoryWeightMismatch,
			Description:     fmt.Sprintf("Consignment weight variance: billed %.1f kg vs actual %.1f kg (%.1f kg excess)", in.BilledWeightKG, actualWeight, weightVar),
			ClaimedValue:    in.BilledWeightKG,
			VerifiedValue:   actualWeight,
			UnitRate:        auditedRate,
			DeductionAmount: deductAmt,
			CreatedAt:       now,
		})
	}

	if rateVar > 0.01 {
		flags = append(flags, "RATE_OVERCHARGE")
		var qty float64
		switch billingBasis {
		case BillingBasisPerKM:
			qty = gpsDistance
		case BillingBasisPerKG:
			qty = actualWeight
		case BillingBasisPerTon:
			qty = actualWeight / 1000.0
		case BillingBasisPerTonKM:
			qty = (actualWeight / 1000.0) * gpsDistance
		case BillingBasisFlat:
			qty = 1.0
		}
		deductions = append(deductions, FreightDeduction{
			ID:              uuid.NewString(),
			TenantID:        tenantID,
			AuditID:         auditID,
			Category:        DeductionCategoryRateOvercharge,
			Description:     fmt.Sprintf("Rate card discrepancy: billed ₹%.2f vs contracted rate ₹%.2f", in.BilledRate, auditedRate),
			ClaimedValue:    in.BilledRate,
			VerifiedValue:   auditedRate,
			UnitRate:        rateVar,
			DeductionAmount: round2(rateVar * qty),
			CreatedAt:       now,
		})
	}

	if tollVar > 1.0 {
		flags = append(flags, "UNVERIFIED_TOLL")
		deductions = append(deductions, FreightDeduction{
			ID:              uuid.NewString(),
			TenantID:        tenantID,
			AuditID:         auditID,
			Category:        DeductionCategoryUnverifiedToll,
			Description:     fmt.Sprintf("Unverified toll claims: billed ₹%.2f vs FASTag actual ₹%.2f", in.BilledTollCharges, auditedTolls),
			ClaimedValue:    in.BilledTollCharges,
			VerifiedValue:   auditedTolls,
			UnitRate:        1.0,
			DeductionAmount: tollVar,
			CreatedAt:       now,
		})
	}

	status := AuditStatusVerified
	if overbilling > 0.0 || len(flags) > 0 {
		status = AuditStatusDiscrepancyFlagged
	}

	var rateCardIDPtr *string
	if rateCard != nil {
		rateCardIDPtr = &rateCard.ID
	}

	audit := &FreightAudit{
		ID:            auditID,
		TenantID:      tenantID,
		AuditNumber:   auditNumber,
		TripID:        in.TripID,
		InvoiceID:     in.InvoiceID,
		BookingID:     in.BookingID,
		RateCardID:    rateCardIDPtr,
		VendorName:    vendorName,
		VehicleNumber: vehicleNum,
		Status:        status,
		BillingBasis:  billingBasis,

		BilledDistanceKM:       round2(in.BilledDistanceKM),
		BilledWeightKG:         round2(in.BilledWeightKG),
		BilledRate:             round2(in.BilledRate),
		BilledBaseFreight:      round2(billedBaseFreight),
		BilledTollCharges:      round2(in.BilledTollCharges),
		BilledDetentionCharges: round2(in.BilledDetentionCharges),
		BilledTotal:            round2(billedTotal),

		GPSDistanceKM:           round2(gpsDistance),
		ActualWeightKG:          round2(actualWeight),
		AuditedRate:             round2(auditedRate),
		AuditedBaseFreight:      round2(auditedBaseFreight),
		AuditedTollCharges:      round2(auditedTolls),
		AuditedDetentionHours:   round2(auditedDetentionHours),
		AuditedDetentionCharges: round2(auditedDetentionCharges),
		AuditedTotal:            round2(auditedTotal),

		DistanceVarianceKM: distVar,
		WeightVarianceKG:   weightVar,
		RateVariance:       rateVar,
		TollVariance:       tollVar,
		OverbillingAmount:  overbilling,
		VariancePct:        variancePct,

		DeductionAmount:  0.0,
		PayableAmount:    round2(billedTotal),
		DiscrepancyFlags: strings.Join(flags, ","),
		CreatedAt:        now,
		UpdatedAt:        now,
		Deductions:       deductions,
	}

	if err := s.repo.CreateAuditTx(ctx, audit, deductions); err != nil {
		return nil, err
	}

	return audit, nil
}

func (s *Service) ApproveDeduction(ctx context.Context, tenantID, id, approver string, in ApproveDeductionInput) (*FreightAudit, error) {
	audit, err := s.repo.GetAuditByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	if audit.Status == AuditStatusSettled {
		return nil, fmt.Errorf("%w: cannot deduct already settled audit", ErrInvalidStatus)
	}

	deductAmt := audit.OverbillingAmount
	if in.DeductionAmount != nil {
		if *in.DeductionAmount < 0.0 {
			return nil, fmt.Errorf("%w: deduction_amount must be non-negative", ErrInvalidInput)
		}
		deductAmt = *in.DeductionAmount
	}

	now := time.Now().UTC()
	audit.DeductionAmount = round2(deductAmt)
	audit.PayableAmount = round2(math.Max(0.0, audit.BilledTotal-deductAmt))
	audit.DebitNoteNumber = fmt.Sprintf("DN-%s", audit.AuditNumber)
	audit.DeductionReason = in.Notes
	if audit.DeductionReason == "" {
		audit.DeductionReason = fmt.Sprintf("Freight audit overbilling deduction: %s", audit.DiscrepancyFlags)
	}
	audit.ApprovedBy = &approver
	audit.ApprovedAt = &now
	audit.Status = AuditStatusDeductionApproved
	audit.UpdatedAt = now

	if err := s.repo.UpdateAudit(ctx, audit); err != nil {
		return nil, err
	}
	return audit, nil
}

func (s *Service) DisputeAudit(ctx context.Context, tenantID, id, disputer string, in DisputeAuditInput) (*FreightAudit, error) {
	audit, err := s.repo.GetAuditByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	if audit.Status == AuditStatusSettled {
		return nil, fmt.Errorf("%w: cannot dispute settled audit", ErrInvalidStatus)
	}

	now := time.Now().UTC()
	audit.Status = AuditStatusDisputed
	audit.DisputeReason = strings.TrimSpace(in.Reason)
	audit.UpdatedAt = now

	if err := s.repo.UpdateAudit(ctx, audit); err != nil {
		return nil, err
	}
	return audit, nil
}

func (s *Service) SettleAudit(ctx context.Context, tenantID, id, settler string, in SettleAuditInput) (*FreightAudit, error) {
	audit, err := s.repo.GetAuditByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	audit.Status = AuditStatusSettled
	audit.ReviewedBy = &settler
	audit.ReviewedAt = &now
	audit.UpdatedAt = now

	if err := s.repo.UpdateAudit(ctx, audit); err != nil {
		return nil, err
	}
	return audit, nil
}

func (s *Service) GetAudit(ctx context.Context, tenantID, id string) (*FreightAudit, error) {
	return s.repo.GetAuditByID(ctx, tenantID, id)
}

func (s *Service) ListAudits(ctx context.Context, tenantID string, filter AuditFilter) ([]FreightAudit, int, error) {
	return s.repo.ListAudits(ctx, tenantID, filter)
}

func (s *Service) GetSummary(ctx context.Context, tenantID string) (*AuditSummary, error) {
	return s.repo.GetSummary(ctx, tenantID)
}

func round2(val float64) float64 {
	return math.Round(val*100) / 100
}
