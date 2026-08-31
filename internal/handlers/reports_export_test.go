package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/pdf"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func newReportsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_reports_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	migrationsDir := "../../db/migrations"
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		for _, cand := range []string{"db/migrations", "../db/migrations", "../../db/migrations"} {
			if _, err := os.Stat(cand); err == nil {
				migrationsDir = cand
				break
			}
		}
	}

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestWriteCSV_BOMAndHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	header := []string{"Col1", "Col2"}
	rows := [][]string{{"Val1", "Val2"}, {"Val3", "Val4"}}

	writeCSV(w, "test.csv", header, rows, 10, "")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/csv; charset=utf-8", w.Header().Get("Content-Type"))
	assert.Equal(t, `attachment; filename="test.csv"`, w.Header().Get("Content-Disposition"))
	assert.Equal(t, "2", w.Header().Get("X-Export-Rows"))
	assert.Empty(t, w.Header().Get("Link"))

	body := w.Body.Bytes()
	require.True(t, len(body) >= 3, "body must have at least 3 bytes for BOM")
	assert.Equal(t, byte(0xEF), body[0])
	assert.Equal(t, byte(0xBB), body[1])
	assert.Equal(t, byte(0xBF), body[2])

	content := string(body[3:])
	assert.Contains(t, content, "Col1,Col2")
	assert.Contains(t, content, "Val1,Val2")
	assert.Contains(t, content, "Val3,Val4")
}

func TestWriteCSV_TruncationAndLinkHeader(t *testing.T) {
	w := httptest.NewRecorder()
	header := []string{"ID"}
	rows := [][]string{{"1"}, {"2"}, {"3"}, {"4"}, {"5"}}

	writeCSV(w, "test.csv", header, rows, 3, "/reports/trips.csv?offset=3")

	assert.Equal(t, "3", w.Header().Get("X-Export-Rows"))
	assert.Equal(t, `</reports/trips.csv?offset=3>; rel="next"`, w.Header().Get("Link"))
}

func TestGenerateReportPDF_MagicBytes(t *testing.T) {
	header := []string{"Metric", "Value"}
	rows := [][]string{{"Revenue", "15000.00"}, {"Trips", "42"}}

	pdfBytes, err := pdf.GenerateReportPDF("Test Summary", "Avandab Transport", header, rows)
	require.NoError(t, err)
	require.NotEmpty(t, pdfBytes)

	// Assert %PDF- magic header
	assert.True(t, bytes.HasPrefix(pdfBytes, []byte("%PDF-")), "PDF output must start with %PDF- header")
}

func TestReportsExport_RBACAndEndpoints(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	db := newReportsTestDB(t)
	repo := sqlite.NewRepository(db)
	bus := events.NewInMemoryBus()
	cfg := &config.Config{
		AppEnv:        "testing",
		Port:          "8080",
		ExportMaxRows: 50000,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, bus)

	authSvc := &mockAuthSvc{
		allowed: map[string]bool{
			"authorized-user:reports:read": true,
		},
	}

	tmpl, err := parseTemplates(authSvc)
	require.NoError(t, err)

	app := &App{
		Config:    cfg,
		Templates: tmpl,
		AuthSrv:   authSvc,
		Services:  services,
	}

	r := chi.NewRouter()
	reportHandlers := &ReportHandlers{App: app}

	// Middleware injects test user with tenant context
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			userID := req.Header.Get("X-Test-User")
			ctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
			if userID != "" {
				ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{
					UserID: userID,
					Role:   "user",
				})
			}
			req = req.WithContext(ctx)
			next.ServeHTTP(w, req)
		})
	})

	r.Route("/reports", func(sub chi.Router) {
		reportHandlers.Routes(sub)
	})

	// 1. RBAC Denied check: unauthorized user -> 403 Forbidden
	reqDenied := httptest.NewRequest("GET", "/reports/trips.csv", nil)
	reqDenied.Header.Set("X-Test-User", "unauthorized-user")
	wDenied := httptest.NewRecorder()
	r.ServeHTTP(wDenied, reqDenied)
	assert.Equal(t, http.StatusForbidden, wDenied.Code)

	// 2. CSV Exports with authorized user
	csvEndpoints := []struct {
		url      string
		filename string
	}{
		{"/reports/revenue.csv", "revenue_report.csv"},
		{"/reports/trips.csv", "trips_report.csv"},
		{"/reports/drivers.csv", "drivers_report.csv"},
		{"/reports/vehicles.csv", "vehicles_report.csv"},
		{"/reports/customers.csv", "customers_report.csv"},
		{"/reports/pending-payments.csv", "pending_payments_report.csv"},
	}

	for _, ep := range csvEndpoints {
		t.Run("CSV_"+ep.filename, func(t *testing.T) {
			req := httptest.NewRequest("GET", ep.url, nil)
			req.Header.Set("X-Test-User", "authorized-user")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Header().Get("Content-Type"), "text/csv")
			assert.Contains(t, w.Header().Get("Content-Disposition"), ep.filename)
			assert.NotEmpty(t, w.Header().Get("X-Export-Rows"))

			body := w.Body.Bytes()
			require.True(t, len(body) >= 3)
			assert.Equal(t, byte(0xEF), body[0], "Must contain UTF-8 BOM byte 1")
			assert.Equal(t, byte(0xBB), body[1], "Must contain UTF-8 BOM byte 2")
			assert.Equal(t, byte(0xBF), body[2], "Must contain UTF-8 BOM byte 3")
		})
	}

	// 3. PDF Exports with authorized user
	pdfEndpoints := []struct {
		url      string
		filename string
	}{
		{"/reports/revenue.pdf", "revenue_report.pdf"},
		{"/reports/pending-payments.pdf", "pending_payments_report.pdf"},
	}

	for _, ep := range pdfEndpoints {
		t.Run("PDF_"+ep.filename, func(t *testing.T) {
			req := httptest.NewRequest("GET", ep.url, nil)
			req.Header.Set("X-Test-User", "authorized-user")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "application/pdf", w.Header().Get("Content-Type"))
			assert.Contains(t, w.Header().Get("Content-Disposition"), ep.filename)
			assert.True(t, bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF-")), "Must have PDF magic bytes")
		})
	}
}
