package reportbuilder

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	schema := `
	CREATE TABLE tenants (id TEXT PRIMARY KEY, name TEXT);
	INSERT INTO tenants (id, name) VALUES ('tenant-1', 'Acme Logistics'), ('tenant-2', 'Beta Transport');

	CREATE TABLE custom_reports (
		id               TEXT PRIMARY KEY,
		tenant_id        TEXT NOT NULL,
		name             TEXT NOT NULL,
		description      TEXT NOT NULL DEFAULT '',
		dataset          TEXT NOT NULL,
		selected_columns TEXT NOT NULL DEFAULT '[]',
		filters          TEXT NOT NULL DEFAULT '[]',
		group_by         TEXT NOT NULL DEFAULT '',
		aggregations     TEXT NOT NULL DEFAULT '[]',
		sort_by          TEXT NOT NULL DEFAULT '',
		sort_direction   TEXT NOT NULL DEFAULT 'DESC',
		chart_type       TEXT NOT NULL DEFAULT 'TABLE',
		is_system        INTEGER NOT NULL DEFAULT 0,
		created_by       TEXT NOT NULL DEFAULT '',
		created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE report_schedules (
		id              TEXT PRIMARY KEY,
		tenant_id       TEXT NOT NULL,
		report_id       TEXT NOT NULL,
		name            TEXT NOT NULL,
		frequency       TEXT NOT NULL,
		cron_expression TEXT NOT NULL DEFAULT '',
		format          TEXT NOT NULL DEFAULT 'CSV',
		channel         TEXT NOT NULL DEFAULT 'EMAIL',
		recipients      TEXT NOT NULL DEFAULT '[]',
		is_active       INTEGER NOT NULL DEFAULT 1,
		last_run_at     DATETIME,
		next_run_at     DATETIME,
		last_status     TEXT NOT NULL DEFAULT '',
		last_error      TEXT NOT NULL DEFAULT '',
		created_by      TEXT NOT NULL DEFAULT '',
		created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE report_executions (
		id                    TEXT PRIMARY KEY,
		tenant_id             TEXT NOT NULL,
		report_id             TEXT NOT NULL,
		schedule_id           TEXT,
		format                TEXT NOT NULL DEFAULT 'CSV',
		status                TEXT NOT NULL DEFAULT 'PENDING',
		row_count             INTEGER NOT NULL DEFAULT 0,
		file_size_bytes       INTEGER NOT NULL DEFAULT 0,
		execution_duration_ms INTEGER NOT NULL DEFAULT 0,
		error_message         TEXT NOT NULL DEFAULT '',
		triggered_by          TEXT NOT NULL DEFAULT 'MANUAL',
		created_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		completed_at          DATETIME
	);

	-- Seed test datasets
	CREATE TABLE trips (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		trip_number TEXT NOT NULL,
		booking_id TEXT,
		driver_id TEXT,
		vehicle_id TEXT,
		route_id TEXT NOT NULL,
		departure_time DATETIME NOT NULL,
		arrival_time DATETIME,
		status TEXT NOT NULL,
		start_odometer REAL,
		close_odometer REAL,
		fuel_consumed_liters REAL NOT NULL DEFAULT 0,
		toll_costs REAL NOT NULL DEFAULT 0,
		estimated_margin REAL NOT NULL DEFAULT 0,
		final_settlement_amount REAL NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	INSERT INTO trips (id, tenant_id, trip_number, route_id, departure_time, status, estimated_margin, toll_costs, fuel_consumed_liters) VALUES
	('tr-1', 'tenant-1', 'TRP-001', 'r-1', '2026-10-01 10:00:00', 'completed', 4500.0, 800.0, 120.0),
	('tr-2', 'tenant-1', 'TRP-002', 'r-1', '2026-10-02 11:00:00', 'completed', 6200.0, 1100.0, 150.0),
	('tr-3', 'tenant-1', 'TRP-003', 'r-2', '2026-10-03 09:00:00', 'started', 0.0, 200.0, 40.0),
	('tr-4', 'tenant-2', 'TRP-999', 'r-9', '2026-10-01 10:00:00', 'completed', 9999.0, 500.0, 90.0);

	CREATE TABLE vehicles (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		registration_number TEXT NOT NULL,
		vehicle_number TEXT NOT NULL,
		vehicle_type TEXT NOT NULL,
		capacity INTEGER NOT NULL,
		fuel_type TEXT NOT NULL,
		insurance_expiry DATE NOT NULL,
		fitness_expiry DATE NOT NULL,
		permit_expiry DATE NOT NULL,
		status TEXT NOT NULL,
		current_mileage REAL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status) VALUES
	('v-1', 'tenant-1', 'MH12AB1234', 'TRK-01', 'truck', 16000, 'diesel', '2027-01-01', '2026-12-01', '2027-06-01', 'available'),
	('v-2', 'tenant-1', 'MH12CD5678', 'TRK-02', 'tempo', 4000, 'cng', '2026-11-15', '2026-10-25', '2026-12-31', 'running');
	`

	_, err = db.Exec(schema)
	require.NoError(t, err)
	return db
}

func TestDatasetCatalog_Integrity(t *testing.T) {
	catalog := GetDatasetCatalog()
	expectedDatasets := []DatasetType{
		DatasetTrips,
		DatasetVehicles,
		DatasetDrivers,
		DatasetFuel,
		DatasetFASTag,
		DatasetTyres,
		DatasetInventory,
		DatasetFreightAudit,
		DatasetExpenses,
		DatasetInvoices,
	}

	assert.Len(t, catalog, 10)
	for _, dt := range expectedDatasets {
		meta, exists := catalog[dt]
		assert.True(t, exists, "missing dataset %s in catalog", dt)
		assert.NotEmpty(t, meta.Label)
		assert.NotEmpty(t, meta.Description)
		assert.NotEmpty(t, meta.TableName)
		assert.NotEmpty(t, meta.Columns)
	}
}

func TestCustomReport_Validation(t *testing.T) {
	// 1. Missing name
	in := CreateReportInput{
		Dataset: DatasetTrips,
	}
	assert.Error(t, in.Validate())

	// 2. Unsupported dataset
	in = CreateReportInput{
		Name:    "Invalid DS",
		Dataset: "UNKNOWN_DATASET",
	}
	assert.ErrorIs(t, in.Validate(), ErrInvalidDataset)

	// 3. Invalid column in projection
	in = CreateReportInput{
		Name:            "Bad Col",
		Dataset:         DatasetTrips,
		SelectedColumns: []string{"non_existent_column"},
	}
	assert.ErrorIs(t, in.Validate(), ErrInvalidColumn)

	// 4. Invalid aggregation on non-aggregable column
	in = CreateReportInput{
		Name:    "Bad Agg",
		Dataset: DatasetTrips,
		Aggregations: []ReportAggregation{
			{Function: AggSum, Column: "trip_number"},
		},
	}
	assert.ErrorIs(t, in.Validate(), ErrInvalidAggregation)

	// 5. Valid report
	in = CreateReportInput{
		Name:            "Trip Profitability",
		Dataset:         DatasetTrips,
		SelectedColumns: []string{"trip_number", "status", "estimated_margin"},
		SortBy:          "estimated_margin",
		SortDirection:   "DESC",
		ChartType:       "TABLE",
	}
	assert.NoError(t, in.Validate())
}

func TestReportBuilder_CRUD(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewService(NewRepository(db))
	ctx := context.Background()

	// 1. Create Report
	in := CreateReportInput{
		Name:            "High Margin Trips",
		Description:     "Trips with margin > 5000",
		Dataset:         DatasetTrips,
		SelectedColumns: []string{"trip_number", "status", "estimated_margin"},
		Filters: []ReportFilter{
			{Column: "status", Operator: OpEq, Value: "completed"},
			{Column: "estimated_margin", Operator: OpGt, Value: 5000.0},
		},
		SortBy:        "estimated_margin",
		SortDirection: "DESC",
		ChartType:     "TABLE",
	}

	rep, err := svc.CreateReport(ctx, "tenant-1", "user-123", in)
	require.NoError(t, err)
	assert.NotEmpty(t, rep.ID)
	assert.Equal(t, "High Margin Trips", rep.Name)
	assert.Equal(t, "tenant-1", rep.TenantID)

	// 2. Get Report
	fetched, err := svc.GetReport(ctx, "tenant-1", rep.ID)
	require.NoError(t, err)
	assert.Equal(t, rep.ID, fetched.ID)
	assert.Len(t, fetched.Filters, 2)

	// 3. List Reports (includes system defaults + created report)
	list, total, err := svc.ListReports(ctx, "tenant-1", ListReportsFilter{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, 1)
	assert.NotEmpty(t, list)

	// 4. Update Report
	newDesc := "Updated trip margin report"
	updated, err := svc.UpdateReport(ctx, "tenant-1", rep.ID, UpdateReportInput{
		Description: &newDesc,
	})
	require.NoError(t, err)
	assert.Equal(t, newDesc, updated.Description)

	// 5. Try updating immutable system report -> fails
	_, err = svc.UpdateReport(ctx, "tenant-1", "sys-rep-trip-profitability", UpdateReportInput{
		Description: &newDesc,
	})
	assert.ErrorIs(t, err, ErrSystemReportImmutable)

	// 6. Delete Report
	err = svc.DeleteReport(ctx, "tenant-1", rep.ID)
	require.NoError(t, err)

	_, err = svc.GetReport(ctx, "tenant-1", rep.ID)
	assert.ErrorIs(t, err, ErrNotFound)

	// 7. Try deleting system report -> fails
	err = svc.DeleteReport(ctx, "tenant-1", "sys-rep-trip-profitability")
	assert.ErrorIs(t, err, ErrSystemReportImmutable)
}

func TestDynamicQueryExecution_TripsAndMultiTenant(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewService(NewRepository(db))
	ctx := context.Background()

	// 1. Query raw columns with tenant isolation
	in := CreateReportInput{
		Name:            "All Completed Trips",
		Dataset:         DatasetTrips,
		SelectedColumns: []string{"trip_number", "status", "estimated_margin"},
		Filters: []ReportFilter{
			{Column: "status", Operator: OpEq, Value: "completed"},
		},
		SortBy:        "estimated_margin",
		SortDirection: "DESC",
	}

	rep, err := svc.CreateReport(ctx, "tenant-1", "u1", in)
	require.NoError(t, err)

	runRes, err := svc.RunReport(ctx, "tenant-1", rep.ID, RunReportInput{}, "MANUAL")
	require.NoError(t, err)
	assert.Equal(t, 2, runRes.RowCount, "Tenant-1 has exactly 2 completed trips (tenant-2 must be excluded)")
	assert.Equal(t, "TRP-002", runRes.QueryResult.Rows[0]["trip_number"])
	assert.Equal(t, 6200.0, runRes.QueryResult.Rows[0]["estimated_margin"])
	assert.Equal(t, "TRP-001", runRes.QueryResult.Rows[1]["trip_number"])

	// Verify Tenant-2 sees only their own trip
	runResT2, err := svc.RunAdHoc(ctx, "tenant-2", in, RunReportInput{})
	require.NoError(t, err)
	assert.Equal(t, 1, runResT2.RowCount)
	assert.Equal(t, "TRP-999", runResT2.QueryResult.Rows[0]["trip_number"])
}

func TestDynamicQueryExecution_AggregationsAndGroupBy(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewService(NewRepository(db))
	ctx := context.Background()

	in := CreateReportInput{
		Name:    "Trip Margins by Status",
		Dataset: DatasetTrips,
		GroupBy: "status",
		Aggregations: []ReportAggregation{
			{Function: AggCount, Column: "*", Alias: "trip_count"},
			{Function: AggSum, Column: "estimated_margin", Alias: "total_margin"},
			{Function: AggAvg, Column: "toll_costs", Alias: "avg_tolls"},
		},
		SortBy:        "status",
		SortDirection: "ASC",
	}

	rep, err := svc.CreateReport(ctx, "tenant-1", "u1", in)
	require.NoError(t, err)

	runRes, err := svc.RunReport(ctx, "tenant-1", rep.ID, RunReportInput{}, "MANUAL")
	require.NoError(t, err)
	assert.Equal(t, 2, runRes.RowCount) // 2 groups: "completed" and "started"

	foundCompleted := false
	for _, row := range runRes.QueryResult.Rows {
		if row["status"] == "completed" {
			foundCompleted = true
			assert.EqualValues(t, 2, row["trip_count"])
			assert.Equal(t, 10700.0, row["total_margin"]) // 4500 + 6200
			assert.Equal(t, 950.0, row["avg_tolls"])      // (800 + 1100) / 2
		}
	}
	assert.True(t, foundCompleted)
}

func TestDynamicQueryExecution_CSVExport(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewService(NewRepository(db))
	ctx := context.Background()

	in := CreateReportInput{
		Name:            "Vehicle Fleet CSV",
		Dataset:         DatasetVehicles,
		SelectedColumns: []string{"registration_number", "vehicle_type", "fuel_type", "capacity"},
		SortBy:          "registration_number",
		SortDirection:   "ASC",
	}

	rep, err := svc.CreateReport(ctx, "tenant-1", "u1", in)
	require.NoError(t, err)

	runRes, err := svc.RunReport(ctx, "tenant-1", rep.ID, RunReportInput{Format: "CSV"}, "MANUAL")
	require.NoError(t, err)
	assert.NotEmpty(t, runRes.CSVData)

	csvStr := string(runRes.CSVData)
	assert.Contains(t, csvStr, "registration_number,vehicle_type,fuel_type,capacity")
	assert.Contains(t, csvStr, "MH12AB1234,truck,diesel,16000")
	assert.Contains(t, csvStr, "MH12CD5678,tempo,cng,4000")
}

func TestScheduledDelivery_LifecycleAndTrigger(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewService(NewRepository(db))
	ctx := context.Background()

	// 1. Create schedule for system report
	schedIn := CreateScheduleInput{
		ReportID:   "sys-rep-trip-profitability",
		Name:       "Daily Morning Profitability Email",
		Frequency:  "DAILY",
		Format:     "CSV",
		Channel:    "EMAIL",
		Recipients: []string{"ops@fleet.local", "cfo@fleet.local"},
	}

	sched, err := svc.CreateSchedule(ctx, "tenant-1", "u1", schedIn)
	require.NoError(t, err)
	assert.NotEmpty(t, sched.ID)
	assert.True(t, sched.IsActive)
	assert.NotNil(t, sched.NextRunAt)

	// 2. Get schedule
	fetched, err := svc.GetSchedule(ctx, "tenant-1", sched.ID)
	require.NoError(t, err)
	assert.Equal(t, sched.ID, fetched.ID)
	assert.Equal(t, 2, len(fetched.Recipients))

	// 3. Trigger schedule
	runRes, err := svc.TriggerSchedule(ctx, "tenant-1", sched.ID)
	require.NoError(t, err)
	assert.NotNil(t, runRes)
	assert.Equal(t, "CSV", runRes.Format)

	// Verify schedule was updated with last status
	updatedSched, err := svc.GetSchedule(ctx, "tenant-1", sched.ID)
	require.NoError(t, err)
	assert.Equal(t, "SUCCESS", updatedSched.LastStatus)
	assert.NotNil(t, updatedSched.LastRunAt)

	// 4. Verify Execution record was logged
	execs, count, err := svc.ListExecutions(ctx, "tenant-1", ListExecutionsFilter{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 1)
	assert.Equal(t, "COMPLETED", execs[0].Status)
	assert.Equal(t, "SCHEDULE", execs[0].TriggeredBy)

	// 5. Delete schedule
	err = svc.DeleteSchedule(ctx, "tenant-1", sched.ID)
	require.NoError(t, err)

	_, err = svc.GetSchedule(ctx, "tenant-1", sched.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDynamicQuery_SecurityInjectionPrevention(t *testing.T) {
	// Attempt to inject via filter column
	badFilterIn := CreateReportInput{
		Name:    "Injection Test",
		Dataset: DatasetTrips,
		Filters: []ReportFilter{
			{Column: "status; DROP TABLE trips; --", Operator: OpEq, Value: "completed"},
		},
	}
	assert.Error(t, badFilterIn.Validate())

	// Attempt to inject via SortBy
	badSortIn := CreateReportInput{
		Name:    "Sort Injection Test",
		Dataset: DatasetTrips,
		SortBy:  "id; DROP TABLE trips; --",
	}
	assert.Error(t, badSortIn.Validate())
}
