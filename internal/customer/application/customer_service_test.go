package application_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/customer/application"
	custSQL "transport-app/internal/customer/infrastructure/persistence/sql"
	driverApp "transport-app/internal/driver/application"
)

func setupCustomerTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	schema := `
	CREATE TABLE tenants (id TEXT PRIMARY KEY, name TEXT);
	INSERT INTO tenants (id, name) VALUES ('tenant-1', 'Fleet Tenant 1'), ('tenant-2', 'Fleet Tenant 2');

	CREATE TABLE customers (id TEXT PRIMARY KEY, tenant_id TEXT, name TEXT, email TEXT, phone TEXT);
	INSERT INTO customers (id, tenant_id, name, email, phone) VALUES 
	('cust-1', 'tenant-1', 'Acme Corp', 'acme@test.com', '9876543210'),
	('cust-2', 'tenant-1', 'Beta Ltd', 'beta@test.com', '9876543211'),
	('cust-other-tenant', 'tenant-2', 'Gamma Corp', 'gamma@test.com', '9876543212');

	CREATE TABLE drivers (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		first_name TEXT NOT NULL,
		last_name TEXT,
		phone TEXT,
		email TEXT,
		aadhaar TEXT,
		pan TEXT,
		bank_details TEXT,
		score REAL,
		status TEXT DEFAULT 'available'
	);
	INSERT INTO drivers (id, tenant_id, first_name, last_name, phone, email, aadhaar, pan, bank_details, score)
	VALUES ('drv-1', 'tenant-1', 'Rajesh', 'Kumar', '9876543210', 'rajesh@test.com', '1234-5678-9012', 'ABCDE1234F', '{"account":"999888777"}', 4.85);

	CREATE TABLE vehicles (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		plate_number TEXT NOT NULL,
		model TEXT,
		type TEXT NOT NULL
	);
	INSERT INTO vehicles (id, tenant_id, plate_number, model, type)
	VALUES ('veh-1', 'tenant-1', 'MH12AB1234', 'Tata Ace Gold', 'TATA_ACE');

	CREATE TABLE driver_vehicle_latest_positions (
		tenant_id TEXT NOT NULL,
		vehicle_id TEXT NOT NULL PRIMARY KEY,
		latitude REAL NOT NULL,
		longitude REAL NOT NULL,
		speed_kmph REAL,
		heading REAL,
		recorded_at DATETIME NOT NULL
	);
	INSERT INTO driver_vehicle_latest_positions (tenant_id, vehicle_id, latitude, longitude, speed_kmph, heading, recorded_at)
	VALUES ('tenant-1', 'veh-1', 19.0760, 72.8777, 45.5, 180.0, datetime('now'));

	CREATE TABLE bookings (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		booking_number TEXT NOT NULL,
		customer_id TEXT NOT NULL,
		pickup_date DATETIME NOT NULL,
		route_id TEXT NOT NULL,
		vehicle_type TEXT NOT NULL,
		passengers INTEGER DEFAULT 1,
		cargo_weight REAL,
		price REAL NOT NULL,
		notes TEXT,
		status TEXT DEFAULT 'pending',
		created_at DATETIME DEFAULT (datetime('now')),
		updated_at DATETIME DEFAULT (datetime('now'))
	);

	CREATE TABLE trips (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		booking_id TEXT NOT NULL,
		driver_id TEXT NOT NULL,
		vehicle_id TEXT NOT NULL,
		status TEXT DEFAULT 'assigned',
		started_at DATETIME,
		created_at DATETIME DEFAULT (datetime('now')),
		updated_at DATETIME DEFAULT (datetime('now'))
	);

	CREATE TABLE driver_documents (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		driver_id TEXT,
		entity_type TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		document_type TEXT NOT NULL,
		verification_status TEXT DEFAULT 'pending',
		file_path TEXT,
		created_at DATETIME DEFAULT (datetime('now'))
	);

	CREATE TABLE customer_quotes (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		customer_id TEXT NOT NULL,
		origin TEXT NOT NULL,
		destination TEXT NOT NULL,
		cargo_type TEXT NOT NULL,
		vehicle_type TEXT NOT NULL,
		weight_kg REAL NOT NULL DEFAULT 0.0,
		distance_km REAL NOT NULL DEFAULT 0.0,
		base_rate REAL NOT NULL DEFAULT 0.0,
		per_km_rate REAL NOT NULL DEFAULT 0.0,
		estimated_toll REAL NOT NULL DEFAULT 0.0,
		subtotal REAL NOT NULL DEFAULT 0.0,
		gst_rate REAL NOT NULL DEFAULT 0.05,
		gst_amount REAL NOT NULL DEFAULT 0.0,
		discount_amount REAL NOT NULL DEFAULT 0.0,
		total_price REAL NOT NULL DEFAULT 0.0,
		status TEXT NOT NULL DEFAULT 'active',
		expires_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL DEFAULT (datetime('now'))
	);

	CREATE TABLE customer_booking_details (
		booking_id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		quote_id TEXT,
		idempotency_key TEXT,
		pickup_address TEXT NOT NULL,
		pickup_lat REAL,
		pickup_lng REAL,
		pickup_contact_name TEXT,
		pickup_contact_phone TEXT,
		delivery_address TEXT NOT NULL,
		delivery_lat REAL,
		delivery_lng REAL,
		delivery_contact_name TEXT,
		delivery_contact_phone TEXT,
		scheduled_at DATETIME,
		cargo_description TEXT,
		special_instructions TEXT,
		payment_status TEXT NOT NULL DEFAULT 'pending',
		payment_method TEXT,
		cancellation_reason TEXT,
		cancelled_by TEXT,
		cancelled_at DATETIME,
		created_at DATETIME NOT NULL DEFAULT (datetime('now')),
		updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
	);

	CREATE TABLE dispatch_offers (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		booking_id TEXT NOT NULL,
		driver_id TEXT NOT NULL,
		vehicle_id TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'offered',
		offered_at DATETIME NOT NULL,
		expires_at DATETIME NOT NULL,
		responded_at DATETIME
	);

	CREATE TABLE driver_commands (
		command_id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		driver_id TEXT NOT NULL,
		command_type TEXT NOT NULL,
		status TEXT NOT NULL,
		response_payload TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT (datetime('now'))
	);
	`

	_, err = db.Exec(schema)
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestPhase7_CustomerBookingWorkflow(t *testing.T) {
	db := setupCustomerTestDB(t)
	repo := custSQL.NewSQLCustomerRepository(db)
	svc := application.NewCustomerAppService(repo)
	ctx := context.Background()

	tenantID := "tenant-1"
	customerID := "cust-1"

	// 1. Create Server-Authoritative Quote
	quoteReq := application.CreateQuoteRequest{
		Origin:      "Andheri East, Mumbai",
		Destination: "Bhiwandi Hub, Thane",
		CargoType:   "Electronics",
		VehicleType: "TATA_ACE",
		WeightKg:    650,
		DistanceKm:  42.5,
		TTLMinutes:  30,
	}
	quote, err := svc.CreateQuote(ctx, tenantID, customerID, quoteReq)
	require.NoError(t, err)
	assert.NotEmpty(t, quote.ID)
	assert.Equal(t, "active", quote.Status)
	assert.Equal(t, 400.0, quote.BaseRate)
	assert.Equal(t, 25.0, quote.PerKmRate)
	assert.True(t, quote.TotalPrice > 0)
	assert.True(t, quote.ExpiresAt.After(time.Now()))

	// 2. Create Booking from Quote (Idempotent)
	bookReq := application.CreateBookingRequest{
		IdempotencyKey:       "idemp_test_001",
		QuoteID:              quote.ID,
		PickupAddress:        "Andheri East, Mumbai",
		PickupContactName:    "Warehouse Mgr",
		PickupContactPhone:   "9988776655",
		DeliveryAddress:      "Bhiwandi Hub, Thane",
		DeliveryContactName:  "Receiver Mgr",
		DeliveryContactPhone: "9988776644",
		CargoDescription:     "15 boxes of circuit boards",
		PaymentMethod:        "razorpay_prepaid",
	}

	bookResp1, err := svc.CreateBooking(ctx, tenantID, customerID, bookReq)
	require.NoError(t, err)
	assert.NotEmpty(t, bookResp1.BookingID)
	assert.Equal(t, "confirmed", bookResp1.Status)
	assert.False(t, bookResp1.IsIdempotent)
	assert.Equal(t, quote.TotalPrice, bookResp1.TotalPrice)

	// Invariant: Duplicate booking creation with same idempotency key returns identical booking
	bookResp2, err := svc.CreateBooking(ctx, tenantID, customerID, bookReq)
	require.NoError(t, err)
	assert.Equal(t, bookResp1.BookingID, bookResp2.BookingID)
	assert.True(t, bookResp2.IsIdempotent)

	// Invariant: Reusing the converted quote for a new booking is rejected
	bookReqDifferent := bookReq
	bookReqDifferent.IdempotencyKey = "idemp_test_002"
	_, err = svc.CreateBooking(ctx, tenantID, customerID, bookReqDifferent)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no longer active")

	// 3. Customer Tracking Unified Operational View
	tracking, err := svc.GetBookingTracking(ctx, tenantID, customerID, bookResp1.BookingID)
	require.NoError(t, err)
	assert.Equal(t, bookResp1.BookingID, tracking.BookingID)
	assert.Equal(t, "confirmed", tracking.Status)
	assert.Equal(t, "Andheri East, Mumbai", tracking.Pickup.Address)
	assert.Equal(t, "Bhiwandi Hub, Thane", tracking.Delivery.Address)
	assert.Equal(t, "pending", tracking.Payment.Status)
	assert.Equal(t, quote.TotalPrice, tracking.Payment.TotalPrice)

	// 4. Invariant: Multi-tenant Isolation
	// Customer in Tenant 2 cannot view Tenant 1's booking
	_, err = svc.GetBookingTracking(ctx, "tenant-2", "cust-other-tenant", bookResp1.BookingID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found or unauthorized")

	// Another customer in Tenant 1 cannot access customer 1's booking
	_, err = svc.GetBookingTracking(ctx, tenantID, "cust-2", bookResp1.BookingID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found or unauthorized")

	// 5. Dispatch & Trip Flow Integration
	driverSvc := driverApp.NewDriverAppService(db)
	offer, err := driverSvc.CreateDispatchOffer(ctx, tenantID, bookResp1.BookingID, "drv-1", "veh-1", 15)
	require.NoError(t, err)

	// Invariant: Offer creation does not prematurely mutate customer booking state
	trackingAfterOffer, err := svc.GetBookingTracking(ctx, tenantID, customerID, bookResp1.BookingID)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", trackingAfterOffer.Status)

	// Driver accepts offer -> Trip created
	cmdAccept := driverApp.DriverCommandRequest{
		CommandID: "cmd-accept-cust-trip",
		Type:      "ACCEPT_OFFER",
		Payload:   map[string]interface{}{"offer_id": offer.ID},
	}
	respAccept, err := driverSvc.ProcessDriverCommand(ctx, tenantID, "drv-1", cmdAccept)
	require.NoError(t, err)
	tripID := respAccept.TripID

	// 6. Verify Customer Tracking Projection reflects assigned vehicle, masked driver, and live GPS
	trackingAssigned, err := svc.GetBookingTracking(ctx, tenantID, customerID, bookResp1.BookingID)
	require.NoError(t, err)
	require.NotNil(t, trackingAssigned.Vehicle)
	assert.Equal(t, "MH12AB1234", trackingAssigned.Vehicle.PlateNumber)
	assert.Equal(t, "Tata Ace Gold", trackingAssigned.Vehicle.Model)

	require.NotNil(t, trackingAssigned.Driver)
	assert.Equal(t, "Rajesh", trackingAssigned.Driver.FirstName)
	// Invariant: Driver PII masking (Aadhaar/PAN/Bank not in projection, phone masked)
	assert.Equal(t, "987****210", trackingAssigned.Driver.PhoneMasked)
	assert.Equal(t, 4.85, *trackingAssigned.Driver.Score)

	// Live tracking coordinates present
	require.NotNil(t, trackingAssigned.Tracking)
	assert.Equal(t, 19.0760, trackingAssigned.Tracking.Latitude)
	assert.Equal(t, 72.8777, trackingAssigned.Tracking.Longitude)

	// 7. Trip Execution & Document Delivery (POD)
	_, _ = db.Exec(`
		INSERT INTO driver_documents (id, tenant_id, driver_id, entity_type, entity_id, document_type, verification_status, file_path)
		VALUES ('doc-lr-1', 'tenant-1', 'drv-1', 'trip', ?, 'LR', 'verified', '/docs/lr_001.pdf')`, tripID)

	// Start and complete trip
	_, err = driverSvc.ProcessDriverCommand(ctx, tenantID, "drv-1", driverApp.DriverCommandRequest{
		CommandID: "cmd-start-cust-flow",
		Type:      "START_TRIP",
		Payload:   map[string]interface{}{"trip_id": tripID},
	})
	require.NoError(t, err)

	_, err = driverSvc.ProcessDriverCommand(ctx, tenantID, "drv-1", driverApp.DriverCommandRequest{
		CommandID: "cmd-complete-cust-flow",
		Type:      "COMPLETE_TRIP",
		Payload:   map[string]interface{}{"trip_id": tripID, "pod_required": false},
	})
	require.NoError(t, err)

	// Add POD document
	_, _ = db.Exec(`
		INSERT INTO driver_documents (id, tenant_id, driver_id, entity_type, entity_id, document_type, verification_status, file_path)
		VALUES ('doc-pod-1', 'tenant-1', 'drv-1', 'trip', ?, 'POD', 'verified', '/docs/pod_001.jpg')`, tripID)

	// Verify Customer Tracking includes LR and POD documents
	trackingCompleted, err := svc.GetBookingTracking(ctx, tenantID, customerID, bookResp1.BookingID)
	require.NoError(t, err)
	assert.Equal(t, 2, len(trackingCompleted.Documents))

	// Invariant: Cancellation rule enforcement
	// Attempting to cancel completed booking must fail
	err = svc.CancelBooking(ctx, tenantID, customerID, bookResp1.BookingID, "Changed mind")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot cancel")
}

func TestPhase7_BookingCancellationBeforeTrip(t *testing.T) {
	db := setupCustomerTestDB(t)
	repo := custSQL.NewSQLCustomerRepository(db)
	svc := application.NewCustomerAppService(repo)
	ctx := context.Background()

	tenantID := "tenant-1"
	customerID := "cust-1"

	// 1. Create Quote & Booking
	quote, err := svc.CreateQuote(ctx, tenantID, customerID, application.CreateQuoteRequest{
		Origin:      "Mumbai",
		Destination: "Pune",
		VehicleType: "BOLERO",
		DistanceKm:  150,
	})
	require.NoError(t, err)

	booking, err := svc.CreateBooking(ctx, tenantID, customerID, application.CreateBookingRequest{
		IdempotencyKey:  "idemp_cancel_001",
		QuoteID:         quote.ID,
		PickupAddress:   "Mumbai",
		DeliveryAddress: "Pune",
	})
	require.NoError(t, err)

	// Create pending dispatch offer
	_, _ = db.Exec(`
		INSERT INTO dispatch_offers (id, tenant_id, booking_id, driver_id, vehicle_id, status, offered_at, expires_at)
		VALUES ('off-cancel-1', 'tenant-1', ?, 'drv-1', 'veh-1', 'offered', datetime('now'), datetime('now', '+15 minutes'))`,
		booking.BookingID)

	// 2. Customer cancels booking before trip start
	err = svc.CancelBooking(ctx, tenantID, customerID, booking.BookingID, "Order rescheduled by supplier")
	require.NoError(t, err)

	// Verify booking status is cancelled
	tracking, err := svc.GetBookingTracking(ctx, tenantID, customerID, booking.BookingID)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", tracking.Status)

	// Invariant: Dispatch offer was automatically cancelled
	var offerStatus string
	err = db.QueryRow(`SELECT status FROM dispatch_offers WHERE id = 'off-cancel-1'`).Scan(&offerStatus)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", offerStatus)
}
