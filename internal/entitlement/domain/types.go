package domain

import "errors"

// FeatureKey represents binary feature capabilities.
type FeatureKey string

const (
	FeatureMobileAppEPOD     FeatureKey = "mobile_app_epod"
	FeatureBasicInvoicing    FeatureKey = "basic_invoicing"
	FeatureBasicTracking     FeatureKey = "basic_tracking"
	FeatureMultiStop         FeatureKey = "multi_stop"
	FeatureAutomatedEWB      FeatureKey = "automated_ewb"
	FeatureControlTower      FeatureKey = "control_tower"
	FeatureDriverSettlements FeatureKey = "driver_settlements"
	FeatureAdvancedAnalytics FeatureKey = "advanced_analytics"
	FeatureAPIAccess         FeatureKey = "api_access"
	FeatureDedicatedSLA      FeatureKey = "dedicated_sla"
)

// QuotaKey represents metered capacity keys.
type QuotaKey string

const (
	QuotaMaxVehicles        QuotaKey = "max_vehicles"
	QuotaMaxDrivers         QuotaKey = "max_drivers"
	QuotaMaxTripsPerMonth   QuotaKey = "max_trips_per_month"
	QuotaMaxDispatcherSeats QuotaKey = "max_dispatcher_seats"
	QuotaSMSAlertsPerMonth  QuotaKey = "sms_alerts_per_month"
)

// OpCode represents operational actions subject to policy enforcement.
type OpCode string

const (
	OpCreateBooking     OpCode = "CREATE_BOOKING"
	OpCreateDispatch    OpCode = "CREATE_DISPATCH"
	OpCreateVehicle     OpCode = "CREATE_VEHICLE"
	OpCreateDriver      OpCode = "CREATE_DRIVER"
	OpIngestTelemetry   OpCode = "INGEST_TELEMETRY"
	OpCompleteStop      OpCode = "COMPLETE_STOP"
	OpCompleteTrip      OpCode = "COMPLETE_TRIP"
	OpIssueInvoice      OpCode = "ISSUE_INVOICE"
	OpExecuteSettlement OpCode = "EXECUTE_SETTLEMENT"
	OpReadResource      OpCode = "READ_RESOURCE"
)

// SubscriptionStatus represents the commercial lifecycle state.
type SubscriptionStatus string

const (
	SubTrial                   SubscriptionStatus = "TRIAL"
	SubActive                  SubscriptionStatus = "ACTIVE"
	SubPastDue                 SubscriptionStatus = "PAST_DUE"
	SubGrace                   SubscriptionStatus = "GRACE"
	SubReadOnly                SubscriptionStatus = "READ_ONLY"
	SubAccountClosed           SubscriptionStatus = "ACCOUNT_CLOSED"
	SubOperationallyTerminated SubscriptionStatus = "OPERATIONALLY_TERMINATED"
)

// PlanID represents standard subscription tiers.
type PlanID string

const (
	PlanStarter    PlanID = "STARTER"
	PlanGrowth     PlanID = "GROWTH"
	PlanScale      PlanID = "SCALE"
	PlanEnterprise PlanID = "ENTERPRISE"
)

// Standard Domain Errors
var (
	ErrSubscriptionNotFound = errors.New("subscription not found for tenant")
	ErrPlanNotFound         = errors.New("subscription plan not found")
	ErrFeatureDisabled      = errors.New("feature not enabled for current subscription tier")
	ErrQuotaExceeded        = errors.New("quota limit exceeded for current billing period")
	ErrOperationBlocked     = errors.New("operation blocked due to subscription status")
	ErrInvalidPeriod        = errors.New("invalid billing period")
	ErrReservationNotFound  = errors.New("quota reservation not found")
)
