package handlers

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/agent/rl"
	alertsdomain "transport-app/internal/alerts/domain"
	"transport-app/internal/auth"
	bookingapp "transport-app/internal/booking/application"
	"transport-app/internal/domain"
	driverapp "transport-app/internal/driver/application"
	"transport-app/internal/fastag"
	"transport-app/internal/features"
	geofencedomain "transport-app/internal/geofence/domain"
	invoiceapp "transport-app/internal/invoice/application"
	maintenancedomain "transport-app/internal/maintenance/domain"
	opserrors "transport-app/internal/operations/errors"
	"transport-app/internal/service"
	"transport-app/internal/shared"
	"transport-app/internal/telemetry"
	tripapp "transport-app/internal/trip/application"
	vehicleapp "transport-app/internal/vehicle/application"
)

// TestAllTemplatesRender executes every registered template with representative
// data and fails on any render error (type mismatches, missing fields, nil
// pointer traversal, syntax errors). Covers the edit forms, auth flows,
// fuel/telemetry/maintenance/geofence UIs, dashboards and partials that the
// list/view template tests did not exercise.
func TestAllTemplatesRender(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		_ = os.Chdir("../..")
	}

	tmpl, err := parseTemplates(&mockAuthSvc{})
	require.NoError(t, err)

	now := time.Now()
	sPtr := func(s string) *string { return &s }
	fPtr := func(f float64) *float64 { return &f }

	user := &auth.SessionData{UserID: "u-1", Role: "admin", Name: "Admin User"}

	sampleClaim := service.FuelAuditClaim{
		ExpenseID:       "exp-1",
		TripID:          "trip-1",
		TripNumber:      "TRP-001",
		DriverID:        "drv-1",
		DriverName:      "John Doe",
		VehicleID:       "veh-1",
		VehicleReg:      "MH12AB1234",
		Category:        "fuel",
		Amount:          5000,
		FuelLitres:      fPtr(40.0),
		Status:          "pending",
		AuditStatus:     "pending",
		ClaimedLitres:   40,
		ExpectedLevel:   fPtr(38.5),
		ExpectedOdo:     fPtr(45200),
		ExpectedBest:    38.5,
		VarianceLitres:  1.5,
		VariancePct:     3.9,
		Result:          "passed",
		KmplUsed:        4.2,
		OdometerDeltaKm: 180,
		TankCapacity:    200,
		LevelDeltaPct:   2.1,
		CreatedAt:       now,
	}

	sampleExpense := service.KharchaExpense{
		ID:          "exp-1",
		TripID:      "trip-1",
		TripNumber:  "TRP-001",
		DriverID:    "drv-1",
		DriverName:  "John Doe",
		Category:    "fuel",
		Amount:      5000,
		Description: "Diesel refill",
		ReceiptURL:  sPtr("/files/1"),
		Status:      "pending",
		AuditStatus: "pending",
		FuelLitres:  fPtr(40.0),
		CreatedAt:   now,
	}

	sampleDevice := telemetry.Device{
		ID:         "dev-1",
		TenantID:   string(shared.DefaultTenant),
		IMEI:       "123456789012345",
		DeviceType: telemetry.DeviceTypeHardware,
		Status:     telemetry.DeviceStatusAssigned,
		VehicleID:  sPtr("veh-1"),
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	sampleQuarantine := telemetry.QuarantineEntry{
		ID:         "q-1",
		TenantID:   string(shared.DefaultTenant),
		IMEI:       "123456789012345",
		Source:     "http_ingest",
		RawPayload: `{"imei":"123456789012345"}`,
		Reason:     telemetry.QuarantineReasonUnknownDevice,
		Status:     telemetry.QuarantineStatusOpen,
		CreatedAt:  now,
	}

	sampleZone := geofencedomain.Geofence{
		ID:        "geo-1",
		TenantID:  string(shared.DefaultTenant),
		Name:      "Warehouse A",
		Kind:      geofencedomain.KindDepot,
		Shape:     geofencedomain.ShapeCircle,
		CenterLat: 19.076,
		CenterLng: 72.8777,
		RadiusM:   500,
		RouteName: "MUM-PUN",
		Priority:  1,
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	mapConfig := map[string]interface{}{
		"Provider":    "auto",
		"GoogleStyle": "m",
		"GL":          "IN",
		"OSMUrl":      "https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png",
		"PollSec":     10,
	}

	sampleVehicleDTO := vehicleapp.VehicleResponseDTO{
		ID:                 "veh-1",
		RegistrationNumber: "MH12AB1234",
		VehicleNumber:      "V1",
		VehicleType:        "truck",
		Capacity:           5000,
		FuelType:           "diesel",
		InsuranceExpiry:    now,
		FitnessExpiry:      now,
		PermitExpiry:       now,
		Status:             "available",
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	feature, _ := GetFeature("dashboard")

	tests := []struct {
		name     string
		template string
		data     interface{}
	}{
		// ---- Auth flows ----
		{"login_form", "login_form.html", map[string]interface{}{
			"Email": "", "Error": "", "FlashSuccess": "", "Redirect": "",
		}},
		{"register_form", "register_form.html", map[string]interface{}{
			"Name": "", "Email": "", "Phone": "", "Error": "",
		}},
		{"forgot_password", "forgot_password.html", map[string]interface{}{
			"Error": "", "SuccessMsg": "",
		}},
		{"reset_password", "reset_password.html", map[string]interface{}{
			"Token": "reset-tok-123", "Error": "",
		}},
		{"change_password", "change_password.html", map[string]interface{}{
			"Error": "",
		}},

		// ---- Fuel audit ----
		{"fuel_audit_dashboard", "fuel_audit_dashboard.html", map[string]interface{}{
			"AuditClaims": []service.FuelAuditClaim{sampleClaim},
			"AuditStats":  service.FuelAuditStats{PendingCount: 1, PassedCount: 1, AvgVariancePct: 3.9, EnforceMode: true},
		}},
		{"fuel_audit_detail", "fuel_audit_detail.html", map[string]interface{}{
			"Claim": sampleClaim,
		}},
		{"fuel_audit_queue", "fuel_audit_queue.html", map[string]interface{}{
			"AuditClaims": []service.FuelAuditClaim{sampleClaim},
		}},
		{"fuel_kmpl_report", "fuel_kmpl_report.html", map[string]interface{}{
			"KmplRows": []service.KmplRow{{
				VehicleID: "veh-1", RegistrationNo: "MH12AB1234",
				OdometerDeltaKm: 180, RefillLitres: 40, ComputedKmpl: 4.5,
				ConfiguredKmpl: 4.0, VariancePct: 12.5, TripCount: 3,
			}},
			"From": "2026-07-20", "To": "2026-08-20",
		}},

		// ---- Telemetry devices ----
		{"telemetry_devices", "telemetry_devices.html", map[string]interface{}{
			"Devices":      []telemetry.Device{sampleDevice},
			"Pagination":   PaginationData{Page: 1, PerPage: 20, Total: 1, TotalPages: 1, BasePath: "/telemetry/devices"},
			"Query":        "",
			"StatusFilter": "",
			"User":         user,
		}},
		{"telemetry_device_row", "telemetry_device_row.html", map[string]interface{}{
			"Devices":      []telemetry.Device{sampleDevice},
			"Pagination":   PaginationData{Page: 1, PerPage: 20, Total: 1, TotalPages: 1, BasePath: "/telemetry/devices"},
			"Query":        "",
			"StatusFilter": "",
			"User":         user,
		}},
		{"telemetry_devices_register", "telemetry_devices_register.html", map[string]interface{}{}},
		{"telemetry_register_result", "telemetry_register_result.html", map[string]interface{}{
			"Results": []telemetry.BulkRegisterResult{
				{IMEI: "123456789012345", Success: true, DeviceID: "dev-1"},
				{IMEI: "999999999999999", Success: false, Error: "duplicate"},
			},
			"Success": 1, "Total": 2,
		}},
		{"telemetry_device_secret", "telemetry_device_secret.html", map[string]interface{}{
			"Device":    &sampleDevice,
			"RawSecret": "SECRET-RAW-123",
		}},
		{"telemetry_quarantine_queue", "telemetry_quarantine_queue.html", map[string]interface{}{
			"Entries": []telemetry.QuarantineEntry{sampleQuarantine},
			"User":    user,
		}},
		{"telemetry_quarantine_row", "telemetry_quarantine_row.html", map[string]interface{}{
			"Entries": []telemetry.QuarantineEntry{sampleQuarantine},
			"User":    user,
		}},

		// ---- Kharcha ----
		{"kharcha_dashboard", "kharcha_dashboard.html", map[string]interface{}{
			"PendingExpenses": []service.KharchaExpense{sampleExpense},
			"LedgerEntries":   []service.KharchaExpense{sampleExpense},
			"Stats":           service.KharchaStats{PendingCount: 1, ApprovedToday: 2, MonthTotal: 15000, UnsettledTotal: 3000},
			"ActiveTrips":     []tripapp.TripResponseDTO{{ID: "trip-1", TripNumber: "TRP-001", Status: "in_transit", DepartureTime: now}},
			"Drivers":         []driverapp.DriverResponseDTO{{ID: "drv-1", FirstName: "John", LastName: "Doe"}},
		}},
		{"kharcha_queue", "kharcha_queue.html", map[string]interface{}{
			"PendingExpenses": []service.KharchaExpense{sampleExpense},
		}},
		{"kharcha_ledger_rows", "kharcha_ledger_rows.html", map[string]interface{}{
			"LedgerEntries": []service.KharchaExpense{sampleExpense},
		}},

		// ---- Maintenance ----
		{"maintenance_index", "maintenance_index.html", map[string]interface{}{
			"DueVehicles": []map[string]interface{}{{
				"ID": "veh-1", "VehicleNumber": "V1", "Registration": "MH12AB1234",
				"VehicleType": "truck", "DueDate": "2026-08-25", "IsOverridden": false,
			}},
			"Schedules": []maintenancedomain.Schedule{{
				ID: "s-1", VehicleID: "veh-1", ServiceType: "oil_change", Active: true,
				IntervalKM: fPtr(10000), IntervalDays: nil, LastDoneAt: nil, LastDoneKM: fPtr(25000),
				CreatedAt: now, UpdatedAt: now,
			}},
			"DTCs": []maintenancedomain.DtcEvent{{
				ID: "d-1", VehicleID: "veh-1", DtcCode: "P0420", Severity: "warning",
				Description: sPtr("Catalyst efficiency"), OccurredAt: now, CreatedAt: now,
			}},
			"User": user,
		}},
		{"maintenance_schedule_form", "maintenance_schedule_form.html", map[string]interface{}{
			"VehicleID":    "veh-1",
			"ServiceTypes": maintenancedomain.ServiceTypes,
		}},
		{"maintenance_record_form", "maintenance_record_form.html", map[string]interface{}{
			"VehicleID":    "veh-1",
			"ServiceTypes": maintenancedomain.ServiceTypes,
		}},

		// ---- Geofences ----
		{"geofence_list", "geofence_list.html", map[string]interface{}{
			"Zones":        []geofencedomain.Geofence{sampleZone},
			"User":         user,
			"FlashError":   "",
			"FlashSuccess": "",
		}},
		{"geofence_row", "geofence_row.html", map[string]interface{}{
			"Zones": []geofencedomain.Geofence{sampleZone},
			"User":  user,
		}},
		{"geofence_edit", "geofence_edit.html", map[string]interface{}{
			"Zone":        &sampleZone,
			"PolygonJSON": "",
			"FlashError":  "",
		}},

		// ---- Shares and tracking ----
		{"shares_list", "shares_list.html", map[string]interface{}{
			"Shares": []ShareLinkItem{{
				ID: "s-1", TripID: "trip-1", TripNumber: "TRP-001", HasPIN: true,
				CreatorName: "Admin", CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
				ViewCount: 3, Status: "active",
			}},
		}},
		{"share_pin_form", "share_pin_form.html", map[string]interface{}{
			"Token": "tok-1", "TripNumber": "TRP-001", "IsLocked": false,
			"LockSeconds": 0, "Error": "", "Version": AppVersion,
		}},
		{"share_public", "share_public.html", map[string]interface{}{
			"Token": "tok-1", "TripNumber": "TRP-001", "TripStatus": "in_transit",
			"DataEndpoint": "/share/tok-1/data", "MapConfig": mapConfig, "Version": AppVersion,
		}},
		{"tracking", "tracking.html", map[string]interface{}{
			"MapAssets":    true,
			"MapConfig":    mapConfig,
			"LiveEndpoint": "/api/v1/telemetry/live",
		}},

		// ---- Settings and profile ----
		{"settings", "settings.html", map[string]interface{}{
			"Settings": domain.CompanySettings{
				CompanyName: "Avandab", Currency: "INR", Timezone: "Asia/Kolkata",
				GSTEnabled: true, GSTRate: 18, BookingPrefix: "BK", TripPrefix: "TRP",
				InvoicePrefix: "INV", GSTNumber: sPtr("27AAACP0000M1Z9"),
			},
			"User": user,
		}},
		{"company_onboard", "company_onboard.html", map[string]interface{}{
			"Settings": domain.CompanySettings{CompanyName: "Avandab"},
			"User":     user,
		}},
		{"profile_page", "profile_page.html", map[string]interface{}{
			"User": user,
			"UserDetail": domain.User{
				ID: "u-1", Name: "Admin User", Email: "admin@example.com",
				Role: domain.Role{Name: domain.RoleAdmin}, Status: domain.UserStatusActive,
				LastLoginAt: &now, CreatedAt: now, UpdatedAt: now,
			},
			"Roles": []domain.Role{{Name: domain.RoleAdmin}},
		}},

		// ---- Reports ----
		{"reports_index", "reports_index.html", map[string]interface{}{}},
		{"report_revenue", "report_revenue.html", map[string]interface{}{
			"MonthlyRevenue": []map[string]interface{}{{"Month": "2026-07", "Total": 150000.0}},
			"TotalRevenue":   150000.0,
			"Total":          1,
			"QueryString":    "",
			"ShowPDFExport":  true,
			"User":           user,
		}},

		// ---- FASTag ----
		{"fastag_index", "fastag_index.html", map[string]interface{}{
			"Tags": []fastag.TagRecord{{
				ID: "t-1", TagID: "FASTAG-001", VehicleNumber: "MH12AB1234",
				Issuer: "ICICI", TagClass: "truck", Balance: 1200, Status: "active",
				LastSync: now, LastSyncStr: "2026-08-20 10:00",
			}},
			"Transactions": []fastag.TransactionRecord{{
				ID: "tx-1", TagID: "FASTAG-001", VehicleNumber: "MH12AB1234",
				TripID: sPtr("trip-1"), PlazaID: "P-1", PlazaName: "Vashi Toll",
				Amount: 450, TxnTimestamp: now, TxnTimeStr: "2026-08-20 09:00", Reconciled: true,
			}},
			"TotalTags":        1,
			"TotalBalance":     1200.0,
			"PendingReconcile": 0,
			"User":             user,
		}},
		{"fastag_transactions", "fastag_transactions.html", []fastag.TransactionRecord{{
			ID: "tx-1", TagID: "FASTAG-001", VehicleNumber: "MH12AB1234",
			TripID: sPtr("trip-1"), PlazaID: "P-1", PlazaName: "Vashi Toll",
			Amount: 450, TxnTimestamp: now, TxnTimeStr: "2026-08-20 09:00", Reconciled: false,
		}}},

		// ---- E-POD / documents / onboarding ----
		{"epod_success", "epod_success.html", map[string]interface{}{
			"TripNumber": "TRP-001",
		}},
		{"documents_upload", "documents_upload.html", map[string]interface{}{}},
		{"user_onboarding", "user_onboarding.html", map[string]interface{}{
			"User":       user,
			"UserDetail": domain.User{Name: "Admin User"},
			"FlashError": "",
		}},

		// ---- Static / public pages ----
		{"home", "home.html", map[string]interface{}{"Version": AppVersion}},
		{"privacy", "privacy.html", map[string]interface{}{"Version": AppVersion}},
		{"terms", "terms.html", map[string]interface{}{"Version": AppVersion}},
		{"refunds", "refunds.html", map[string]interface{}{"Version": AppVersion}},
		{"feature", "feature.html", map[string]interface{}{
			"Feature": feature, "RelatedFeatures": []FeatureContent{}, "Version": AppVersion,
		}},
		{"contact", "contact.html", map[string]interface{}{
			"User": user, "Ticket": nil, "SearchErr": "", "SearchQuery": "",
			"SearchEmail": "", "SubmittedNum": "", "SubmittedEmail": "",
		}},
		{"error", "error.html", map[string]interface{}{
			"StatusCode": 500, "Title": "Test Error", "Message": "boom",
		}},

		// ---- Alerts / agent / compliance ----
		{"alerts_list", "alerts_list.html", map[string]interface{}{
			"Alerts": []alertsdomain.Alert{{
				ID: "a-1", Source: "fuel_engine", AlertType: "fuel_fraud", Severity: "high",
				Status: "open", Title: "Fuel variance", Message: "Claim exceeds tolerance",
				Occurrences: 3, LastSeenAt: now, FirstSeenAt: now,
			}},
			"CurrentStatus": "",
			"User":          user,
		}},
		{"agent_actions", "agent_actions.html", map[string]interface{}{
			"Actions": []rl.Action{{
				ID: "act-1", EpisodeID: "ep-1", ToolName: "create_booking",
				ArgsJSON: `{"customer_id":"c-1"}`, Summary: "Create booking for ACME",
				Status: "pending", RequestedBy: "agent", CreatedAt: now.Format(time.RFC3339),
			}},
			"User": user,
		}},
		{"assistant", "assistant.html", map[string]interface{}{
			"User": user,
		}},
		{"compliance_dashboard", "compliance_dashboard.html", map[string]interface{}{
			"Data": ComplianceDashboardData{
				Drivers:          ComplianceEntityMetrics{Total: 10, Blocked: 1, ExpiringSoon: 2},
				Vehicles:         ComplianceEntityMetrics{Total: 5, Blocked: 0, ExpiringSoon: 1},
				BlockedDrivers:   []BlockedEntity{{ID: "drv-1", Reason: "license_expired"}},
				BlockedVehicles:  []BlockedEntity{{ID: "veh-1", Reason: "insurance_expired"}},
				DocumentsPending: 3,
			},
			"User": user,
		}},

		// ---- View pages ----
		{"customer_view", "customer_view.html", map[string]interface{}{
			"Customer": domain.Customer{
				ID: "c-1", Name: "ACME Corp", Phone: "9999999999",
				Company: sPtr("ACME Ltd"), Email: sPtr("acme@example.com"),
				GST: sPtr("27AAACP0000M1Z9"), Address: sPtr("Mumbai"),
				Status: "active", CreatedAt: now, UpdatedAt: now,
			},
		}},
		{"vehicle_view", "vehicle_view.html", map[string]interface{}{
			"Vehicle":                   sampleVehicleDTO,
			"MaintenanceDue":            "",
			"MaintenanceOverrideBy":     "",
			"MaintenanceOverrideReason": "",
			"IsMaintenanceDue":          false,
			"IsMaintenanceOverridden":   false,
			"User":                      user,
		}},

		// ---- Edit forms ----
		{"booking_edit", "booking_edit.html", map[string]interface{}{
			"Booking": bookingapp.BookingResponseDTO{
				ID: "b-1", BookingNumber: "BK-001", CustomerID: "c-1",
				RouteID: "r-1", PickupDate: now, VehicleType: "truck",
				Passengers: 1, Price: 10000, Notes: "", Status: "draft",
			},
			"Customers": []domain.Customer{{ID: "c-1", Name: "ACME Corp", Phone: "9999999999"}},
			"Routes":    []domain.Route{{ID: "r-1", Source: "Mumbai", Destination: "Pune"}},
		}},
		{"customer_edit", "customer_edit.html", map[string]interface{}{
			"Customer": domain.Customer{ID: "c-1", Name: "ACME Corp", Phone: "9999999999"},
		}},
		{"driver_edit", "driver_edit.html", map[string]interface{}{
			"Driver": driverapp.DriverResponseDTO{
				ID: "drv-1", FirstName: "John", LastName: "Doe", Phone: "555-1234",
				LicenseNumber: "LIC-001", LicenseExpiry: now, ExperienceYears: 5,
				Status: "available", CreatedAt: now, UpdatedAt: now,
			},
		}},
		{"vehicle_edit", "vehicle_edit.html", map[string]interface{}{
			"Vehicle":                   sampleVehicleDTO,
			"MaintenanceDue":            "",
			"MaintenanceOverrideBy":     "",
			"MaintenanceOverrideReason": "",
			"IsMaintenanceDue":          false,
			"IsMaintenanceOverridden":   false,
		}},
		{"trip_edit", "trip_edit.html", map[string]interface{}{
			"Trip": tripapp.TripResponseDTO{
				ID: "trip-1", TripNumber: "TRP-001", RouteSource: "Mumbai",
				RouteDestination: "Pune", DepartureTime: now, Status: "scheduled",
			},
			"Drivers":           []driverapp.DriverResponseDTO{{ID: "drv-1", FirstName: "John", LastName: "Doe"}},
			"Vehicles":          []vehicleapp.VehicleResponseDTO{sampleVehicleDTO},
			"Routes":            []domain.Route{{ID: "r-1", Source: "Mumbai", Destination: "Pune"}},
			"SelectedBookingID": "",
			"SelectedDriverID":  "",
			"SelectedVehicleID": "",
		}},
		{"route_edit", "route_edit.html", map[string]interface{}{
			"Route": domain.Route{
				ID: "r-1", Source: "Mumbai", Destination: "Pune",
				Distance: 150, EstimatedHours: 3.5, StandardFare: 4500,
				Direction: "bidirectional", IsActive: true,
			},
		}},
		{"user_edit", "user_edit.html", map[string]interface{}{
			"User":       user,
			"UserDetail": domain.User{ID: "u-1", Name: "Admin", Email: "a@b.com"},
			"Roles":      []domain.Role{{Name: domain.RoleAdmin}},
		}},
		{"payment_edit", "payment_edit.html", map[string]interface{}{
			"Invoice":       invoiceapp.InvoiceResponseDTO{ID: "inv-1", InvoiceNumber: "INV-001", Total: 1180},
			"Balance":       180.0,
			"Now":           now,
			"InvoiceID":     "inv-1",
			"InvoiceNumber": "INV-001",
		}},

		// ---- Layouts ----
		{"layout", "layout.html", map[string]interface{}{
			"Title": "Test", "Content": template.HTML("<p>content</p>"),
			"User": user, "Query": "", "Notifications": nil, "UnreadCount": 0,
			"HasUnread": false, "FlashError": "", "FlashSuccess": "",
			"Version": AppVersion, "PWAEnabled": false,
			"Extra": map[string]interface{}{},
		}},
		{"auth_layout", "auth_layout.html", map[string]interface{}{
			"Title": "Test", "Content": template.HTML("<p>content</p>"),
			"User": user, "FlashError": "", "FlashSuccess": "", "Version": AppVersion,
		}},

		// ---- Partials ----
		{"partial_alert", "alert.html", map[string]interface{}{"Tone": "error", "Title": "Oops", "Message": "boom"}},
		{"partial_badge", "badge.html", map[string]interface{}{"Label": "New", "Tone": "info"}},
		{"partial_btn", "btn.html", map[string]interface{}{"Label": "Save", "Type": "submit", "Icon": "save", "Href": "/", "Variant": "primary", "Size": "md", "Class": ""}},
		{"partial_cookie_consent", "cookie-consent", map[string]interface{}{}},
		{"partial_empty_state", "empty_state.html", map[string]interface{}{"Icon": "inbox", "Title": "Nothing here", "Message": "", "ActionHref": "", "ActionLabel": ""}},
		{"partial_ewaybill_card", "ewaybill_card.html", map[string]interface{}{"User": user, "Trip": map[string]string{"ID": "trip-1"}, "TripID": "trip-1", "EWayBill": nil}},
		{"partial_ewaybill_row", "ewaybill_row.html", map[string]interface{}{"EwbNumber": "EWB-1", "TripID": "trip-1", "TripNumber": "TRP-001"}},
		{"partial_field", "field.html", map[string]interface{}{"Label": "Name", "Required": "true", "Type": "text", "Name": "name", "Value": "x"}},
		{"partial_footer", "footer.html", map[string]interface{}{}},
		{"partial_irn_qr", "irn_qr.html", map[string]interface{}{"Invoice": invoiceapp.InvoiceResponseDTO{IRN: "irn", IRNAckNo: "ack", IRNAckDate: "2026-08-20", SignedQR: "data:image/png;base64,x"}}},
		{"partial_page_header", "page_header.html", map[string]interface{}{"Title": "Page", "Subtitle": "sub", "ActionHref": "/", "ActionIcon": "add"}},
		{"partial_pagination", "pagination.html", map[string]interface{}{"Pagination": PaginationData{Page: 1, PerPage: 20, Total: 5, TotalPages: 1, HasPrev: false, HasNext: false, BasePath: "/items"}}},
		{"partial_public_header", "public_header.html", map[string]interface{}{}},
		{"partial_stat_card", "stat_card.html", map[string]interface{}{"Label": "Trips", "Value": "12", "Accent": "success", "Icon": "route"}},
		{"partial_status_dot", "status_dot.html", map[string]interface{}{"Tone": "success", "Label": "Active"}},
		{"partial_tax_split", "tax_split.html", map[string]interface{}{"TaxableTotal": 1000.0, "IsIntraState": true, "Cgst": 90.0, "Sgst": 90.0, "Igst": 0.0, "Total": 1180.0}},
		{"partial_theme_head", "theme_head.html", map[string]interface{}{}},

		// Shared layout blocks defined by page templates
		// ---- Phase-12 pages ----
		{"founder_dashboard", "founder_dashboard.html", map[string]interface{}{
			"UnacknowledgedSignals": 2,
			"ActiveExperiments":     3,
			"OpenOpsAlerts":         1,
			"LatestPNL": map[string]interface{}{
				"SnapshotDate": "2026-08-19", "Revenue": 120000.0, "Expenses": 95000.0,
				"NetProfit": 25000.0, "TripCount": 42,
			},
			"RecentSignals": []map[string]interface{}{{
				"ID": "sig-1", "SignalType": "fuel_spend", "SignalValue": 123.45,
				"ThresholdValue": 100.0, "Direction": "above",
			}},
			"User": user,
		}},
		{"pnl_dashboard", "pnl_dashboard.html", map[string]interface{}{
			"Snapshots": []map[string]interface{}{{
				"SnapshotDate": "2026-08-19", "Revenue": 120000.0, "Expenses": 95000.0,
				"DriverPayouts": 30000.0, "FuelCosts": 20000.0, "NetProfit": 25000.0, "TripCount": 42,
			}},
			"From": "2026-07-20", "To": "2026-08-20", "User": user,
		}},
		{"ops_alerts_list", "ops_alerts_list.html", map[string]interface{}{
			"Alerts": []map[string]interface{}{{
				"ID": "a-1", "AlertType": "fuel_variance", "Severity": "high",
				"Title": "Fuel variance detected", "Status": "open", "CreatedAt": now,
			}},
			"User": user,
		}},
		{"errors", "errors.html", map[string]interface{}{
			"Errors": []opserrors.ErrorReport{{
				ID: "err-1", Fingerprint: "fp1234567890abcdef", Timestamp: now, RequestID: "req-abc",
				URL: "/api/v1/bookings", Method: "POST", StatusCode: 500,
				Message: "insert failed\nat db.go:1", Severity: opserrors.SeverityHigh,
				Occurrences: 3, FirstSeen: now,
			}},
			"Incidents": []opserrors.Incident{{
				ID: "inc-1", ErrorID: "err-1", Status: "OPEN",
				Severity: opserrors.SeverityCritical, Created: now,
			}},
			"Total": 1, "OpenIncidents": 1, "Severity": "", "Fingerprint": "",
			"From": "", "To": "",
			"Pagination": PaginationData{
				Page: 1, PerPage: 20, Total: 40, TotalPages: 2,
				HasPrev: false, HasNext: true, BasePath: "/ops/errors",
			},
			"User": user,
		}},
		{"kpi_grid", "kpi_grid.html", map[string]interface{}{
			"KPIs": []KPI{
				{Label: "Total Bookings", Value: "42", Sub: "12 new this month · ₹3.4 L"},
				{Label: "Pending", Value: "7", Accent: "text-status-warning"},
				{Label: "Confirmed", Value: "11", Accent: "text-status-info"},
				{Label: "Completed", Value: "24", Accent: "text-status-success"},
			},
		}},
		{"features_admin", "features_admin.html", map[string]interface{}{
			"Categories": map[string][]features.SnapshotEntry{
				"Operations": {
					{Feature: features.Feature{Key: "telemetry", Name: "GPS Telemetry & Live Tracking", Category: "Operations", Tier: features.TierAddon, EnvFlag: "TELEMETRY_ENABLED"}, Enabled: true},
					{Feature: features.Feature{Key: "share_links", Name: "Public Trip Share Links", Category: "Operations", Tier: features.TierCore}, Enabled: true},
				},
			},
			"Order": []string{"Operations"},
			"User":  user,
		}},
		{"search_results", "search_results.html", map[string]interface{}{
			"Query": "MH01",
			"Sections": []SearchSection{{
				Key: "vehicles", Label: "Vehicles", Total: 2,
				Rows: []SearchRow{
					{ID: "v-1", Title: "MH01AB1234", Sub: "Tata Ace", Href: "/vehicles/v-1"},
					{ID: "v-2", Title: "MH01CD5678", Sub: "Eicher Pro", Href: "/vehicles/v-2"},
				},
			}},
			"User": user,
		}},
		{"experiments_list", "experiments_list.html", map[string]interface{}{
			"Experiments": []map[string]interface{}{{
				"ID": "exp-1", "Name": "Booking flow v2", "Status": "running",
				"MetricName": "conversion", "TrafficSplit": 0.5, "CreatedAt": now,
			}},
			"User": user,
		}},
		{"kharcha_row_approved", "kharcha_row_approved.html", service.KharchaExpense{
			ID: "exp-1", DriverName: "John Doe", Amount: 500.0, Category: "fuel",
			Status: "approved", CreatedAt: now,
		}},
		{"kharcha_row_rejected", "kharcha_row_rejected.html", service.KharchaExpense{
			ID: "exp-1", DriverName: "John Doe", Amount: 500.0, Category: "fuel",
			Status: "rejected", CreatedAt: now,
		}},
		{"cookie_consent", "cookie-consent", map[string]interface{}{}},
	}

	covered := map[string]bool{}
	for _, tt := range tests {
		covered[tt.template] = true
		t.Run(tt.name, func(t *testing.T) {
			tpl := tmpl.Lookup(tt.template)
			require.NotNilf(t, tpl, "template %s not found", tt.template)
			var buf bytes.Buffer
			require.NoErrorf(t, tpl.Execute(&buf, tt.data), "template %s failed to render", tt.template)
			assert.NotEmpty(t, buf.String(), "template %s rendered empty output", tt.template)
		})
	}

	// Every registered template must be covered so newly added templates
	// fail this test until they get a representative data case. Templates
	// exercised by TestAllTemplatesRenderCleanly are exempted.
	for _, n := range []string{
		"audit_logs_list.html", "audit_logs_list_table.html", "cookie-consent.html",
		"booking_list.html", "booking_list_table.html", "booking_view.html",
		"customer_list.html", "customer_list_table.html", "customer_bookings.html", "customer_invoices.html", "customer_tracking.html", "trip_feedback.html", "dashboard.html",
		"driver_list.html", "driver_list_table.html", "driver_view.html",
		"ewaybill_detail.html", "ewaybill_index.html", "invoice_line_items.html",
		"invoice_list.html", "invoice_list_table.html", "invoice_view.html",
		"map.html", "payment_list.html", "payment_list_table.html", "payment_view.html",
		"report_customers.html", "report_drivers.html", "report_pending_payments.html",
		"report_trips.html", "report_vehicles.html", "route_list.html",
		"route_list_table.html", "route_view.html", "route_optimize.html", "route_optimize_jobs.html", "scorecard_driver.html",
		"scorecard_leaderboard.html", "scorecard_table.html", "settlement_list.html",
		"settlement_view.html", "trip_list.html", "trip_list_table.html", "trip_view.html",
		"user_list.html", "user_list_table.html", "vehicle_list.html", "vehicle_list_table.html",
		"console.html", "alert_inbox.html", "money_strip.html",
		"fleet_strip.html", "context_panel.html", "bookings_board.html",
	} {
		covered[n] = true
	}
	for _, tpl := range tmpl.Templates() {
		name := tpl.Name()
		if name == "" || covered[name] {
			continue
		}
		t.Errorf("template %s is not covered by TestAllTemplatesRender", name)
	}
}
