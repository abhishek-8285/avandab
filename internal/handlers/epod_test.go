package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/config"
	"transport-app/internal/shared"
	tripagg "transport-app/internal/trip/domain/aggregate"
	triprepo "transport-app/internal/trip/infrastructure/persistence/sql"
)

func TestSubmitStopPOD_MultipartFileUpload(t *testing.T) {
	db := handlerTestDB(t)
	tenantID := shared.TenantID("1")
	authSvc := &mockAuthSvc{}
	tmpl, err := parseTemplates(authSvc)
	require.NoError(t, err)

	tempDir := t.TempDir()
	app := &App{
		DB:        db,
		Templates: tmpl,
		Config: &config.Config{
			UploadDir: tempDir,
		},
	}
	h := &TripHandlers{App: app}

	// Seed customer, route, vehicle, driver, booking, trip, stop
	tripID := "trip_pod_upload_1"
	stopID := "stop_pod_upload_1"

	_, err = db.Exec(`INSERT INTO customers (id, name, company, phone, tenant_id) VALUES ('cust_pod', 'Alice Corp', 'Alice Enterprises', '9988771122', '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES ('r_pod', 'Mumbai', 'Pune', 150, 4, 12000, '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id) VALUES ('d_pod', 'DRV-POD-1', 'Sunil', 'Sharma', '9876500001', 'DL-POD-1', date('now','+1 year'), 'available', '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, tenant_id) VALUES ('v_pod', 'MH-12-AB-9999', 'MH-12-AB-9999', 'truck', 10, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price, status, tenant_id) VALUES ('bk_pod', 'BK-POD-01', 'cust_pod', '2026-09-02 08:00:00', 'r_pod', 'truck', 12000, 'confirmed', '1')`)
	require.NoError(t, err)

	// Save Trip Aggregate with stop
	uowImpl := triprepo.NewTripRepository(db)
	agg := tripagg.NewTripAggregate(
		tripagg.TripID(tripID),
		tenantID,
		"TRIP-POD-001",
		nil,
		"r_pod",
		parseTimeStr("2026-09-02 08:00:00"),
		"POD Upload Test Trip",
		parseTimeStr("2026-09-02 08:00:00"),
	)
	agg.AssignDriver("d_pod", parseTimeStr("2026-09-02 08:05:00"))
	agg.AssignVehicle("v_pod", parseTimeStr("2026-09-02 08:10:00"))
	agg.Start(parseTimeStr("2026-09-02 08:15:00"))
	agg.ReachPickup(parseTimeStr("2026-09-02 08:30:00"))
	agg.StartTransit(parseTimeStr("2026-09-02 08:45:00"))

	agg.AddStop(tripagg.TripStop{
		ID:             stopID,
		TenantID:       tenantID,
		TripID:         tripagg.TripID(tripID),
		StopSequence:   1,
		StopType:       tripagg.StopTypeDrop,
		LocationName:   "Pune Warehouse Central",
		Address:        "Plot 42, Hinjewadi Phase 1, Pune",
		ConsigneeName:  "Rahul Verma",
		ConsigneePhone: "9876543210",
		Status:         tripagg.StopStatusPending,
		OTPRequired:    true,
		OTPCode:        "1234",
		PODRequired:    true,
	})
	require.NoError(t, uowImpl.Save(context.Background(), agg))

	// Create multipart request with real PNG photo and signature
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 1x1 valid PNG bytes
	pngBytes := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
		0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
		0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
	}

	partPhoto, err := writer.CreateFormFile("pod_file", "cargo_delivery.png")
	require.NoError(t, err)
	_, err = partPhoto.Write(pngBytes)
	require.NoError(t, err)

	partSig, err := writer.CreateFormFile("signature_file", "consignee_sign.png")
	require.NoError(t, err)
	_, err = partSig.Write(pngBytes)
	require.NoError(t, err)

	_ = writer.WriteField("notes", "All 50 crates verified intact and undamaged")
	_ = writer.WriteField("otp", "1234")
	require.NoError(t, writer.Close())

	req := httptest.NewRequest("POST", fmt.Sprintf("/trips/%s/stops/%s/pod", tripID, stopID), body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", tripID)
	rctx.URLParams.Add("stopId", stopID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = shared.ContextWithTenantID(ctx, tenantID)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.SubmitStopPOD(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "pod_verified", resp["status"])
	assert.True(t, strings.HasPrefix(resp["pod_url"].(string), "/uploads/pod/"))
	assert.True(t, strings.HasPrefix(resp["signature_url"].(string), "/uploads/pod/"))

	// Verify file was written to disk
	savedPhotoPath := strings.TrimPrefix(resp["pod_url"].(string), "/uploads/pod/")
	fullPath := fmt.Sprintf("%s/pod/%s", tempDir, savedPhotoPath)
	_, err = os.Stat(fullPath)
	assert.NoError(t, err, "Saved POD photo file should exist on disk")

	// Verify DB state
	var podURL, sigURL, notes string
	var otpVerifiedAt sql.NullString
	err = db.QueryRow(`SELECT COALESCE(pod_url,''), COALESCE(pod_signature_url,''), COALESCE(pod_notes,''), otp_verified_at FROM trip_stops WHERE id = ?`, stopID).Scan(&podURL, &sigURL, &notes, &otpVerifiedAt)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(podURL, "/uploads/pod/"))
	assert.True(t, strings.HasPrefix(sigURL, "/uploads/pod/"))
	assert.Equal(t, "All 50 crates verified intact and undamaged", notes)
	assert.True(t, otpVerifiedAt.Valid)
}

func TestSubmitStopPOD_Base64SignatureAndJSON(t *testing.T) {
	db := handlerTestDB(t)
	tenantID := shared.TenantID("1")
	tempDir := t.TempDir()

	app := &App{
		DB: db,
		Config: &config.Config{
			UploadDir: tempDir,
		},
	}
	h := &TripHandlers{App: app}

	tripID := "trip_pod_json_1"
	stopID := "stop_pod_json_1"

	uowImpl := triprepo.NewTripRepository(db)
	agg := tripagg.NewTripAggregate(
		tripagg.TripID(tripID),
		tenantID,
		"TRIP-JSON-001",
		nil,
		"r_pod",
		parseTimeStr("2026-09-02 08:00:00"),
		"JSON POD Test",
		parseTimeStr("2026-09-02 08:00:00"),
	)
	agg.AddStop(tripagg.TripStop{
		ID:            stopID,
		TenantID:      tenantID,
		TripID:        tripagg.TripID(tripID),
		StopSequence:  1,
		StopType:      tripagg.StopTypeDrop,
		LocationName:  "Hyderabad Logistics Hub",
		ConsigneeName: "Priya Sharma",
		Status:        tripagg.StopStatusPending,
		OTPRequired:   false,
		PODRequired:   true,
	})
	require.NoError(t, uowImpl.Save(context.Background(), agg))

	base64Sig := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	reqBody, _ := json.Marshal(map[string]string{
		"pod_url":       "https://cdn.example.com/cargo_photo.jpg",
		"signature_url": base64Sig,
		"notes":         "Delivered to Priya Sharma in person",
	})

	req := httptest.NewRequest("POST", fmt.Sprintf("/trips/%s/stops/%s/pod", tripID, stopID), bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", tripID)
	rctx.URLParams.Add("stopId", stopID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = shared.ContextWithTenantID(ctx, tenantID)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.SubmitStopPOD(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "pod_verified", resp["status"])
	assert.Equal(t, "https://cdn.example.com/cargo_photo.jpg", resp["pod_url"])
	assert.True(t, strings.HasPrefix(resp["signature_url"].(string), "/uploads/pod/"), "Base64 signature should be converted to saved local file")
}

func TestPublicEPODCertificate_RenderHTMLAndJSON(t *testing.T) {
	db := handlerTestDB(t)
	tenantID := shared.TenantID("1")
	authSvc := &mockAuthSvc{}
	tmpl, err := parseTemplates(authSvc)
	require.NoError(t, err)

	app := &App{
		DB:        db,
		Templates: tmpl,
	}
	h := &TripHandlers{App: app}

	tripID := "trip_epod_public_1"
	stopID := "stop_epod_public_1"

	// Seed vehicle, driver, route, company_settings, trip, stop
	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES ('r_pod', 'Mumbai', 'Bengaluru', 980, 18, 45000, '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id) VALUES ('d_epod', 'DRV-EPOD-1', 'Rajesh', 'Kumar', '+91 9876543210', 'DL-EPOD-99', date('now','+1 year'), 'available', '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, tenant_id) VALUES ('v_epod', 'KA-01-EQ-5555', 'KA-01-EQ-5555', 'truck', 15, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO company_settings (id, company_name, currency, timezone) VALUES (1, 'Avandab Logistics Network', 'INR', 'Asia/Kolkata') ON CONFLICT(id) DO UPDATE SET company_name = 'Avandab Logistics Network'`)
	require.NoError(t, err)
	_, _ = db.Exec(`UPDATE tenants SET name = 'Avandab Logistics Network' WHERE id = '1'`)

	now := parseTimeStr("2026-09-02 14:30:00")
	uowImpl := triprepo.NewTripRepository(db)
	agg := tripagg.NewTripAggregate(
		tripagg.TripID(tripID),
		tenantID,
		"TRIP-EPOD-900",
		nil,
		"r_pod",
		now.Add(-4*time.Hour),
		"Express Freight Delivery",
		now.Add(-4*time.Hour),
	)
	agg.AssignDriver("d_epod", now.Add(-3*time.Hour))
	agg.AssignVehicle("v_epod", now.Add(-3*time.Hour))
	agg.Start(now.Add(-3 * time.Hour))
	agg.ReachPickup(now.Add(-2 * time.Hour))
	agg.StartTransit(now.Add(-2 * time.Hour))

	agg.AddStop(tripagg.TripStop{
		ID:              stopID,
		TenantID:        tenantID,
		TripID:          tripagg.TripID(tripID),
		StopSequence:    1,
		StopType:        tripagg.StopTypeDrop,
		LocationName:    "Bengaluru Electronic City Terminal",
		Address:         "Plot 100, Phase 2, Hosur Road, Bengaluru 560100",
		ConsigneeName:   "Amit Patel",
		ConsigneePhone:  "+91 9123456780",
		ConsigneeEmail:  "amit.patel@example.com",
		Status:          tripagg.StopStatusCompleted,
		OTPRequired:     true,
		OTPCode:         "5678",
		PODRequired:     true,
		PODURL:          "/uploads/pod/delivery_cargo_proof.jpg",
		PODSignatureURL: "/uploads/pod/consignee_signature.png",
		PODNotes:        "Received in good condition with seal intact",
	})
	_ = agg.VerifyStopOTP(stopID, "5678", now.Add(-10*time.Minute))
	_ = agg.SubmitStopPOD(stopID, "/uploads/pod/delivery_cargo_proof.jpg", "/uploads/pod/consignee_signature.png", "Received in good condition with seal intact", now.Add(-5*time.Minute))
	_ = agg.CompleteStop(stopID, now)
	_ = agg.Deliver(now)
	require.NoError(t, uowImpl.Save(context.Background(), agg))

	// Also mirror to trips table for completeness
	_, err = db.Exec(`UPDATE trips SET status = 'delivered', vehicle_id = 'v_epod', driver_id = 'd_epod', pod_url = '/uploads/pod/delivery_cargo_proof.jpg', pod_signature_url = '/uploads/pod/consignee_signature.png', pod_consignee_name = 'Amit Patel', pod_consignee_phone = '+91 9123456780', pod_otp_verified = 1, pod_notes = 'Received in good condition with seal intact', delivered_at = datetime('now') WHERE id = ?`, tripID)
	require.NoError(t, err)

	// 1. Test HTML Certificate Rendering
	reqHTML := httptest.NewRequest("GET", "/epod/"+tripID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tripId", tripID)
	reqHTML = reqHTML.WithContext(context.WithValue(reqHTML.Context(), chi.RouteCtxKey, rctx))

	wHTML := httptest.NewRecorder()
	h.PublicEPODCertificate(wHTML, reqHTML)

	assert.Equal(t, http.StatusOK, wHTML.Code)
	bodyStr := wHTML.Body.String()

	// Verify essential requirements on the certificate
	assert.Contains(t, bodyStr, "Digital Proof of Delivery", "Must contain title")
	assert.Contains(t, bodyStr, "Avandab Logistics Network", "Must contain company name")
	assert.Contains(t, bodyStr, "TRIP-EPOD-900", "Must contain trip number")
	assert.Contains(t, bodyStr, "KA-01-EQ-5555", "Must contain vehicle registration")
	assert.Contains(t, bodyStr, "Rajesh Kumar", "Must contain driver name")
	assert.Contains(t, bodyStr, "Bengaluru Electronic City Terminal", "Must contain location")
	assert.Contains(t, bodyStr, "Amit Patel", "Must contain consignee name")
	assert.Contains(t, bodyStr, "9123456780", "Must contain consignee phone")
	assert.Contains(t, bodyStr, "OTP Verified", "Must contain verified OTP badge")
	assert.Contains(t, bodyStr, "/uploads/pod/consignee_signature.png", "Must contain signature image url")
	assert.Contains(t, bodyStr, "/uploads/pod/delivery_cargo_proof.jpg", "Must contain cargo photo url")
	assert.Contains(t, bodyStr, "Print / Download PDF", "Must contain print button")
	assert.Contains(t, bodyStr, "Digital Verification Seal", "Must contain security seal hash")

	// 2. Test JSON Representation
	reqJSON := httptest.NewRequest("GET", "/epod/"+tripID+"?format=json", nil)
	reqJSON.Header.Set("Accept", "application/json")
	reqJSON = reqJSON.WithContext(context.WithValue(reqJSON.Context(), chi.RouteCtxKey, rctx))

	wJSON := httptest.NewRecorder()
	h.PublicEPODCertificate(wJSON, reqJSON)

	assert.Equal(t, http.StatusOK, wJSON.Code)
	var jsonView EPODReceiptView
	err = json.Unmarshal(wJSON.Body.Bytes(), &jsonView)
	require.NoError(t, err)
	assert.Equal(t, "TRIP-EPOD-900", jsonView.TripNumber)
	assert.Equal(t, "KA-01-EQ-5555", jsonView.VehicleReg)
	assert.Equal(t, "Rajesh Kumar", jsonView.DriverName)
	assert.Equal(t, "Amit Patel", jsonView.ConsigneeName)
	assert.True(t, jsonView.OTPVerified)
	assert.Equal(t, "/uploads/pod/delivery_cargo_proof.jpg", jsonView.PODURL)
	assert.Equal(t, "/uploads/pod/consignee_signature.png", jsonView.SignatureURL)
	assert.NotEmpty(t, jsonView.VerificationHash)

	// 3. Test 404 for unknown trip
	req404 := httptest.NewRequest("GET", "/epod/unknown_trip_9999", nil)
	rctx404 := chi.NewRouteContext()
	rctx404.URLParams.Add("tripId", "unknown_trip_9999")
	req404 = req404.WithContext(context.WithValue(req404.Context(), chi.RouteCtxKey, rctx404))

	w404 := httptest.NewRecorder()
	h.PublicEPODCertificate(w404, req404)
	assert.Equal(t, http.StatusNotFound, w404.Code)
}
