package reportbuilder

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
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

// Catalog & Metadata

func (s *Service) GetCatalog() map[DatasetType]DatasetMeta {
	return GetDatasetCatalog()
}

// Custom Reports Management

func (s *Service) CreateReport(ctx context.Context, tenantID, userID string, in CreateReportInput) (*CustomReport, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}

	chartType := in.ChartType
	if chartType == "" {
		chartType = "TABLE"
	}

	sortDir := strings.ToUpper(in.SortDirection)
	if sortDir == "" {
		sortDir = "DESC"
	}

	now := time.Now().UTC()
	rep := &CustomReport{
		ID:              uuid.NewString(),
		TenantID:        tenantID,
		Name:            strings.TrimSpace(in.Name),
		Description:     strings.TrimSpace(in.Description),
		Dataset:         in.Dataset,
		SelectedColumns: in.SelectedColumns,
		Filters:         in.Filters,
		GroupBy:         in.GroupBy,
		Aggregations:    in.Aggregations,
		SortBy:          in.SortBy,
		SortDirection:   sortDir,
		ChartType:       chartType,
		IsSystem:        false,
		CreatedBy:       userID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.repo.CreateReport(ctx, rep); err != nil {
		return nil, err
	}
	return rep, nil
}

func (s *Service) GetReport(ctx context.Context, tenantID, id string) (*CustomReport, error) {
	rep, err := s.repo.GetReportByID(ctx, tenantID, id)
	if err == nil {
		return rep, nil
	}

	// Check if ID is one of the built-in system reports
	for _, sysRep := range GetDefaultSystemReports() {
		if sysRep.ID == id {
			copyRep := sysRep
			copyRep.TenantID = tenantID
			return &copyRep, nil
		}
	}

	return nil, ErrNotFound
}

func (s *Service) ListReports(ctx context.Context, tenantID string, filter ListReportsFilter) ([]CustomReport, int, error) {
	reports, total, err := s.repo.ListReports(ctx, tenantID, filter)
	if err != nil {
		return nil, 0, err
	}

	// If no custom reports yet or system reports requested, augment with default system reports
	systemReports := GetDefaultSystemReports()
	existingIDs := make(map[string]bool)
	for _, r := range reports {
		existingIDs[r.ID] = true
	}

	for _, sys := range systemReports {
		if existingIDs[sys.ID] {
			continue
		}
		if filter.Dataset != nil && *filter.Dataset != "" && sys.Dataset != *filter.Dataset {
			continue
		}
		if filter.IsSystem != nil && !*filter.IsSystem {
			continue
		}
		if filter.Search != "" {
			searchLower := strings.ToLower(filter.Search)
			if !strings.Contains(strings.ToLower(sys.Name), searchLower) &&
				!strings.Contains(strings.ToLower(sys.Description), searchLower) {
				continue
			}
		}
		reports = append([]CustomReport{sys}, reports...)
		total++
	}

	return reports, total, nil
}

func (s *Service) UpdateReport(ctx context.Context, tenantID, id string, in UpdateReportInput) (*CustomReport, error) {
	rep, err := s.GetReport(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	if rep.IsSystem {
		return nil, ErrSystemReportImmutable
	}

	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: report name cannot be empty", ErrInvalidInput)
		}
		rep.Name = name
	}
	if in.Description != nil {
		rep.Description = strings.TrimSpace(*in.Description)
	}
	if in.SelectedColumns != nil {
		rep.SelectedColumns = in.SelectedColumns
	}
	if in.Filters != nil {
		rep.Filters = in.Filters
	}
	if in.GroupBy != nil {
		rep.GroupBy = *in.GroupBy
	}
	if in.Aggregations != nil {
		rep.Aggregations = in.Aggregations
	}
	if in.SortBy != nil {
		rep.SortBy = *in.SortBy
	}
	if in.SortDirection != nil {
		upper := strings.ToUpper(*in.SortDirection)
		if upper != "ASC" && upper != "DESC" {
			return nil, fmt.Errorf("%w: sort_direction must be ASC or DESC", ErrInvalidInput)
		}
		rep.SortDirection = upper
	}
	if in.ChartType != nil {
		rep.ChartType = *in.ChartType
	}

	// Validate updated structure
	validateIn := CreateReportInput{
		Name:            rep.Name,
		Dataset:         rep.Dataset,
		SelectedColumns: rep.SelectedColumns,
		Filters:         rep.Filters,
		GroupBy:         rep.GroupBy,
		Aggregations:    rep.Aggregations,
		SortBy:          rep.SortBy,
		SortDirection:   rep.SortDirection,
		ChartType:       rep.ChartType,
	}
	if err := validateIn.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}

	rep.UpdatedAt = time.Now().UTC()

	if err := s.repo.UpdateReport(ctx, rep); err != nil {
		return nil, err
	}
	return rep, nil
}

func (s *Service) DeleteReport(ctx context.Context, tenantID, id string) error {
	rep, err := s.GetReport(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if rep.IsSystem {
		return ErrSystemReportImmutable
	}
	return s.repo.DeleteReport(ctx, tenantID, id)
}

// Execution & Query Engine

type RunReportResult struct {
	ReportID            string       `json:"report_id"`
	ReportName          string       `json:"report_name"`
	Dataset             DatasetType  `json:"dataset"`
	Format              string       `json:"format"`
	QueryResult         *QueryResult `json:"query_result"`
	CSVData             []byte       `json:"-"`
	ExecutionID         string       `json:"execution_id"`
	RowCount            int          `json:"row_count"`
	ExecutionDurationMs int64        `json:"execution_duration_ms"`
}

func (s *Service) RunReport(
	ctx context.Context,
	tenantID, reportID string,
	in RunReportInput,
	triggeredBy string,
) (*RunReportResult, error) {
	rep, err := s.GetReport(ctx, tenantID, reportID)
	if err != nil {
		return nil, err
	}

	format := strings.ToUpper(strings.TrimSpace(in.Format))
	if format == "" {
		format = "JSON"
	}
	if format != "JSON" && format != "CSV" && format != "PDF" {
		return nil, fmt.Errorf("%w: format must be JSON, CSV, or PDF", ErrInvalidInput)
	}

	now := time.Now().UTC()
	exec := &ReportExecution{
		ID:          uuid.NewString(),
		TenantID:    tenantID,
		ReportID:    reportID,
		Format:      format,
		Status:      "RUNNING",
		TriggeredBy: triggeredBy,
		CreatedAt:   now,
	}
	_ = s.repo.CreateExecution(ctx, exec)

	res, err := s.repo.ExecuteReportQuery(ctx, tenantID, rep, in.AdHocFilters, in.Limit, in.Offset)
	if err != nil {
		exec.Status = "FAILED"
		exec.ErrorMessage = err.Error()
		completedAt := time.Now().UTC()
		exec.CompletedAt = &completedAt
		_ = s.repo.UpdateExecution(ctx, exec)
		return nil, fmt.Errorf("failed to run report query: %w", err)
	}

	var csvBytes []byte
	var fileSizeBytes int64

	if format == "CSV" {
		csvBytes, err = GenerateCSV(res)
		if err != nil {
			exec.Status = "FAILED"
			exec.ErrorMessage = "failed to format CSV: " + err.Error()
			completedAt := time.Now().UTC()
			exec.CompletedAt = &completedAt
			_ = s.repo.UpdateExecution(ctx, exec)
			return nil, err
		}
		fileSizeBytes = int64(len(csvBytes))
	}

	completedAt := time.Now().UTC()
	exec.Status = "COMPLETED"
	exec.RowCount = res.TotalRows
	exec.ExecutionDurationMs = res.ExecutionDurationMs
	exec.FileSizeBytes = fileSizeBytes
	exec.CompletedAt = &completedAt
	_ = s.repo.UpdateExecution(ctx, exec)

	return &RunReportResult{
		ReportID:            rep.ID,
		ReportName:          rep.Name,
		Dataset:             rep.Dataset,
		Format:              format,
		QueryResult:         res,
		CSVData:             csvBytes,
		ExecutionID:         exec.ID,
		RowCount:            res.TotalRows,
		ExecutionDurationMs: res.ExecutionDurationMs,
	}, nil
}

func (s *Service) RunAdHoc(
	ctx context.Context,
	tenantID string,
	in CreateReportInput,
	runIn RunReportInput,
) (*RunReportResult, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}

	chartType := in.ChartType
	if chartType == "" {
		chartType = "TABLE"
	}
	sortDir := strings.ToUpper(in.SortDirection)
	if sortDir == "" {
		sortDir = "DESC"
	}

	rep := &CustomReport{
		ID:              "ad-hoc-preview",
		TenantID:        tenantID,
		Name:            in.Name,
		Description:     in.Description,
		Dataset:         in.Dataset,
		SelectedColumns: in.SelectedColumns,
		Filters:         in.Filters,
		GroupBy:         in.GroupBy,
		Aggregations:    in.Aggregations,
		SortBy:          in.SortBy,
		SortDirection:   sortDir,
		ChartType:       chartType,
	}

	res, err := s.repo.ExecuteReportQuery(ctx, tenantID, rep, runIn.AdHocFilters, runIn.Limit, runIn.Offset)
	if err != nil {
		return nil, fmt.Errorf("failed to run ad-hoc query: %w", err)
	}

	format := strings.ToUpper(strings.TrimSpace(runIn.Format))
	if format == "" {
		format = "JSON"
	}

	var csvBytes []byte
	if format == "CSV" {
		csvBytes, err = GenerateCSV(res)
		if err != nil {
			return nil, err
		}
	}

	return &RunReportResult{
		ReportID:            rep.ID,
		ReportName:          rep.Name,
		Dataset:             rep.Dataset,
		Format:              format,
		QueryResult:         res,
		CSVData:             csvBytes,
		RowCount:            res.TotalRows,
		ExecutionDurationMs: res.ExecutionDurationMs,
	}, nil
}

// Scheduled Delivery Management

func (s *Service) CreateSchedule(ctx context.Context, tenantID, userID string, in CreateScheduleInput) (*ReportSchedule, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}

	rep, err := s.GetReport(ctx, tenantID, in.ReportID)
	if err != nil {
		return nil, fmt.Errorf("target report invalid: %w", err)
	}

	now := time.Now().UTC()
	nextRun := CalculateNextRun(in.Frequency, in.CronExpression)

	sched := &ReportSchedule{
		ID:             uuid.NewString(),
		TenantID:       tenantID,
		ReportID:       rep.ID,
		ReportName:     rep.Name,
		Name:           strings.TrimSpace(in.Name),
		Frequency:      strings.ToUpper(strings.TrimSpace(in.Frequency)),
		CronExpression: strings.TrimSpace(in.CronExpression),
		Format:         strings.ToUpper(strings.TrimSpace(in.Format)),
		Channel:        strings.ToUpper(strings.TrimSpace(in.Channel)),
		Recipients:     in.Recipients,
		IsActive:       true,
		NextRunAt:      nextRun,
		CreatedBy:      userID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.repo.CreateSchedule(ctx, sched); err != nil {
		return nil, err
	}
	return sched, nil
}

func (s *Service) GetSchedule(ctx context.Context, tenantID, id string) (*ReportSchedule, error) {
	return s.repo.GetScheduleByID(ctx, tenantID, id)
}

func (s *Service) ListSchedules(ctx context.Context, tenantID string, filter ListSchedulesFilter) ([]ReportSchedule, int, error) {
	return s.repo.ListSchedules(ctx, tenantID, filter)
}

func (s *Service) UpdateSchedule(ctx context.Context, tenantID, id string, in UpdateScheduleInput) (*ReportSchedule, error) {
	sched, err := s.GetSchedule(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: schedule name cannot be empty", ErrInvalidInput)
		}
		sched.Name = name
	}
	if in.Frequency != nil {
		freq := strings.ToUpper(strings.TrimSpace(*in.Frequency))
		if freq != "DAILY" && freq != "WEEKLY" && freq != "MONTHLY" {
			return nil, fmt.Errorf("%w: invalid frequency", ErrInvalidInput)
		}
		sched.Frequency = freq
		sched.NextRunAt = CalculateNextRun(sched.Frequency, sched.CronExpression)
	}
	if in.CronExpression != nil {
		sched.CronExpression = strings.TrimSpace(*in.CronExpression)
	}
	if in.Format != nil {
		fmtStr := strings.ToUpper(strings.TrimSpace(*in.Format))
		if fmtStr != "CSV" && fmtStr != "JSON" && fmtStr != "PDF" {
			return nil, fmt.Errorf("%w: invalid format", ErrInvalidInput)
		}
		sched.Format = fmtStr
	}
	if in.Channel != nil {
		ch := strings.ToUpper(strings.TrimSpace(*in.Channel))
		if ch != "EMAIL" && ch != "WEBHOOK" {
			return nil, fmt.Errorf("%w: invalid channel", ErrInvalidInput)
		}
		sched.Channel = ch
	}
	if in.Recipients != nil {
		if len(in.Recipients) == 0 {
			return nil, fmt.Errorf("%w: at least one recipient required", ErrInvalidInput)
		}
		sched.Recipients = in.Recipients
	}
	if in.IsActive != nil {
		sched.IsActive = *in.IsActive
	}

	sched.UpdatedAt = time.Now().UTC()

	if err := s.repo.UpdateSchedule(ctx, sched); err != nil {
		return nil, err
	}
	return sched, nil
}

func (s *Service) DeleteSchedule(ctx context.Context, tenantID, id string) error {
	return s.repo.DeleteSchedule(ctx, tenantID, id)
}

func (s *Service) TriggerSchedule(ctx context.Context, tenantID, scheduleID string) (*RunReportResult, error) {
	sched, err := s.GetSchedule(ctx, tenantID, scheduleID)
	if err != nil {
		return nil, err
	}

	runRes, err := s.RunReport(ctx, tenantID, sched.ReportID, RunReportInput{
		Format: sched.Format,
		Limit:  1000,
	}, "SCHEDULE")

	now := time.Now().UTC()
	nextRun := CalculateNextRun(sched.Frequency, sched.CronExpression)

	if err != nil {
		_ = s.repo.UpdateScheduleRunStatus(ctx, tenantID, scheduleID, "FAILED", err.Error(), &now, nextRun)
		return nil, fmt.Errorf("scheduled report execution failed: %w", err)
	}

	// Delivery simulation: mock channel dispatch
	// In production, this dispatches to EmailProviderPool or WebhookClient
	_ = s.repo.UpdateScheduleRunStatus(ctx, tenantID, scheduleID, "SUCCESS", "", &now, nextRun)

	return runRes, nil
}

// Executions History

func (s *Service) ListExecutions(ctx context.Context, tenantID string, filter ListExecutionsFilter) ([]ReportExecution, int, error) {
	return s.repo.ListExecutions(ctx, tenantID, filter)
}

// Helpers

func GenerateCSV(result *QueryResult) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	// Header row
	if err := w.Write(result.Columns); err != nil {
		return nil, fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Data rows
	for _, row := range result.Rows {
		record := make([]string, len(result.Columns))
		for i, col := range result.Columns {
			val := row[col]
			if val == nil {
				record[i] = ""
			} else {
				switch v := val.(type) {
				case time.Time:
					record[i] = v.Format(time.RFC3339)
				case *time.Time:
					if v != nil {
						record[i] = v.Format(time.RFC3339)
					}
				default:
					record[i] = fmt.Sprintf("%v", v)
				}
			}
		}
		if err := w.Write(record); err != nil {
			return nil, fmt.Errorf("failed to write CSV row: %w", err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("failed to flush CSV writer: %w", err)
	}

	return buf.Bytes(), nil
}

func CalculateNextRun(frequency, cronExpr string) *time.Time {
	now := time.Now().UTC()
	freq := strings.ToUpper(strings.TrimSpace(frequency))
	var next time.Time

	switch freq {
	case "DAILY":
		next = now.AddDate(0, 0, 1)
	case "WEEKLY":
		next = now.AddDate(0, 0, 7)
	case "MONTHLY":
		next = now.AddDate(0, 1, 0)
	default:
		next = now.AddDate(0, 0, 1)
	}

	return &next
}
