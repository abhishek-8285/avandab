package reportbuilder

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Custom Reports Repository

func (r *Repository) CreateReport(ctx context.Context, report *CustomReport) error {
	selectedColsJSON, err := json.Marshal(report.SelectedColumns)
	if err != nil {
		return fmt.Errorf("failed to marshal selected_columns: %w", err)
	}
	filtersJSON, err := json.Marshal(report.Filters)
	if err != nil {
		return fmt.Errorf("failed to marshal filters: %w", err)
	}
	aggsJSON, err := json.Marshal(report.Aggregations)
	if err != nil {
		return fmt.Errorf("failed to marshal aggregations: %w", err)
	}

	query := `INSERT INTO custom_reports (
		id, tenant_id, name, description, dataset,
		selected_columns, filters, group_by, aggregations,
		sort_by, sort_direction, chart_type, is_system,
		created_by, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = r.db.ExecContext(ctx, query,
		report.ID,
		report.TenantID,
		report.Name,
		report.Description,
		string(report.Dataset),
		string(selectedColsJSON),
		string(filtersJSON),
		report.GroupBy,
		string(aggsJSON),
		report.SortBy,
		report.SortDirection,
		report.ChartType,
		report.IsSystem,
		report.CreatedBy,
		report.CreatedAt,
		report.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert custom report: %w", err)
	}
	return nil
}

func (r *Repository) GetReportByID(ctx context.Context, tenantID, id string) (*CustomReport, error) {
	query := `SELECT 
		id, tenant_id, name, description, dataset,
		selected_columns, filters, group_by, aggregations,
		sort_by, sort_direction, chart_type, is_system,
		created_by, created_at, updated_at
	FROM custom_reports
	WHERE id = ? AND (tenant_id = ? OR is_system = 1 OR is_system = TRUE)`

	row := r.db.QueryRowContext(ctx, query, id, tenantID)

	var (
		rep              CustomReport
		datasetStr       string
		selectedColsJSON string
		filtersJSON      string
		aggsJSON         string
	)

	err := row.Scan(
		&rep.ID,
		&rep.TenantID,
		&rep.Name,
		&rep.Description,
		&datasetStr,
		&selectedColsJSON,
		&filtersJSON,
		&rep.GroupBy,
		&aggsJSON,
		&rep.SortBy,
		&rep.SortDirection,
		&rep.ChartType,
		&rep.IsSystem,
		&rep.CreatedBy,
		&rep.CreatedAt,
		&rep.UpdatedAt,
	)
	if err != nil {
		if errorsIs(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to query custom report: %w", err)
	}

	rep.Dataset = DatasetType(datasetStr)
	_ = json.Unmarshal([]byte(selectedColsJSON), &rep.SelectedColumns)
	_ = json.Unmarshal([]byte(filtersJSON), &rep.Filters)
	_ = json.Unmarshal([]byte(aggsJSON), &rep.Aggregations)

	return &rep, nil
}

func (r *Repository) ListReports(ctx context.Context, tenantID string, filter ListReportsFilter) ([]CustomReport, int, error) {
	whereClauses := []string{"(tenant_id = ? OR is_system = 1 OR is_system = TRUE)"}
	args := []interface{}{tenantID}

	if filter.Dataset != nil && *filter.Dataset != "" {
		whereClauses = append(whereClauses, "dataset = ?")
		args = append(args, string(*filter.Dataset))
	}

	if filter.IsSystem != nil {
		whereClauses = append(whereClauses, "is_system = ?")
		args = append(args, *filter.IsSystem)
	}

	if filter.Search != "" {
		whereClauses = append(whereClauses, "(name LIKE ? OR description LIKE ?)")
		searchPattern := "%" + filter.Search + "%"
		args = append(args, searchPattern, searchPattern)
	}

	whereSQL := " WHERE " + strings.Join(whereClauses, " AND ")

	countQuery := "SELECT COUNT(*) FROM custom_reports" + whereSQL
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count custom reports: %w", err)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	selectQuery := `SELECT 
		id, tenant_id, name, description, dataset,
		selected_columns, filters, group_by, aggregations,
		sort_by, sort_direction, chart_type, is_system,
		created_by, created_at, updated_at
	FROM custom_reports` + whereSQL + " ORDER BY is_system DESC, created_at DESC LIMIT ? OFFSET ?"

	queryArgs := append([]interface{}{}, args...)
	queryArgs = append(queryArgs, limit, offset)

	rows, err := r.db.QueryContext(ctx, selectQuery, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query custom reports: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var reports []CustomReport
	for rows.Next() {
		var (
			rep              CustomReport
			datasetStr       string
			selectedColsJSON string
			filtersJSON      string
			aggsJSON         string
		)
		err := rows.Scan(
			&rep.ID,
			&rep.TenantID,
			&rep.Name,
			&rep.Description,
			&datasetStr,
			&selectedColsJSON,
			&filtersJSON,
			&rep.GroupBy,
			&aggsJSON,
			&rep.SortBy,
			&rep.SortDirection,
			&rep.ChartType,
			&rep.IsSystem,
			&rep.CreatedBy,
			&rep.CreatedAt,
			&rep.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan custom report: %w", err)
		}
		rep.Dataset = DatasetType(datasetStr)
		_ = json.Unmarshal([]byte(selectedColsJSON), &rep.SelectedColumns)
		_ = json.Unmarshal([]byte(filtersJSON), &rep.Filters)
		_ = json.Unmarshal([]byte(aggsJSON), &rep.Aggregations)
		reports = append(reports, rep)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("rows error: %w", err)
	}

	return reports, total, nil
}

func (r *Repository) UpdateReport(ctx context.Context, report *CustomReport) error {
	selectedColsJSON, err := json.Marshal(report.SelectedColumns)
	if err != nil {
		return fmt.Errorf("failed to marshal selected_columns: %w", err)
	}
	filtersJSON, err := json.Marshal(report.Filters)
	if err != nil {
		return fmt.Errorf("failed to marshal filters: %w", err)
	}
	aggsJSON, err := json.Marshal(report.Aggregations)
	if err != nil {
		return fmt.Errorf("failed to marshal aggregations: %w", err)
	}

	query := `UPDATE custom_reports SET
		name = ?,
		description = ?,
		selected_columns = ?,
		filters = ?,
		group_by = ?,
		aggregations = ?,
		sort_by = ?,
		sort_direction = ?,
		chart_type = ?,
		updated_at = ?
	WHERE id = ? AND tenant_id = ? AND (is_system = 0 OR is_system = FALSE)`

	res, err := r.db.ExecContext(ctx, query,
		report.Name,
		report.Description,
		string(selectedColsJSON),
		string(filtersJSON),
		report.GroupBy,
		string(aggsJSON),
		report.SortBy,
		report.SortDirection,
		report.ChartType,
		report.UpdatedAt,
		report.ID,
		report.TenantID,
	)
	if err != nil {
		return fmt.Errorf("failed to update custom report: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) DeleteReport(ctx context.Context, tenantID, id string) error {
	query := `DELETE FROM custom_reports 
	WHERE id = ? AND tenant_id = ? AND (is_system = 0 OR is_system = FALSE)`

	res, err := r.db.ExecContext(ctx, query, id, tenantID)
	if err != nil {
		return fmt.Errorf("failed to delete custom report: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Report Schedules Repository

func (r *Repository) CreateSchedule(ctx context.Context, sched *ReportSchedule) error {
	recipientsJSON, err := json.Marshal(sched.Recipients)
	if err != nil {
		return fmt.Errorf("failed to marshal recipients: %w", err)
	}

	query := `INSERT INTO report_schedules (
		id, tenant_id, report_id, name, frequency,
		cron_expression, format, channel, recipients,
		is_active, last_run_at, next_run_at, last_status,
		last_error, created_by, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = r.db.ExecContext(ctx, query,
		sched.ID,
		sched.TenantID,
		sched.ReportID,
		sched.Name,
		sched.Frequency,
		sched.CronExpression,
		sched.Format,
		sched.Channel,
		string(recipientsJSON),
		sched.IsActive,
		sched.LastRunAt,
		sched.NextRunAt,
		sched.LastStatus,
		sched.LastError,
		sched.CreatedBy,
		sched.CreatedAt,
		sched.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert report schedule: %w", err)
	}
	return nil
}

func (r *Repository) GetScheduleByID(ctx context.Context, tenantID, id string) (*ReportSchedule, error) {
	query := `SELECT 
		s.id, s.tenant_id, s.report_id, s.name, s.frequency,
		s.cron_expression, s.format, s.channel, s.recipients,
		s.is_active, s.last_run_at, s.next_run_at, s.last_status,
		s.last_error, s.created_by, s.created_at, s.updated_at,
		COALESCE(r.name, '') AS report_name
	FROM report_schedules s
	LEFT JOIN custom_reports r ON r.id = s.report_id
	WHERE s.id = ? AND s.tenant_id = ?`

	row := r.db.QueryRowContext(ctx, query, id, tenantID)

	var (
		s              ReportSchedule
		recipientsJSON string
	)

	err := row.Scan(
		&s.ID,
		&s.TenantID,
		&s.ReportID,
		&s.Name,
		&s.Frequency,
		&s.CronExpression,
		&s.Format,
		&s.Channel,
		&recipientsJSON,
		&s.IsActive,
		&s.LastRunAt,
		&s.NextRunAt,
		&s.LastStatus,
		&s.LastError,
		&s.CreatedBy,
		&s.CreatedAt,
		&s.UpdatedAt,
		&s.ReportName,
	)
	if err != nil {
		if errorsIs(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to query report schedule: %w", err)
	}

	_ = json.Unmarshal([]byte(recipientsJSON), &s.Recipients)
	return &s, nil
}

func (r *Repository) ListSchedules(ctx context.Context, tenantID string, filter ListSchedulesFilter) ([]ReportSchedule, int, error) {
	whereClauses := []string{"s.tenant_id = ?"}
	args := []interface{}{tenantID}

	if filter.ReportID != nil && *filter.ReportID != "" {
		whereClauses = append(whereClauses, "s.report_id = ?")
		args = append(args, *filter.ReportID)
	}

	if filter.IsActive != nil {
		whereClauses = append(whereClauses, "s.is_active = ?")
		args = append(args, *filter.IsActive)
	}

	whereSQL := " WHERE " + strings.Join(whereClauses, " AND ")

	countQuery := "SELECT COUNT(*) FROM report_schedules s" + whereSQL
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count report schedules: %w", err)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	selectQuery := `SELECT 
		s.id, s.tenant_id, s.report_id, s.name, s.frequency,
		s.cron_expression, s.format, s.channel, s.recipients,
		s.is_active, s.last_run_at, s.next_run_at, s.last_status,
		s.last_error, s.created_by, s.created_at, s.updated_at,
		COALESCE(r.name, '') AS report_name
	FROM report_schedules s
	LEFT JOIN custom_reports r ON r.id = s.report_id` + whereSQL + " ORDER BY s.created_at DESC LIMIT ? OFFSET ?"

	queryArgs := append([]interface{}{}, args...)
	queryArgs = append(queryArgs, limit, offset)

	rows, err := r.db.QueryContext(ctx, selectQuery, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query report schedules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var schedules []ReportSchedule
	for rows.Next() {
		var (
			s              ReportSchedule
			recipientsJSON string
		)
		err := rows.Scan(
			&s.ID,
			&s.TenantID,
			&s.ReportID,
			&s.Name,
			&s.Frequency,
			&s.CronExpression,
			&s.Format,
			&s.Channel,
			&recipientsJSON,
			&s.IsActive,
			&s.LastRunAt,
			&s.NextRunAt,
			&s.LastStatus,
			&s.LastError,
			&s.CreatedBy,
			&s.CreatedAt,
			&s.UpdatedAt,
			&s.ReportName,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan report schedule: %w", err)
		}
		_ = json.Unmarshal([]byte(recipientsJSON), &s.Recipients)
		schedules = append(schedules, s)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("rows error: %w", err)
	}

	return schedules, total, nil
}

func (r *Repository) UpdateSchedule(ctx context.Context, sched *ReportSchedule) error {
	recipientsJSON, err := json.Marshal(sched.Recipients)
	if err != nil {
		return fmt.Errorf("failed to marshal recipients: %w", err)
	}

	query := `UPDATE report_schedules SET
		name = ?,
		frequency = ?,
		cron_expression = ?,
		format = ?,
		channel = ?,
		recipients = ?,
		is_active = ?,
		updated_at = ?
	WHERE id = ? AND tenant_id = ?`

	res, err := r.db.ExecContext(ctx, query,
		sched.Name,
		sched.Frequency,
		sched.CronExpression,
		sched.Format,
		sched.Channel,
		string(recipientsJSON),
		sched.IsActive,
		sched.UpdatedAt,
		sched.ID,
		sched.TenantID,
	)
	if err != nil {
		return fmt.Errorf("failed to update report schedule: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) DeleteSchedule(ctx context.Context, tenantID, id string) error {
	query := `DELETE FROM report_schedules WHERE id = ? AND tenant_id = ?`

	res, err := r.db.ExecContext(ctx, query, id, tenantID)
	if err != nil {
		return fmt.Errorf("failed to delete report schedule: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) UpdateScheduleRunStatus(ctx context.Context, tenantID, id, status, errStr string, runAt, nextRunAt *time.Time) error {
	query := `UPDATE report_schedules SET
		last_run_at = ?,
		next_run_at = ?,
		last_status = ?,
		last_error = ?,
		updated_at = ?
	WHERE id = ? AND tenant_id = ?`

	_, err := r.db.ExecContext(ctx, query, runAt, nextRunAt, status, errStr, time.Now().UTC(), id, tenantID)
	if err != nil {
		return fmt.Errorf("failed to update schedule run status: %w", err)
	}
	return nil
}

// Report Executions Repository

func (r *Repository) CreateExecution(ctx context.Context, exec *ReportExecution) error {
	query := `INSERT INTO report_executions (
		id, tenant_id, report_id, schedule_id, format,
		status, row_count, file_size_bytes, execution_duration_ms,
		error_message, triggered_by, created_at, completed_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(ctx, query,
		exec.ID,
		exec.TenantID,
		exec.ReportID,
		exec.ScheduleID,
		exec.Format,
		exec.Status,
		exec.RowCount,
		exec.FileSizeBytes,
		exec.ExecutionDurationMs,
		exec.ErrorMessage,
		exec.TriggeredBy,
		exec.CreatedAt,
		exec.CompletedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert report execution: %w", err)
	}
	return nil
}

func (r *Repository) UpdateExecution(ctx context.Context, exec *ReportExecution) error {
	query := `UPDATE report_executions SET
		status = ?,
		row_count = ?,
		file_size_bytes = ?,
		execution_duration_ms = ?,
		error_message = ?,
		completed_at = ?
	WHERE id = ? AND tenant_id = ?`

	_, err := r.db.ExecContext(ctx, query,
		exec.Status,
		exec.RowCount,
		exec.FileSizeBytes,
		exec.ExecutionDurationMs,
		exec.ErrorMessage,
		exec.CompletedAt,
		exec.ID,
		exec.TenantID,
	)
	if err != nil {
		return fmt.Errorf("failed to update report execution: %w", err)
	}
	return nil
}

func (r *Repository) ListExecutions(ctx context.Context, tenantID string, filter ListExecutionsFilter) ([]ReportExecution, int, error) {
	whereClauses := []string{"e.tenant_id = ?"}
	args := []interface{}{tenantID}

	if filter.ReportID != nil && *filter.ReportID != "" {
		whereClauses = append(whereClauses, "e.report_id = ?")
		args = append(args, *filter.ReportID)
	}

	if filter.Status != nil && *filter.Status != "" {
		whereClauses = append(whereClauses, "e.status = ?")
		args = append(args, *filter.Status)
	}

	whereSQL := " WHERE " + strings.Join(whereClauses, " AND ")

	countQuery := "SELECT COUNT(*) FROM report_executions e" + whereSQL
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count report executions: %w", err)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	selectQuery := `SELECT 
		e.id, e.tenant_id, e.report_id, e.schedule_id, e.format,
		e.status, e.row_count, e.file_size_bytes, e.execution_duration_ms,
		e.error_message, e.triggered_by, e.created_at, e.completed_at,
		COALESCE(r.name, '') AS report_name
	FROM report_executions e
	LEFT JOIN custom_reports r ON r.id = e.report_id` + whereSQL + " ORDER BY e.created_at DESC LIMIT ? OFFSET ?"

	queryArgs := append([]interface{}{}, args...)
	queryArgs = append(queryArgs, limit, offset)

	rows, err := r.db.QueryContext(ctx, selectQuery, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query report executions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var execs []ReportExecution
	for rows.Next() {
		var e ReportExecution
		err := rows.Scan(
			&e.ID,
			&e.TenantID,
			&e.ReportID,
			&e.ScheduleID,
			&e.Format,
			&e.Status,
			&e.RowCount,
			&e.FileSizeBytes,
			&e.ExecutionDurationMs,
			&e.ErrorMessage,
			&e.TriggeredBy,
			&e.CreatedAt,
			&e.CompletedAt,
			&e.ReportName,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan report execution: %w", err)
		}
		execs = append(execs, e)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("rows error: %w", err)
	}

	return execs, total, nil
}

// Dynamic Dataset Query Engine

func (r *Repository) ExecuteReportQuery(
	ctx context.Context,
	tenantID string,
	report *CustomReport,
	adHocFilters []ReportFilter,
	limit, offset int,
) (*QueryResult, error) {
	start := time.Now()

	catalog := GetDatasetCatalog()
	meta, ok := catalog[report.Dataset]
	if !ok {
		return nil, ErrInvalidDataset
	}

	validCols := make(map[string]ColumnMeta)
	for _, c := range meta.Columns {
		validCols[c.Name] = c
	}

	// 1. Build SELECT expressions
	var selectExprs []string
	var colHeaders []string

	if len(report.Aggregations) > 0 {
		if report.GroupBy != "" {
			if _, ok := validCols[report.GroupBy]; !ok {
				return nil, ErrInvalidColumn
			}
			selectExprs = append(selectExprs, report.GroupBy)
			colHeaders = append(colHeaders, report.GroupBy)
		}

		for _, agg := range report.Aggregations {
			fn := strings.ToUpper(string(agg.Function))
			if fn != string(AggCount) && fn != string(AggSum) && fn != string(AggAvg) && fn != string(AggMin) && fn != string(AggMax) {
				return nil, ErrInvalidAggregation
			}

			alias := agg.Alias
			if alias == "" {
				if agg.Column == "" || agg.Column == "*" {
					alias = strings.ToLower(fn) + "_all"
				} else {
					alias = strings.ToLower(fn) + "_" + agg.Column
				}
			}

			// Clean alias to alphanumeric/underscore only
			alias = cleanIdentifier(alias)

			var expr string
			if agg.Column == "" || agg.Column == "*" {
				expr = fmt.Sprintf("%s(*) AS %s", fn, alias)
			} else {
				if _, ok := validCols[agg.Column]; !ok {
					return nil, ErrInvalidColumn
				}
				expr = fmt.Sprintf("%s(%s) AS %s", fn, agg.Column, alias)
			}

			selectExprs = append(selectExprs, expr)
			colHeaders = append(colHeaders, alias)
		}
	} else if len(report.SelectedColumns) > 0 {
		for _, col := range report.SelectedColumns {
			if _, ok := validCols[col]; !ok {
				return nil, ErrInvalidColumn
			}
			selectExprs = append(selectExprs, col)
			colHeaders = append(colHeaders, col)
		}
	} else {
		// Default: all columns in catalog
		for _, c := range meta.Columns {
			selectExprs = append(selectExprs, c.Name)
			colHeaders = append(colHeaders, c.Name)
		}
	}

	if len(selectExprs) == 0 {
		return nil, errorsNew("no valid columns to select")
	}

	// 2. Build FROM & WHERE clauses
	whereClauses := []string{"tenant_id = ?"}
	args := []interface{}{tenantID}

	allFilters := append([]ReportFilter{}, report.Filters...)
	allFilters = append(allFilters, adHocFilters...)

	for _, f := range allFilters {
		if _, ok := validCols[f.Column]; !ok {
			return nil, ErrInvalidColumn
		}
		clause, fArgs, err := buildFilterClause(f)
		if err != nil {
			return nil, err
		}
		if clause != "" {
			whereClauses = append(whereClauses, clause)
			args = append(args, fArgs...)
		}
	}

	whereSQL := " WHERE " + strings.Join(whereClauses, " AND ")

	// 3. Build GROUP BY
	groupBySQL := ""
	if len(report.Aggregations) > 0 && report.GroupBy != "" {
		groupBySQL = " GROUP BY " + report.GroupBy
	}

	// 4. Build ORDER BY
	orderBySQL := ""
	if report.SortBy != "" {
		if _, ok := validCols[report.SortBy]; ok || isColHeader(report.SortBy, colHeaders) {
			dir := "DESC"
			if strings.ToUpper(report.SortDirection) == "ASC" {
				dir = "ASC"
			}
			orderBySQL = fmt.Sprintf(" ORDER BY %s %s", report.SortBy, dir)
		}
	} else if report.GroupBy != "" {
		orderBySQL = fmt.Sprintf(" ORDER BY %s ASC", report.GroupBy)
	} else if _, hasCreated := validCols["created_at"]; hasCreated {
		orderBySQL = " ORDER BY created_at DESC"
	}

	// 5. Build LIMIT & OFFSET
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}
	if offset < 0 {
		offset = 0
	}

	limitSQL := " LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	fullSQL := fmt.Sprintf("SELECT %s FROM %s%s%s%s%s",
		strings.Join(selectExprs, ", "),
		meta.TableName,
		whereSQL,
		groupBySQL,
		orderBySQL,
		limitSQL,
	)

	rows, err := r.db.QueryContext(ctx, fullSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute dynamic report query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get result columns: %w", err)
	}

	var results []map[string]interface{}
	for rows.Next() {
		columns := make([]interface{}, len(cols))
		columnPointers := make([]interface{}, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}

		if err := rows.Scan(columnPointers...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		rowMap := make(map[string]interface{})
		for i, colName := range cols {
			val := columns[i]
			// Convert byte slice to string for JSON/text readability
			if b, ok := val.([]byte); ok {
				rowMap[colName] = string(b)
			} else {
				rowMap[colName] = val
			}
		}
		results = append(results, rowMap)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	duration := time.Since(start).Milliseconds()

	return &QueryResult{
		Columns:             colHeaders,
		Rows:                results,
		TotalRows:           len(results),
		ExecutionDurationMs: duration,
	}, nil
}

func buildFilterClause(f ReportFilter) (string, []interface{}, error) {
	op := strings.ToUpper(string(f.Operator))
	switch FilterOperator(op) {
	case OpEq:
		return fmt.Sprintf("%s = ?", f.Column), []interface{}{f.Value}, nil
	case OpNeq:
		return fmt.Sprintf("%s != ?", f.Column), []interface{}{f.Value}, nil
	case OpGt:
		return fmt.Sprintf("%s > ?", f.Column), []interface{}{f.Value}, nil
	case OpGte:
		return fmt.Sprintf("%s >= ?", f.Column), []interface{}{f.Value}, nil
	case OpLt:
		return fmt.Sprintf("%s < ?", f.Column), []interface{}{f.Value}, nil
	case OpLte:
		return fmt.Sprintf("%s <= ?", f.Column), []interface{}{f.Value}, nil
	case OpContains:
		return fmt.Sprintf("%s LIKE ?", f.Column), []interface{}{fmt.Sprintf("%%%v%%", f.Value)}, nil
	case OpBetween:
		return fmt.Sprintf("%s BETWEEN ? AND ?", f.Column), []interface{}{f.Value, f.ValueTo}, nil
	case OpIn:
		switch slice := f.Value.(type) {
		case []interface{}:
			if len(slice) == 0 {
				return "", nil, nil
			}
			placeholders := make([]string, len(slice))
			for i := range slice {
				placeholders[i] = "?"
			}
			return fmt.Sprintf("%s IN (%s)", f.Column, strings.Join(placeholders, ",")), slice, nil
		case []string:
			if len(slice) == 0 {
				return "", nil, nil
			}
			placeholders := make([]string, len(slice))
			args := make([]interface{}, len(slice))
			for i, v := range slice {
				placeholders[i] = "?"
				args[i] = v
			}
			return fmt.Sprintf("%s IN (%s)", f.Column, strings.Join(placeholders, ",")), args, nil
		default:
			return fmt.Sprintf("%s = ?", f.Column), []interface{}{f.Value}, nil
		}
	default:
		return "", nil, ErrInvalidFilter
	}
}

func cleanIdentifier(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	res := b.String()
	if res == "" {
		return "val"
	}
	return res
}

func isColHeader(col string, headers []string) bool {
	for _, h := range headers {
		if strings.EqualFold(col, h) {
			return true
		}
	}
	return false
}

func errorsIs(err, target error) bool {
	return strings.Contains(err.Error(), target.Error())
}

func errorsNew(s string) error {
	return fmt.Errorf("%s", s)
}
