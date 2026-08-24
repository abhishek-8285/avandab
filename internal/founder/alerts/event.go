package alerts

import "time"

type Priority string

const (
	PriorityCritical Priority = "CRITICAL"
	PriorityHigh     Priority = "HIGH"
	PriorityMedium   Priority = "MEDIUM"
	PriorityLow      Priority = "LOW"
)

type Category string

const (
	CategorySystem          Category = "SYSTEM"
	CategoryRevenue         Category = "REVENUE"
	CategoryChurnRisk       Category = "CHURN_RISK"
	CategorySecurity        Category = "SECURITY"
	CategoryProductUsage    Category = "PRODUCT_USAGE"
	CategoryActivation      Category = "ACTIVATION"
	CategoryTrialMonitoring Category = "TRIAL_MONITORING"
	CategoryPayment         Category = "PAYMENT"
	CategoryCustomerSuccess Category = "CUSTOMER_SUCCESS"
	CategoryFuel            Category = "FUEL"
	CategorySafety          Category = "SAFETY"
)

type AlertEvent struct {
	ID        string                 `json:"id"`
	Category  Category               `json:"category"`
	Priority  Priority               `json:"priority"`
	Title     string                 `json:"title"`
	Summary   string                 `json:"summary"`
	CompanyID string                 `json:"company_id,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}
