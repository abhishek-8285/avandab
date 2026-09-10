package maintenance

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/maintenance/domain"
	maintsql "transport-app/internal/maintenance/infrastructure/sql"
)

// PlanScheduler implements SAP PM IP41 Single Cycle Plan scheduling with
// annual_estimate projection (Spec 04 §14, TMS SOP pp.10-15).
type PlanScheduler struct {
	db   *sql.DB
	repo *maintsql.MaintenanceRepository
}

// NewPlanScheduler creates a new PlanScheduler instance.
func NewPlanScheduler(db *sql.DB, repo *maintsql.MaintenanceRepository) *PlanScheduler {
	if repo == nil {
		repo = maintsql.NewMaintenanceRepository(db)
	}
	return &PlanScheduler{
		db:   db,
		repo: repo,
	}
}

// CalculateProjection evaluates distance and calendar intervals against current
// odometer and annual estimate, returning the projected due KM, due date, and
// whether the call horizon has been reached (TMS SOP p.10).
func CalculateProjection(plan domain.MaintenancePlan, currentOdometer float64, annualEstimate float64, asOf time.Time) (nextDueKM *float64, nextDueDate *time.Time, needsCall bool) {
	dailyRate := 0.0
	if annualEstimate > 0 {
		dailyRate = annualEstimate / 365.0
	}

	// 1. Distance-based cycle (KM)
	if plan.CycleIntervalKM != nil && *plan.CycleIntervalKM > 0 {
		intervalKM := *plan.CycleIntervalKM
		var targetKM float64
		if plan.NextDueKM != nil && *plan.NextDueKM > 0 {
			targetKM = *plan.NextDueKM
		} else if plan.LastScheduledKM != nil {
			targetKM = *plan.LastScheduledKM + intervalKM
		} else {
			targetKM = currentOdometer + intervalKM
		}
		nextDueKM = &targetKM

		if dailyRate > 0 {
			remainingKM := targetKM - currentOdometer
			if remainingKM <= 0 {
				t := asOf
				nextDueDate = &t
			} else {
				daysToDue := remainingKM / dailyRate
				t := asOf.Add(time.Duration(daysToDue * 24 * float64(time.Hour)))
				nextDueDate = &t
			}
		}

		// Call Horizon Check (e.g. 90% or 100%)
		callHorizon := plan.CallHorizonPercent
		if callHorizon <= 0 || callHorizon > 100 {
			callHorizon = 100.0
		}
		callKM := targetKM - (intervalKM * (1.0 - (callHorizon / 100.0)))
		if currentOdometer >= callKM {
			needsCall = true
		}
	}

	// 2. Time-based cycle (Days)
	if plan.CycleIntervalDays != nil && *plan.CycleIntervalDays > 0 {
		intervalDays := *plan.CycleIntervalDays
		baseDate := plan.CreatedAt
		if plan.LastScheduledDate != nil {
			baseDate = *plan.LastScheduledDate
		}
		calendarDueDate := baseDate.AddDate(0, 0, intervalDays)
		if nextDueDate == nil || calendarDueDate.Before(*nextDueDate) {
			nextDueDate = &calendarDueDate
		}

		callHorizon := plan.CallHorizonPercent
		if callHorizon <= 0 || callHorizon > 100 {
			callHorizon = 100.0
		}
		callDays := float64(intervalDays) * (callHorizon / 100.0)
		callDate := baseDate.Add(time.Duration(callDays * 24 * float64(time.Hour)))
		if !asOf.Before(callDate) {
			needsCall = true
		}
	}

	return nextDueKM, nextDueDate, needsCall
}

// EvaluatePlan evaluates a single maintenance plan by ID and opens a job card
// (work order) if the call horizon has been reached.
func (s *PlanScheduler) EvaluatePlan(ctx context.Context, tenantID, planID string, asOf time.Time) (*domain.WorkOrder, error) {
	if tenantID == "" || planID == "" {
		return nil, fmt.Errorf("maintenance: tenant and plan id required")
	}

	plan, err := s.repo.FindMaintenancePlan(ctx, tenantID, planID)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, fmt.Errorf("maintenance: plan %q not found", planID)
	}
	if plan.Status != "active" {
		return nil, nil
	}

	// 1. Fetch current vehicle odometer
	currOdo, _ := s.repo.GetLatestOdometer(ctx, plan.VehicleID)

	// 2. Fetch measuring point annual estimate
	mpID := ""
	if plan.MeasuringPointID != nil {
		mpID = *plan.MeasuringPointID
	}
	annualEstimate, _ := s.repo.GetMeasuringPointAnnualEstimate(ctx, tenantID, plan.VehicleID, mpID)

	// 3. Compute projection
	dueKM, dueDate, needsCall := CalculateProjection(*plan, currOdo, annualEstimate, asOf)

	// Update projected targets on plan
	plan.NextDueKM = dueKM
	plan.NextDueDate = dueDate
	_ = s.repo.UpdateMaintenancePlan(ctx, *plan)

	if !needsCall {
		return nil, nil
	}

	// 4. Deduplicate active job card
	existing, err := s.repo.FindOpenWorkOrderByPlan(ctx, tenantID, plan.VehicleID, plan.ID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	// 5. Generate open work order (Job Card)
	woID := uuid.NewString()
	wo := domain.WorkOrder{
		ID:          woID,
		TenantID:    tenantID,
		VehicleID:   plan.VehicleID,
		PlanID:      &plan.ID,
		DueKM:       dueKM,
		DueAt:       dueDate,
		Title:       fmt.Sprintf("IP41: %s - %s", plan.ServiceType, plan.PlanNumber),
		Description: plan.Description,
		Status:      domain.WorkOrderOpen,
		CreatedAt:   asOf,
	}

	if err := s.repo.CreateWorkOrder(ctx, wo); err != nil {
		return nil, fmt.Errorf("maintenance: failed to generate work order for plan: %w", err)
	}

	// 6. Advance plan schedule history & prepare next cycle
	plan.LastScheduledKM = &currOdo
	plan.LastScheduledDate = &asOf
	if plan.CycleIntervalKM != nil && *plan.CycleIntervalKM > 0 {
		nextKM := currOdo + *plan.CycleIntervalKM
		plan.NextDueKM = &nextKM
	}
	nextKM, nextDate, _ := CalculateProjection(*plan, currOdo, annualEstimate, asOf)
	plan.NextDueKM = nextKM
	plan.NextDueDate = nextDate
	_ = s.repo.UpdateMaintenancePlan(ctx, *plan)

	return &wo, nil
}

// EvaluateAllPlans sweeps active plans for the tenant and triggers calls.
func (s *PlanScheduler) EvaluateAllPlans(ctx context.Context, tenantID string, asOf time.Time) ([]domain.WorkOrder, error) {
	if tenantID == "" {
		return nil, nil
	}
	plans, err := s.repo.ListMaintenancePlans(ctx, tenantID, "")
	if err != nil {
		return nil, err
	}

	var generated []domain.WorkOrder
	for _, p := range plans {
		if p.Status != "active" {
			continue
		}
		wo, err := s.EvaluatePlan(ctx, tenantID, p.ID, asOf)
		if err != nil {
			continue
		}
		if wo != nil {
			generated = append(generated, *wo)
		}
	}
	return generated, nil
}
