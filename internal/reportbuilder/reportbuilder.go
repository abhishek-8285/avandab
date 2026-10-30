package reportbuilder

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrNotFound              = errors.New("report or schedule not found")
	ErrInvalidInput          = errors.New("invalid report builder input")
	ErrUnauthorized          = errors.New("unauthorized report action")
	ErrSystemReportImmutable = errors.New("system reports cannot be modified or deleted")
	ErrInvalidDataset        = errors.New("unsupported fleet dataset")
	ErrInvalidColumn         = errors.New("column not allowed for dataset")
	ErrInvalidAggregation    = errors.New("invalid aggregation function or column")
	ErrInvalidFilter         = errors.New("invalid filter configuration")
)

type DatasetType string

const (
	DatasetTrips        DatasetType = "TRIPS"
	DatasetVehicles     DatasetType = "VEHICLES"
	DatasetDrivers      DatasetType = "DRIVERS"
	DatasetFuel         DatasetType = "FUEL"
	DatasetFASTag       DatasetType = "FASTAG"
	DatasetTyres        DatasetType = "TYRES"
	DatasetInventory    DatasetType = "INVENTORY"
	DatasetFreightAudit DatasetType = "FREIGHT_AUDIT"
	DatasetExpenses     DatasetType = "EXPENSES"
	DatasetInvoices     DatasetType = "INVOICES"
)

type FilterOperator string

const (
	OpEq       FilterOperator = "EQ"
	OpNeq      FilterOperator = "NEQ"
	OpGt       FilterOperator = "GT"
	OpGte      FilterOperator = "GTE"
	OpLt       FilterOperator = "LT"
	OpLte      FilterOperator = "LTE"
	OpContains FilterOperator = "CONTAINS"
	OpIn       FilterOperator = "IN"
	OpBetween  FilterOperator = "BETWEEN"
)

type AggregationFunc string

const (
	AggCount AggregationFunc = "COUNT"
	AggSum   AggregationFunc = "SUM"
	AggAvg   AggregationFunc = "AVG"
	AggMin   AggregationFunc = "MIN"
	AggMax   AggregationFunc = "MAX"
)

type ColumnMeta struct {
	Name       string `json:"name"`
	Label      string `json:"label"`
	Type       string `json:"type"` // "string", "number", "date", "boolean"
	Aggregable bool   `json:"aggregable"`
	Filterable bool   `json:"filterable"`
}

type DatasetMeta struct {
	Type        DatasetType  `json:"type"`
	Label       string       `json:"label"`
	Description string       `json:"description"`
	TableName   string       `json:"table_name"`
	Columns     []ColumnMeta `json:"columns"`
}

type ReportFilter struct {
	Column   string         `json:"column"`
	Operator FilterOperator `json:"operator"`
	Value    interface{}    `json:"value"`
	ValueTo  interface{}    `json:"value_to,omitempty"` // For BETWEEN
}

type ReportAggregation struct {
	Function AggregationFunc `json:"function"`
	Column   string          `json:"column"`
	Alias    string          `json:"alias"`
}

type CustomReport struct {
	ID              string              `json:"id"`
	TenantID        string              `json:"tenant_id"`
	Name            string              `json:"name"`
	Description     string              `json:"description"`
	Dataset         DatasetType         `json:"dataset"`
	SelectedColumns []string            `json:"selected_columns"`
	Filters         []ReportFilter      `json:"filters"`
	GroupBy         string              `json:"group_by"`
	Aggregations    []ReportAggregation `json:"aggregations"`
	SortBy          string              `json:"sort_by"`
	SortDirection   string              `json:"sort_direction"` // ASC or DESC
	ChartType       string              `json:"chart_type"`     // TABLE, BAR, LINE, PIE
	IsSystem        bool                `json:"is_system"`
	CreatedBy       string              `json:"created_by"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

type CreateReportInput struct {
	Name            string              `json:"name"`
	Description     string              `json:"description"`
	Dataset         DatasetType         `json:"dataset"`
	SelectedColumns []string            `json:"selected_columns"`
	Filters         []ReportFilter      `json:"filters"`
	GroupBy         string              `json:"group_by"`
	Aggregations    []ReportAggregation `json:"aggregations"`
	SortBy          string              `json:"sort_by"`
	SortDirection   string              `json:"sort_direction"`
	ChartType       string              `json:"chart_type"`
}

func (in *CreateReportInput) Validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("report name is required")
	}
	catalog := GetDatasetCatalog()
	meta, ok := catalog[in.Dataset]
	if !ok {
		return ErrInvalidDataset
	}

	validCols := make(map[string]ColumnMeta)
	for _, c := range meta.Columns {
		validCols[c.Name] = c
	}

	for _, col := range in.SelectedColumns {
		if _, exists := validCols[col]; !exists {
			return ErrInvalidColumn
		}
	}

	for _, f := range in.Filters {
		colMeta, exists := validCols[f.Column]
		if !exists || !colMeta.Filterable {
			return ErrInvalidFilter
		}
	}

	if in.GroupBy != "" {
		if _, exists := validCols[in.GroupBy]; !exists {
			return ErrInvalidColumn
		}
	}

	for _, agg := range in.Aggregations {
		if agg.Function != AggCount {
			colMeta, exists := validCols[agg.Column]
			if !exists || !colMeta.Aggregable {
				return ErrInvalidAggregation
			}
		}
	}

	if in.SortBy != "" {
		if _, exists := validCols[in.SortBy]; !exists {
			return ErrInvalidColumn
		}
	}

	if in.SortDirection != "" {
		upper := strings.ToUpper(in.SortDirection)
		if upper != "ASC" && upper != "DESC" {
			return errors.New("sort_direction must be ASC or DESC")
		}
	}

	return nil
}

type UpdateReportInput struct {
	Name            *string             `json:"name"`
	Description     *string             `json:"description"`
	SelectedColumns []string            `json:"selected_columns"`
	Filters         []ReportFilter      `json:"filters"`
	GroupBy         *string             `json:"group_by"`
	Aggregations    []ReportAggregation `json:"aggregations"`
	SortBy          *string             `json:"sort_by"`
	SortDirection   *string             `json:"sort_direction"`
	ChartType       *string             `json:"chart_type"`
}

type ListReportsFilter struct {
	Dataset  *DatasetType
	IsSystem *bool
	Search   string
	Limit    int
	Offset   int
}

type ReportSchedule struct {
	ID             string     `json:"id"`
	TenantID       string     `json:"tenant_id"`
	ReportID       string     `json:"report_id"`
	ReportName     string     `json:"report_name,omitempty"`
	Name           string     `json:"name"`
	Frequency      string     `json:"frequency"` // DAILY, WEEKLY, MONTHLY
	CronExpression string     `json:"cron_expression"`
	Format         string     `json:"format"`  // CSV, JSON, PDF
	Channel        string     `json:"channel"` // EMAIL, WEBHOOK
	Recipients     []string   `json:"recipients"`
	IsActive       bool       `json:"is_active"`
	LastRunAt      *time.Time `json:"last_run_at"`
	NextRunAt      *time.Time `json:"next_run_at"`
	LastStatus     string     `json:"last_status"`
	LastError      string     `json:"last_error"`
	CreatedBy      string     `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type CreateScheduleInput struct {
	ReportID       string   `json:"report_id"`
	Name           string   `json:"name"`
	Frequency      string   `json:"frequency"` // DAILY, WEEKLY, MONTHLY
	CronExpression string   `json:"cron_expression"`
	Format         string   `json:"format"`  // CSV, JSON, PDF
	Channel        string   `json:"channel"` // EMAIL, WEBHOOK
	Recipients     []string `json:"recipients"`
}

func (in *CreateScheduleInput) Validate() error {
	if strings.TrimSpace(in.ReportID) == "" {
		return errors.New("report_id is required")
	}
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("schedule name is required")
	}
	freq := strings.ToUpper(strings.TrimSpace(in.Frequency))
	if freq != "DAILY" && freq != "WEEKLY" && freq != "MONTHLY" {
		return errors.New("frequency must be DAILY, WEEKLY, or MONTHLY")
	}
	format := strings.ToUpper(strings.TrimSpace(in.Format))
	if format != "CSV" && format != "JSON" && format != "PDF" {
		return errors.New("format must be CSV, JSON, or PDF")
	}
	channel := strings.ToUpper(strings.TrimSpace(in.Channel))
	if channel != "EMAIL" && channel != "WEBHOOK" {
		return errors.New("channel must be EMAIL or WEBHOOK")
	}
	if len(in.Recipients) == 0 {
		return errors.New("at least one recipient is required")
	}
	return nil
}

type UpdateScheduleInput struct {
	Name           *string  `json:"name"`
	Frequency      *string  `json:"frequency"`
	CronExpression *string  `json:"cron_expression"`
	Format         *string  `json:"format"`
	Channel        *string  `json:"channel"`
	Recipients     []string `json:"recipients"`
	IsActive       *bool    `json:"is_active"`
}

type ListSchedulesFilter struct {
	ReportID *string
	IsActive *bool
	Limit    int
	Offset   int
}

type ReportExecution struct {
	ID                  string     `json:"id"`
	TenantID            string     `json:"tenant_id"`
	ReportID            string     `json:"report_id"`
	ReportName          string     `json:"report_name,omitempty"`
	ScheduleID          *string    `json:"schedule_id,omitempty"`
	Format              string     `json:"format"`
	Status              string     `json:"status"` // PENDING, RUNNING, COMPLETED, FAILED
	RowCount            int        `json:"row_count"`
	FileSizeBytes       int64      `json:"file_size_bytes"`
	ExecutionDurationMs int64      `json:"execution_duration_ms"`
	ErrorMessage        string     `json:"error_message"`
	TriggeredBy         string     `json:"triggered_by"` // MANUAL, SCHEDULE
	CreatedAt           time.Time  `json:"created_at"`
	CompletedAt         *time.Time `json:"completed_at"`
}

type ListExecutionsFilter struct {
	ReportID *string
	Status   *string
	Limit    int
	Offset   int
}

type QueryResult struct {
	Columns             []string                 `json:"columns"`
	Rows                []map[string]interface{} `json:"rows"`
	TotalRows           int                      `json:"total_rows"`
	ExecutionDurationMs int64                    `json:"execution_duration_ms"`
}

type RunReportInput struct {
	Format       string         `json:"format"` // JSON, CSV, PDF
	Limit        int            `json:"limit"`
	Offset       int            `json:"offset"`
	AdHocFilters []ReportFilter `json:"ad_hoc_filters,omitempty"`
}

func GetDatasetCatalog() map[DatasetType]DatasetMeta {
	return map[DatasetType]DatasetMeta{
		DatasetTrips: {
			Type:        DatasetTrips,
			Label:       "Trips",
			Description: "Operational trips, dispatches, margins, fuel consumed, and settlement amounts",
			TableName:   "trips",
			Columns: []ColumnMeta{
				{Name: "id", Label: "Trip ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "trip_number", Label: "Trip Number", Type: "string", Aggregable: false, Filterable: true},
				{Name: "status", Label: "Status", Type: "string", Aggregable: false, Filterable: true},
				{Name: "booking_id", Label: "Booking ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "driver_id", Label: "Driver ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "vehicle_id", Label: "Vehicle ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "route_id", Label: "Route ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "departure_time", Label: "Departure Time", Type: "date", Aggregable: false, Filterable: true},
				{Name: "arrival_time", Label: "Arrival Time", Type: "date", Aggregable: false, Filterable: true},
				{Name: "start_odometer", Label: "Start Odometer", Type: "number", Aggregable: true, Filterable: true},
				{Name: "close_odometer", Label: "Close Odometer", Type: "number", Aggregable: true, Filterable: true},
				{Name: "fuel_consumed_liters", Label: "Fuel Consumed (L)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "toll_costs", Label: "Toll Costs (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "estimated_margin", Label: "Estimated Margin (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "final_settlement_amount", Label: "Final Settlement (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "created_at", Label: "Created At", Type: "date", Aggregable: false, Filterable: true},
				{Name: "updated_at", Label: "Updated At", Type: "date", Aggregable: false, Filterable: true},
			},
		},
		DatasetVehicles: {
			Type:        DatasetVehicles,
			Label:       "Vehicles",
			Description: "Fleet vehicle master, registration numbers, types, capacity, compliance dates, and status",
			TableName:   "vehicles",
			Columns: []ColumnMeta{
				{Name: "id", Label: "Vehicle ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "registration_number", Label: "Registration Number", Type: "string", Aggregable: false, Filterable: true},
				{Name: "vehicle_number", Label: "Vehicle Number", Type: "string", Aggregable: false, Filterable: true},
				{Name: "vehicle_type", Label: "Vehicle Type", Type: "string", Aggregable: false, Filterable: true},
				{Name: "capacity", Label: "Capacity (Tons/Kg)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "fuel_type", Label: "Fuel Type", Type: "string", Aggregable: false, Filterable: true},
				{Name: "insurance_expiry", Label: "Insurance Expiry", Type: "date", Aggregable: false, Filterable: true},
				{Name: "fitness_expiry", Label: "Fitness Expiry", Type: "date", Aggregable: false, Filterable: true},
				{Name: "permit_expiry", Label: "Permit Expiry", Type: "date", Aggregable: false, Filterable: true},
				{Name: "status", Label: "Status", Type: "string", Aggregable: false, Filterable: true},
				{Name: "current_mileage", Label: "Current Mileage (km)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "created_at", Label: "Created At", Type: "date", Aggregable: false, Filterable: true},
				{Name: "updated_at", Label: "Updated At", Type: "date", Aggregable: false, Filterable: true},
			},
		},
		DatasetDrivers: {
			Type:        DatasetDrivers,
			Label:       "Drivers",
			Description: "Driver profiles, mobile numbers, license numbers, duty status, and performance rating",
			TableName:   "drivers",
			Columns: []ColumnMeta{
				{Name: "id", Label: "Driver ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "name", Label: "Driver Name", Type: "string", Aggregable: false, Filterable: true},
				{Name: "phone", Label: "Phone Number", Type: "string", Aggregable: false, Filterable: true},
				{Name: "license_number", Label: "License Number", Type: "string", Aggregable: false, Filterable: true},
				{Name: "license_expiry", Label: "License Expiry", Type: "date", Aggregable: false, Filterable: true},
				{Name: "status", Label: "Status", Type: "string", Aggregable: false, Filterable: true},
				{Name: "duty_status", Label: "Duty Status", Type: "string", Aggregable: false, Filterable: true},
				{Name: "rating", Label: "Rating", Type: "number", Aggregable: true, Filterable: true},
				{Name: "created_at", Label: "Created At", Type: "date", Aggregable: false, Filterable: true},
				{Name: "updated_at", Label: "Updated At", Type: "date", Aggregable: false, Filterable: true},
			},
		},
		DatasetFuel: {
			Type:        DatasetFuel,
			Label:       "Fuel Transactions",
			Description: "Fuel card transactions, station locations, volume litres, rates, and reconciliation status",
			TableName:   "fuel_card_transactions",
			Columns: []ColumnMeta{
				{Name: "id", Label: "Transaction ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "fuel_card_id", Label: "Card ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "external_txn_id", Label: "External Txn Ref", Type: "string", Aggregable: false, Filterable: true},
				{Name: "txn_time", Label: "Transaction Time", Type: "date", Aggregable: false, Filterable: true},
				{Name: "fuel_station_name", Label: "Station Name", Type: "string", Aggregable: false, Filterable: true},
				{Name: "fuel_station_city", Label: "Station City", Type: "string", Aggregable: false, Filterable: true},
				{Name: "fuel_type", Label: "Fuel Type", Type: "string", Aggregable: false, Filterable: true},
				{Name: "volume_litres", Label: "Volume (L)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "rate_per_litre", Label: "Rate / Litre (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "total_amount", Label: "Total Amount (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "odometer_reported", Label: "Reported Odometer", Type: "number", Aggregable: true, Filterable: true},
				{Name: "reconciliation_status", Label: "Reconciliation Status", Type: "string", Aggregable: false, Filterable: true},
				{Name: "notes", Label: "Notes", Type: "string", Aggregable: false, Filterable: true},
				{Name: "created_at", Label: "Created At", Type: "date", Aggregable: false, Filterable: true},
			},
		},
		DatasetFASTag: {
			Type:        DatasetFASTag,
			Label:       "FASTag Tolls",
			Description: "FASTag toll plaza transactions, vehicle tags, deductions, and trip reconciliation",
			TableName:   "fastag_transactions",
			Columns: []ColumnMeta{
				{Name: "id", Label: "Transaction ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "tag_id", Label: "FASTag ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "vehicle_id", Label: "Vehicle ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "vehicle_number", Label: "Vehicle Number", Type: "string", Aggregable: false, Filterable: true},
				{Name: "trip_id", Label: "Trip ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "plaza_id", Label: "Plaza ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "plaza_name", Label: "Plaza Name", Type: "string", Aggregable: false, Filterable: true},
				{Name: "amount", Label: "Toll Amount (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "txn_timestamp", Label: "Timestamp", Type: "date", Aggregable: false, Filterable: true},
				{Name: "status", Label: "Status", Type: "string", Aggregable: false, Filterable: true},
				{Name: "source", Label: "Source", Type: "string", Aggregable: false, Filterable: true},
				{Name: "reconciled", Label: "Reconciled", Type: "boolean", Aggregable: false, Filterable: true},
				{Name: "created_at", Label: "Created At", Type: "date", Aggregable: false, Filterable: true},
			},
		},
		DatasetTyres: {
			Type:        DatasetTyres,
			Label:       "Tyre Management",
			Description: "Tyre serial tracking, tread depth, retreads, axle positions, and cost per KM",
			TableName:   "tyres",
			Columns: []ColumnMeta{
				{Name: "id", Label: "Tyre ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "serial_number", Label: "Serial Number", Type: "string", Aggregable: false, Filterable: true},
				{Name: "brand", Label: "Brand", Type: "string", Aggregable: false, Filterable: true},
				{Name: "model", Label: "Model", Type: "string", Aggregable: false, Filterable: true},
				{Name: "size", Label: "Size", Type: "string", Aggregable: false, Filterable: true},
				{Name: "tyre_type", Label: "Tyre Type", Type: "string", Aggregable: false, Filterable: true},
				{Name: "status", Label: "Status", Type: "string", Aggregable: false, Filterable: true},
				{Name: "vehicle_id", Label: "Vehicle ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "axle_position", Label: "Axle Position", Type: "string", Aggregable: false, Filterable: true},
				{Name: "mount_odometer_km", Label: "Mount Odometer (km)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "initial_tread_depth_mm", Label: "Initial Tread (mm)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "current_tread_depth_mm", Label: "Current Tread (mm)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "purchase_cost", Label: "Purchase Cost (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "retread_count", Label: "Retread Count", Type: "number", Aggregable: true, Filterable: true},
				{Name: "total_km_run", Label: "Total KM Run", Type: "number", Aggregable: true, Filterable: true},
				{Name: "created_at", Label: "Created At", Type: "date", Aggregable: false, Filterable: true},
				{Name: "updated_at", Label: "Updated At", Type: "date", Aggregable: false, Filterable: true},
			},
		},
		DatasetInventory: {
			Type:        DatasetInventory,
			Label:       "Parts & Inventory",
			Description: "Spare parts catalogue, stock thresholds, unit costs, and categories",
			TableName:   "parts",
			Columns: []ColumnMeta{
				{Name: "id", Label: "Part ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "part_number", Label: "Part Number", Type: "string", Aggregable: false, Filterable: true},
				{Name: "name", Label: "Part Name", Type: "string", Aggregable: false, Filterable: true},
				{Name: "category", Label: "Category", Type: "string", Aggregable: false, Filterable: true},
				{Name: "uom", Label: "Unit of Measure", Type: "string", Aggregable: false, Filterable: true},
				{Name: "min_stock_level", Label: "Min Stock Level", Type: "number", Aggregable: true, Filterable: true},
				{Name: "reorder_quantity", Label: "Reorder Quantity", Type: "number", Aggregable: true, Filterable: true},
				{Name: "cost_price", Label: "Cost Price (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "selling_price", Label: "Selling Price (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "oem_brand", Label: "OEM Brand", Type: "string", Aggregable: false, Filterable: true},
				{Name: "compatible_models", Label: "Compatible Models", Type: "string", Aggregable: false, Filterable: true},
				{Name: "is_active", Label: "Is Active", Type: "boolean", Aggregable: false, Filterable: true},
				{Name: "created_at", Label: "Created At", Type: "date", Aggregable: false, Filterable: true},
				{Name: "updated_at", Label: "Updated At", Type: "date", Aggregable: false, Filterable: true},
			},
		},
		DatasetFreightAudit: {
			Type:        DatasetFreightAudit,
			Label:       "Freight Audit & Pay",
			Description: "3-way matched freight audits, billed vs audited variances, and approved deductions",
			TableName:   "freight_audits",
			Columns: []ColumnMeta{
				{Name: "id", Label: "Audit ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "audit_number", Label: "Audit Number", Type: "string", Aggregable: false, Filterable: true},
				{Name: "trip_id", Label: "Trip ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "invoice_id", Label: "Invoice ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "booking_id", Label: "Booking ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "vendor_name", Label: "Vendor Name", Type: "string", Aggregable: false, Filterable: true},
				{Name: "vehicle_number", Label: "Vehicle Number", Type: "string", Aggregable: false, Filterable: true},
				{Name: "status", Label: "Status", Type: "string", Aggregable: false, Filterable: true},
				{Name: "billing_basis", Label: "Billing Basis", Type: "string", Aggregable: false, Filterable: true},
				{Name: "billed_total", Label: "Billed Total (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "audited_total", Label: "Audited Total (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "discrepancy_amount", Label: "Discrepancy (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "net_payable_amount", Label: "Net Payable (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "created_at", Label: "Created At", Type: "date", Aggregable: false, Filterable: true},
				{Name: "updated_at", Label: "Updated At", Type: "date", Aggregable: false, Filterable: true},
			},
		},
		DatasetExpenses: {
			Type:        DatasetExpenses,
			Label:       "Driver Expenses",
			Description: "Trip expenses, en-route driver claims, fuel, toll, food, repairs, and approvals",
			TableName:   "driver_expenses",
			Columns: []ColumnMeta{
				{Name: "id", Label: "Expense ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "trip_id", Label: "Trip ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "driver_id", Label: "Driver ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "expense_type", Label: "Expense Type", Type: "string", Aggregable: false, Filterable: true},
				{Name: "amount", Label: "Amount (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "description", Label: "Description", Type: "string", Aggregable: false, Filterable: true},
				{Name: "approved", Label: "Approved", Type: "boolean", Aggregable: false, Filterable: true},
				{Name: "created_at", Label: "Created At", Type: "date", Aggregable: false, Filterable: true},
			},
		},
		DatasetInvoices: {
			Type:        DatasetInvoices,
			Label:       "Invoices & Billing",
			Description: "Customer invoices, billing subtotal, taxes, discounts, and payment status",
			TableName:   "invoices",
			Columns: []ColumnMeta{
				{Name: "id", Label: "Invoice ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "invoice_number", Label: "Invoice Number", Type: "string", Aggregable: false, Filterable: true},
				{Name: "booking_id", Label: "Booking ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "customer_id", Label: "Customer ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "trip_id", Label: "Trip ID", Type: "string", Aggregable: false, Filterable: true},
				{Name: "subtotal", Label: "Subtotal (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "tax", Label: "Tax (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "discount", Label: "Discount (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "total", Label: "Total (₹)", Type: "number", Aggregable: true, Filterable: true},
				{Name: "payment_status", Label: "Payment Status", Type: "string", Aggregable: false, Filterable: true},
				{Name: "created_at", Label: "Created At", Type: "date", Aggregable: false, Filterable: true},
				{Name: "updated_at", Label: "Updated At", Type: "date", Aggregable: false, Filterable: true},
			},
		},
	}
}

func GetDefaultSystemReports() []CustomReport {
	now := time.Now().UTC()
	return []CustomReport{
		{
			ID:              "sys-rep-trip-profitability",
			Name:            "Trip Profitability & Margin Summary",
			Description:     "Analyzes estimated margins, fuel consumed, and toll expenses across completed trips",
			Dataset:         DatasetTrips,
			SelectedColumns: []string{"trip_number", "status", "estimated_margin", "toll_costs", "fuel_consumed_liters", "final_settlement_amount"},
			SortBy:          "created_at",
			SortDirection:   "DESC",
			ChartType:       "TABLE",
			IsSystem:        true,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		{
			ID:              "sys-rep-vehicle-compliance",
			Name:            "Vehicle Fleet Compliance & Health",
			Description:     "Monitors fitness, insurance, and permit expiries across all fleet vehicles",
			Dataset:         DatasetVehicles,
			SelectedColumns: []string{"registration_number", "vehicle_type", "capacity", "fuel_type", "fitness_expiry", "insurance_expiry", "status"},
			SortBy:          "fitness_expiry",
			SortDirection:   "ASC",
			ChartType:       "TABLE",
			IsSystem:        true,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		{
			ID:              "sys-rep-tyre-performance",
			Name:            "Tyre Mileage & Tread Wear Analysis",
			Description:     "Tracks tyre wear rate, total distance run, and remaining tread depth",
			Dataset:         DatasetTyres,
			SelectedColumns: []string{"serial_number", "brand", "size", "status", "total_km_run", "current_tread_depth_mm", "retread_count"},
			SortBy:          "current_tread_depth_mm",
			SortDirection:   "ASC",
			ChartType:       "BAR",
			IsSystem:        true,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		{
			ID:              "sys-rep-inventory-reorder",
			Name:            "Parts Low Stock & Reorder Alert",
			Description:     "Identifies spare parts where available quantities are at or below reorder threshold",
			Dataset:         DatasetInventory,
			SelectedColumns: []string{"part_number", "name", "category", "uom", "min_stock_level", "reorder_quantity", "cost_price"},
			SortBy:          "min_stock_level",
			SortDirection:   "DESC",
			ChartType:       "TABLE",
			IsSystem:        true,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		{
			ID:              "sys-rep-freight-audit",
			Name:            "Freight Audit Discrepancies & Variances",
			Description:     "Summary of billed vs audited variances and pending deduction approvals",
			Dataset:         DatasetFreightAudit,
			SelectedColumns: []string{"audit_number", "vendor_name", "vehicle_number", "billed_total", "audited_total", "discrepancy_amount", "status"},
			SortBy:          "discrepancy_amount",
			SortDirection:   "DESC",
			ChartType:       "TABLE",
			IsSystem:        true,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
	}
}
