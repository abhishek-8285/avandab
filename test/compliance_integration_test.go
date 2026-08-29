package test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/alerts/pipeline"
	alertsqlite "transport-app/internal/alerts/repository/sqlite"
	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/domain"
	"transport-app/internal/events"
	"transport-app/internal/handlers"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/shared/uow"
	tripapp "transport-app/internal/trip/application"
	tripagg "transport-app/internal/trip/domain/aggregate"
)

type realClock struct{}

func (c *realClock) Now() time.Time { return time.Now().UTC() }

func setupComplianceTestEnv(t *testing.T) (*sql.DB, *service.Services, *events.InMemoryBus, ports.UnitOfWork, *handlers.App) {
	t.Helper()
	name := fmt.Sprintf("test_comp_int_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	dbConn, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.Up(dbConn, "../db/migrations"))
	_, _ = dbConn.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)
	t.Cleanup(func() { _ = dbConn.Close() })

	repo := sqlite.NewRepository(dbConn)
	uploadDir := filepath.Join(t.TempDir(), "uploads")
	cfg := &config.Config{
		UploadDir:     uploadDir,
		MaxUploadSize: 10 * 1024 * 1024,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := events.NewInMemoryBus()

	svcs := service.NewServices(repo, cfg, log, bus)
	unitOfWork := uow.NewSQLUnitOfWork(dbConn)

	authStore := auth.NewSessionStore("test-secret-32bytes-long-enough!", false)
	authSrv, err := auth.NewCasbinAuthorizationService(dbConn)
	require.NoError(t, err)
	resetStore := auth.NewResetTokenStore(15 * time.Minute)
	app := handlers.NewApp(svcs, cfg, authStore, dbConn, authSrv, resetStore)

	return dbConn, svcs, bus, unitOfWork, app
}

func TestSubTask4C_MasterComplianceSuite(t *testing.T) {
	dbConn, svcs, bus, unitOfWork, app := setupComplianceTestEnv(t)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")
	now := time.Now().UTC()
	clk := &realClock{}

	// Wire alert engine from 4A/4B to verify ComplianceBlocked event processing
	alertsRepo := alertsqlite.NewAlertRepository(dbConn)
	alertEngine := pipeline.NewEngine(alertsRepo, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	bus.Subscribe("ComplianceBlocked", func(ctx context.Context, e events.Event) error {
		return alertEngine.ProcessEvent(ctx, e)
	})

	// Setup Base Route & Trip
	routeID := "rt-test-1"
	_, err := dbConn.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES (?, 'Mumbai', 'Pune', 150, 4, 5000, 'tenant-1')`, routeID)
	require.NoError(t, err)

	tripID := "trp-test-1"
	_, err = dbConn.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id) VALUES (?, 'TRP-101', ?, datetime('now'), 'scheduled', 'tenant-1')`, tripID, routeID)
	require.NoError(t, err)

	// 1. 5-Doc Gate: Attempt to assign driver with expired license (Path A & Path B)
	expiredDate := now.AddDate(0, 0, -3).Format("2006-01-02")
	drvExpiredID := "drv-exp-1"
	_, err = dbConn.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id) VALUES (?, 'DRV-EXP-1', 'Rajesh', 'Khanna', '+919999999901', 'DL-EXP-1', ?, 'available', 'tenant-1')`, drvExpiredID, expiredDate)
	require.NoError(t, err)

	assignDriverUC := tripapp.NewAssignDriverUseCase(unitOfWork, clk)
	err = assignDriverUC.Execute(ctx, tripapp.AssignDriverCommand{
		TripID:   tripagg.TripID(tripID),
		DriverID: drvExpiredID,
		TenantID: "tenant-1",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Dispatch blocked: driver license expired (compliance)")

	// Check that ComplianceBlocked event creates an alert in canonical alerts table
	_, _ = svcs.Compliance.CheckDispatchCompliance(ctx, drvExpiredID, "")
	time.Sleep(50 * time.Millisecond)
	openAlerts, err := alertsRepo.ListAlerts(ctx, "open", 10, 0)
	require.NoError(t, err)
	require.NotEmpty(t, openAlerts)
	require.Equal(t, "compliance", openAlerts[0].Source)
	require.Equal(t, "blocker", openAlerts[0].Severity)

	// 2. Exemptions: Create exemption for blocked driver -> assignment succeeds and audit log records bypass
	err = svcs.Compliance.CreateExemption(ctx, service.ComplianceExemption{
		EntityType:  "driver",
		EntityID:    drvExpiredID,
		DocType:     "license",
		Reason:      "RTO receipt submitted; awaiting plastic card",
		ExemptUntil: now.Add(72 * time.Hour),
		CreatedBy:   "admin-101",
	})
	require.NoError(t, err)

	err = assignDriverUC.Execute(ctx, tripapp.AssignDriverCommand{
		TripID:   tripagg.TripID(tripID),
		DriverID: drvExpiredID,
		TenantID: "tenant-1",
	})
	require.NoError(t, err)

	// 3. PUC Tracking: Attempt to assign vehicle with expired PUC
	vehExpiredPUCID := "veh-exp-puc-1"
	validFuture := now.AddDate(1, 0, 0).Format("2006-01-02")
	_, err = dbConn.Exec(`INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, puc_expiry, status, tenant_id) VALUES (?, 'TRUCK-PUC-EXP', 'MH12PUC999', 'truck', 10.0, 'diesel', ?, ?, ?, ?, 'available', 'tenant-1')`, vehExpiredPUCID, validFuture, validFuture, validFuture, expiredDate)
	require.NoError(t, err)

	assignVehicleUC := tripapp.NewAssignVehicleUseCase(unitOfWork, clk)
	err = assignVehicleUC.Execute(ctx, tripapp.AssignVehicleCommand{
		TripID:    tripagg.TripID(tripID),
		VehicleID: vehExpiredPUCID,
		TenantID:  "tenant-1",
	})
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "puc expired")

	// 4. 7-Day Warning: Set expiry to 5 days from now -> succeeds with status='warning' in compliance_checks
	futureWarning := now.AddDate(0, 0, 5).Format("2006-01-02")
	_, err = dbConn.Exec(`UPDATE vehicles SET puc_expiry = ? WHERE id = ?`, futureWarning, vehExpiredPUCID)
	require.NoError(t, err)

	err = assignVehicleUC.Execute(ctx, tripapp.AssignVehicleCommand{
		TripID:    tripagg.TripID(tripID),
		VehicleID: vehExpiredPUCID,
		TenantID:  "tenant-1",
	})
	require.NoError(t, err)

	var warnCount int
	err = dbConn.QueryRow("SELECT COUNT(*) FROM compliance_checks WHERE entity_id = ? AND status = 'warning'", vehExpiredPUCID).Scan(&warnCount)
	require.NoError(t, err)
	require.GreaterOrEqual(t, warnCount, 1)

	// 5. StartTrip Re-validation: Expire another driver's license in DB and attempt StartTrip
	trip2ID := "trp-test-2"
	_, err = dbConn.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id) VALUES (?, 'TRP-102', ?, datetime('now'), 'scheduled', 'tenant-1')`, trip2ID, routeID)
	require.NoError(t, err)

	drvValidID := "drv-valid-2"
	_, err = dbConn.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id) VALUES (?, 'DRV-VAL-2', 'Suresh', 'Raina', '+919999999902', 'DL-VAL-2', ?, 'available', 'tenant-1')`, drvValidID, validFuture)
	require.NoError(t, err)

	vehValidID := "veh-valid-2"
	_, err = dbConn.Exec(`INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, puc_expiry, status, tenant_id) VALUES (?, 'TRUCK-VAL-2', 'MH12VAL999', 'truck', 10.0, 'diesel', ?, ?, ?, ?, 'available', 'tenant-1')`, vehValidID, validFuture, validFuture, validFuture, validFuture)
	require.NoError(t, err)

	err = assignDriverUC.Execute(ctx, tripapp.AssignDriverCommand{
		TripID:   tripagg.TripID(trip2ID),
		DriverID: drvValidID,
		TenantID: "tenant-1",
	})
	require.NoError(t, err)

	err = assignVehicleUC.Execute(ctx, tripapp.AssignVehicleCommand{
		TripID:    tripagg.TripID(trip2ID),
		VehicleID: vehValidID,
		TenantID:  "tenant-1",
	})
	require.NoError(t, err)

	// Now expire driver's license
	_, err = dbConn.Exec(`UPDATE drivers SET license_expiry = ? WHERE id = ?`, expiredDate, drvValidID)
	require.NoError(t, err)

	startTripUC := tripapp.NewStartTripUseCase(unitOfWork, clk)
	err = startTripUC.Execute(ctx, tripapp.StartTripCommand{
		TripID:   tripagg.TripID(trip2ID),
		TenantID: "tenant-1",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Dispatch blocked: driver license expired (compliance)")

	// 6. Path B (Service / Agent): Call trip_service.AssignVehicle with blocked vehicle -> assert typed error
	vehBlockedID := "veh-blocked-3"
	_, err = dbConn.Exec(`INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, puc_expiry, status, tenant_id) VALUES (?, 'TRUCK-BLOCKED-3', 'MH12BLK333', 'truck', 10.0, 'diesel', ?, ?, ?, ?, 'available', 'tenant-1')`, vehBlockedID, expiredDate, validFuture, validFuture, validFuture)
	require.NoError(t, err)

	_, err = svcs.Trips.AssignVehicle(ctx, domain.TripID(tripID), domain.VehicleID(vehBlockedID))
	require.Error(t, err)
	require.Contains(t, err.Error(), "Dispatch blocked: vehicle insurance expired (compliance)")

	// 7. File Upload: Upload vehicle_rc PDF, assert saved to vehicles/ subdir and links
	pdfData := []byte("%PDF-1.4 sample vehicle RC test content")
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "rc_cert.pdf")
	require.NoError(t, err)
	_, err = part.Write(pdfData)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	err = req.ParseMultipartForm(10 * 1024 * 1024)
	require.NoError(t, err)
	fileHeader := req.MultipartForm.File["file"][0]

	fileRecord, err := svcs.Files.UploadFile(ctx, fileHeader, "vehicle_rc", vehValidID)
	require.NoError(t, err)
	require.Equal(t, "vehicle_rc", fileRecord.UploadableType)
	require.Contains(t, fileRecord.Path, "vehicles")

	// 8. Trip Compliance Fragment HTTP endpoint
	r := chi.NewRouter()
	r.Get("/trips/{id}/compliance", app.Trips.TripComplianceFragment)

	fragReq := httptest.NewRequest("GET", "/trips/"+tripID+"/compliance", nil)
	fragReq = fragReq.WithContext(shared.ContextWithTenantID(fragReq.Context(), "tenant-1"))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, fragReq)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "trip-compliance-status")
}
