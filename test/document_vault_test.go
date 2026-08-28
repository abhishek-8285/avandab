package test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
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

type docTestClock struct{}

func (c *docTestClock) Now() time.Time { return time.Now().UTC() }

func setupDocVaultEnv(t *testing.T) (*sql.DB, *service.Services, *events.InMemoryBus, ports.UnitOfWork, *handlers.App) {
	t.Helper()
	name := fmt.Sprintf("test_doc_env_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
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
	app.Documents = handlers.NewDocumentHandlers(app, svcs.Documents, authSrv)
	app.Compliance = handlers.NewComplianceHandlers(app, svcs.Compliance)

	return dbConn, svcs, bus, unitOfWork, app
}

// 1. Migration 00052 Round-Trip Test
func TestDocumentVault_Migration00052_RoundTrip(t *testing.T) {
	name := fmt.Sprintf("test_doc_mig_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=foreign_keys(OFF)")
	require.NoError(t, err)
	defer db.Close()

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.UpTo(db, "../db/migrations", 52))

	// Verify columns and tables exist
	var count int
	err = db.QueryRow("SELECT count(*) FROM driver_documents").Scan(&count)
	require.NoError(t, err, "driver_documents table must exist")

	err = db.QueryRow("SELECT count(*) FROM vehicle_documents").Scan(&count)
	require.NoError(t, err, "vehicle_documents table must exist")

	// Rollback 1 version (to 51)
	require.NoError(t, goose.DownTo(db, "../db/migrations", 51))

	// Verify driver_documents dropped
	err = db.QueryRow("SELECT count(*) FROM driver_documents").Scan(&count)
	assert.Error(t, err, "driver_documents table should be dropped after down")

	// Reapply 00052
	require.NoError(t, goose.Up(db, "../db/migrations"))
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
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
}

// 2. Vehicle PUC Expiry Persistence & CanAssign Enforcement
func TestDocumentVault_PUCExpiry_PersistenceAndAssignment(t *testing.T) {
	dbConn, _, _, _, _ := setupDocVaultEnv(t)
	repo := sqlite.NewRepository(dbConn)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	pastPUC := time.Now().Add(-10 * 24 * time.Hour)
	futurePUC := time.Now().Add(60 * 24 * time.Hour)

	// Create vehicle with expired PUC
	vExpired := domain.Vehicle{
		ID:                 domain.VehicleID("veh-expired-puc"),
		RegistrationNumber: "MH-01-EXP-1234",
		VehicleNumber:      "MH01EXP1234",
		VehicleType:        domain.VehicleTypeTruck,
		Capacity:           5000,
		FuelType:           domain.FuelTypeDiesel,
		InsuranceExpiry:    time.Now().Add(100 * 24 * time.Hour),
		FitnessExpiry:      time.Now().Add(100 * 24 * time.Hour),
		PermitExpiry:       time.Now().Add(100 * 24 * time.Hour),
		PUCExpiry:          &pastPUC,
		Status:             domain.VehicleAvailable,
	}
	_, err := repo.CreateVehicle(ctx, vExpired)
	require.NoError(t, err)

	// Fetch back
	loaded, err := repo.GetVehicleByID(ctx, vExpired.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded.PUCExpiry)

	// Test CanAssign fails
	err = loaded.CanAssign()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PUC expired")

	// Update to future PUC
	loaded.PUCExpiry = &futurePUC
	_, err = repo.UpdateVehicle(ctx, loaded)
	require.NoError(t, err)

	updated, err := repo.GetVehicleByID(ctx, vExpired.ID)
	require.NoError(t, err)
	assert.NoError(t, updated.CanAssign())
}

// 3. Driver Aadhaar, PAN, Bank Details Persistence
func TestDocumentVault_DriverIdentity_Persistence(t *testing.T) {
	dbConn, _, _, _, _ := setupDocVaultEnv(t)
	repo := sqlite.NewRepository(dbConn)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	aadhaar := "1234-5678-9012"
	pan := "ABCDE1234F"
	bank := "HDFC Bank, AC: 987654321, IFSC: HDFC0001234"

	drv := domain.Driver{
		ID:              domain.DriverID("drv-identity-test"),
		DriverID:        "DRV-ID-001",
		FirstName:       "Ramesh",
		LastName:        "Kumar",
		Phone:           "+919876543210",
		LicenseNumber:   "DL-MH-123456",
		LicenseExpiry:   time.Now().Add(200 * 24 * time.Hour),
		ExperienceYears: 5,
		Status:          domain.DriverAvailable,
		Aadhaar:         &aadhaar,
		PAN:             &pan,
		BankDetails:     &bank,
	}

	_, err := repo.CreateDriver(ctx, drv)
	require.NoError(t, err)

	loaded, err := repo.GetDriverByID(ctx, drv.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded.Aadhaar)
	assert.Equal(t, aadhaar, *loaded.Aadhaar)
	require.NotNil(t, loaded.PAN)
	assert.Equal(t, pan, *loaded.PAN)
	require.NotNil(t, loaded.BankDetails)
	assert.Equal(t, bank, *loaded.BankDetails)

	// Update driver details
	newPAN := "XYZAB9876C"
	loaded.PAN = &newPAN
	_, err = repo.UpdateDriver(ctx, loaded)
	require.NoError(t, err)

	updated, err := repo.GetDriverByID(ctx, drv.ID)
	require.NoError(t, err)
	require.NotNil(t, updated.PAN)
	assert.Equal(t, newPAN, *updated.PAN)
}

// 4. DocumentService Upload & Verification Workflow
func TestDocumentVault_UploadAndVerify(t *testing.T) {
	dbConn, svcs, _, _, _ := setupDocVaultEnv(t)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// Create driver in DB
	driverID := "drv-doc-test-1"
	_, _ = dbConn.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, experience_years, status, tenant_id)
		VALUES (?, 'DRV-1', 'Rajesh', 'Sharma', '+919988776655', 'DL123', '2027-01-01', 4, 'available', '1')`, driverID)

	// Create vehicle in DB
	vehicleID := "veh-doc-test-1"
	_, _ = dbConn.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, puc_expiry, status, tenant_id)
		VALUES (?, 'MH-12-AB-9999', 'MH12AB9999', 'truck', 6000, 'diesel', '2027-01-01', '2027-01-01', '2027-01-01', '2027-01-01', 'available', '1')`, vehicleID)

	// Mock file header for driver doc upload
	header := createTestFileHeader(t, "aadhaar_card.pdf", "%PDF-1.4 test dummy pdf content")
	expDate := time.Now().Add(365 * 24 * time.Hour)

	doc, err := svcs.Documents.UploadDriverDoc(ctx, driverID, "aadhaar", header, &expDate)
	require.NoError(t, err)
	assert.Equal(t, "pending_review", doc.Status)
	assert.Equal(t, "aadhaar", doc.DocType)

	// Verify driver docs listing
	driverDocs, err := svcs.Documents.ListDriverDocs(ctx, driverID)
	require.NoError(t, err)
	require.Len(t, driverDocs, 1)

	// Verify document approval
	err = svcs.Documents.VerifyDocument(ctx, "driver", driverID, doc.ID, "admin-user-1")
	require.NoError(t, err)

	driverDocsAfter, err := svcs.Documents.ListDriverDocs(ctx, driverID)
	require.NoError(t, err)
	assert.Equal(t, "verified", driverDocsAfter[0].Status)
	require.NotNil(t, driverDocsAfter[0].VerifiedBy)
	assert.Equal(t, "admin-user-1", *driverDocsAfter[0].VerifiedBy)

	// Vehicle document upload & verify
	vehHeader := createTestFileHeader(t, "puc_cert.pdf", "%PDF-1.4 test dummy puc content")
	pucDoc, err := svcs.Documents.UploadVehicleDoc(ctx, vehicleID, "puc", vehHeader, &expDate)
	require.NoError(t, err)
	assert.Equal(t, "pending_review", pucDoc.Status)

	err = svcs.Documents.VerifyDocument(ctx, "vehicle", vehicleID, pucDoc.ID, "admin-user-2")
	require.NoError(t, err)

	vehDocs, err := svcs.Documents.ListVehicleDocs(ctx, vehicleID)
	require.NoError(t, err)
	assert.Equal(t, "verified", vehDocs[0].Status)
}

// 5. Compliance Hard Block at Dispatch & Exemption Bypass
func TestDocumentVault_HardDispatchBlockAndExemption(t *testing.T) {
	dbConn, svcs, _, _, _ := setupDocVaultEnv(t)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// Create expired driver and vehicle
	driverID := "drv-exp-lic"
	_, _ = dbConn.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, experience_years, status, tenant_id)
		VALUES (?, 'DRV-EXP', 'Suresh', 'Patel', '+919988776611', 'DL-EXP', '2025-01-01', 4, 'available', '1')`, driverID)

	vehicleID := "veh-exp-puc"
	_, _ = dbConn.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, puc_expiry, status, tenant_id)
		VALUES (?, 'KA-01-ZZ-1234', 'KA01ZZ1234', 'truck', 6000, 'diesel', '2027-01-01', '2027-01-01', '2027-01-01', '2025-01-01', 'available', '1')`, vehicleID)

	// Validate compliance check reports block
	compRes, err := svcs.Compliance.CheckDispatchCompliance(ctx, driverID, vehicleID)
	require.Error(t, err)
	assert.False(t, compRes.Valid)
	assert.True(t, compRes.Blocked)

	// Create exemption for driver license
	err = svcs.Compliance.CreateExemption(ctx, service.ComplianceExemption{
		EntityType:  "driver",
		EntityID:    driverID,
		DocType:     "license",
		Reason:      "Renewal submitted at RTO, receipt provided",
		ExemptUntil: time.Now().Add(14 * 24 * time.Hour),
		CreatedBy:   "admin",
	})
	require.NoError(t, err)

	// Create exemption for vehicle PUC
	err = svcs.Compliance.CreateExemption(ctx, service.ComplianceExemption{
		EntityType:  "vehicle",
		EntityID:    vehicleID,
		DocType:     "puc",
		Reason:      "PUC machine down at emission center, scheduled for tomorrow",
		ExemptUntil: time.Now().Add(2 * 24 * time.Hour),
		CreatedBy:   "admin",
	})
	require.NoError(t, err)

	// Now check compliance again — should pass with warning alerts
	compResAfter, err := svcs.Compliance.CheckDispatchCompliance(ctx, driverID, vehicleID)
	require.NoError(t, err)
	assert.True(t, compResAfter.Valid)
	assert.False(t, compResAfter.Blocked)
	assert.NotEmpty(t, compResAfter.Alerts)
}

// 6. StartTrip Re-Validation Gate
func TestDocumentVault_StartTripComplianceGate(t *testing.T) {
	dbConn, _, _, unitOfWork, _ := setupDocVaultEnv(t)
	tenantID := shared.TenantID("tenant-1")
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")

	routeID := "rt-vault-1"
	_, _ = dbConn.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES (?, 'Mumbai', 'Pune', 150, 4, 5000, 'tenant-1')`, routeID)

	// Create valid trip, driver, vehicle
	tripID := "trp-vault-gate-1"
	driverID := "drv-start-001"
	vehicleID := "veh-start-001"

	_, _ = dbConn.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, experience_years, status, tenant_id)
		VALUES (?, 'DRV-START', 'Anil', 'Verma', '+919988776622', 'DL-START', '2025-01-01', 4, 'available', 'tenant-1')`, driverID)

	_, _ = dbConn.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, puc_expiry, status, tenant_id)
		VALUES (?, 'DL-01-AA-5555', 'DL01AA5555', 'truck', 6000, 'diesel', '2027-01-01', '2027-01-01', '2027-01-01', '2027-01-01', 'available', 'tenant-1')`, vehicleID)

	_, _ = dbConn.Exec(`INSERT INTO trips (id, trip_number, route_id, driver_id, vehicle_id, departure_time, status, tenant_id)
		VALUES (?, 'TRIP-V001', ?, ?, ?, datetime('now'), 'assigned', 'tenant-1')`, tripID, routeID, driverID, vehicleID)

	// Instantiate StartTripUseCase
	clock := &docTestClock{}
	startUC := tripapp.NewStartTripUseCase(unitOfWork, clock)

	// Execute StartTrip -> should fail because driver license is expired
	err := startUC.Execute(ctx, tripapp.StartTripCommand{
		TripID:   tripagg.TripID(tripID),
		TenantID: tenantID,
	})
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "dispatch blocked")

	// Update driver license to future date
	_, _ = dbConn.Exec(`UPDATE drivers SET license_expiry = '2027-01-01' WHERE id = ?`, driverID)

	// Execute StartTrip again -> should succeed
	err = startUC.Execute(ctx, tripapp.StartTripCommand{
		TripID:   tripagg.TripID(tripID),
		TenantID: tenantID,
	})
	require.NoError(t, err)
}

// 7. HTTP API Endpoints & Compliance Dashboard JSON
func TestDocumentVault_HTTP_Endpoints_And_Dashboard(t *testing.T) {
	dbConn, svcs, _, _, _ := setupDocVaultEnv(t)

	authSvc := &mockPhase6Auth{allowed: true}
	app := &handlers.App{DB: dbConn, AuthSrv: authSvc, Services: svcs}
	app.Documents = handlers.NewDocumentHandlers(app, svcs.Documents, authSvc)
	app.Compliance = handlers.NewComplianceHandlers(app, svcs.Compliance)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := context.WithValue(req.Context(), auth.ContextUser, &auth.SessionData{
				UserID: "admin-user",
				Role:   "admin",
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})

	app.Documents.Mount(r)
	app.Compliance.Mount(r)

	// 1. Test GET /api/compliance/dashboard
	req := httptest.NewRequest(http.MethodGet, "/api/compliance/dashboard", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var dash handlers.ComplianceDashboardData
	err := json.NewDecoder(rec.Body).Decode(&dash)
	require.NoError(t, err)

	// 2. Test GET /api/documents/driver/{driver_id}
	req2 := httptest.NewRequest(http.MethodGet, "/api/documents/driver/drv-123", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusOK, rec2.Code)
}

// Helpers
func createTestFileHeader(t *testing.T, filename, content string) *multipart.FileHeader {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	h.Set("Content-Type", "application/pdf")
	part, err := writer.CreatePart(h)
	require.NoError(t, err)
	_, err = part.Write([]byte(content))
	require.NoError(t, err)
	_ = writer.Close()

	reader := multipart.NewReader(&body, writer.Boundary())
	form, err := reader.ReadForm(10 << 20)
	require.NoError(t, err)
	files := form.File["file"]
	require.NotEmpty(t, files)
	return files[0]
}
