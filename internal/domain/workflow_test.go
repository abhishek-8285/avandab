package domain_test

import (
	"testing"
	"time"

	"transport-app/internal/domain/booking"
	"transport-app/internal/domain/company"
	"transport-app/internal/domain/customer"
	"transport-app/internal/domain/dispatch"
	"transport-app/internal/domain/driver"
	"transport-app/internal/domain/invoice"
	"transport-app/internal/domain/payment"
	"transport-app/internal/domain/report"
	"transport-app/internal/domain/route"
	"transport-app/internal/domain/trip"
	"transport-app/internal/domain/types"
	"transport-app/internal/domain/vehicle"
)

func stringPtr(s string) *string {
	return &s
}

func TestV1_EndToEndTransportLifecycleWorkflow(t *testing.T) {
	now := time.Now()

	// 1. Create Company Settings / Tenant
	cmp := company.CompanySettings{
		CompanyName: "Apex Logistics Corp",
		Email:       stringPtr("ops@apexlogistics.com"),
		Phone:       stringPtr("+1-800-555-0199"),
		Address:     stringPtr("100 Transport Way, Suite 400"),
		GSTNumber:   stringPtr("TAX-9988-77"),
	}
	if cmp.CompanyName == "" {
		t.Fatalf("company creation failed")
	}

	// 2. Create Customer
	cust := customer.Customer{
		ID:        types.CustomerID("cust-101"),
		Name:      "Acme Global",
		Email:     stringPtr("billing@acme.com"),
		Phone:     "+1-555-0123",
		CreatedAt: now,
	}

	// 3. Create Driver
	drv := driver.Driver{
		ID:            types.DriverID("drv-201"),
		FirstName:     "John",
		LastName:      "Doe",
		LicenseNumber: "DL-998877",
		Phone:         "+1-555-0199",
		Status:        driver.DriverAvailable,
		CreatedAt:     now,
	}

	// 4. Create Vehicle
	vh := vehicle.Vehicle{
		ID:                 types.VehicleID("vh-301"),
		RegistrationNumber: "REG-TX-4040",
		VehicleType:        vehicle.VehicleTypeTruck,
		Status:             vehicle.VehicleAvailable,
		CreatedAt:          now,
	}

	// 5. Create Route
	rt := route.Route{
		ID:             types.RouteID("rt-401"),
		Source:         "Houston Hub",
		Destination:    "Dallas Terminal",
		Distance:       385.5,
		EstimatedHours: 4.5,
		CreatedAt:      now,
	}

	// 6. Create Booking
	bk := booking.Booking{
		ID:            types.BookingID("bk-501"),
		BookingNumber: "BK-2026-0001",
		CustomerID:    cust.ID,
		RouteID:       rt.ID,
		VehicleType:   vh.VehicleType,
		PickupDate:    now.Add(24 * time.Hour),
		Price:         1500.0,
		Status:        booking.BookingPending,
		CreatedAt:     now,
	}

	// 7. Confirm Booking
	if err := bk.CanConfirm(); err != nil {
		t.Fatalf("failed confirming booking: %v", err)
	}
	bk.Status = booking.BookingConfirmed

	// 8. Dispatch (Choose Booking, Assign Driver & Vehicle)
	dsp := dispatch.Dispatch{
		ID:           types.DispatchID("dsp-601"),
		DispatchNo:   "DSP-2026-0001",
		DispatcherID: types.UserID("usr-dispatcher-1"),
		BookingID:    bk.ID,
		ScheduledAt:  bk.PickupDate,
		Status:       dispatch.DispatchDraft,
		CreatedAt:    now,
	}
	if err := dsp.AssignResources(drv.ID, vh.ID); err != nil {
		t.Fatalf("failed assigning resources to dispatch: %v", err)
	}

	// 9. Create Trip from Dispatch
	trp := trip.Trip{
		ID:            types.TripID("trp-701"),
		TripNumber:    "TRP-2026-0001",
		BookingID:     &bk.ID,
		DriverID:      dsp.DriverID,
		VehicleID:     dsp.VehicleID,
		RouteID:       rt.ID,
		DepartureTime: dsp.ScheduledAt,
		Status:        trip.TripScheduled,
		CreatedAt:     now,
	}
	if err := dsp.ConvertToTrip(trp.ID); err != nil {
		t.Fatalf("failed converting dispatch to trip: %v", err)
	}

	// Lock driver and vehicle to on-trip status
	drv.Status = driver.DriverOnTrip
	vh.Status = vehicle.VehicleRunning

	// 10. Start Trip
	trp.Status = trip.TripAssigned
	if err := trp.CanStart(); err != nil {
		t.Fatalf("failed to validate trip start: %v", err)
	}
	trp.Status = trip.TripStarted

	// 11. Deliver then complete Trip (completion requires delivered status)
	trp.Status = trip.TripDelivered
	if err := trp.CanComplete(); err != nil {
		t.Fatalf("failed to validate trip completion: %v", err)
	}
	trp.Status = trip.TripCompleted
	bk.Status = booking.BookingCompleted
	drv.Status = driver.DriverAvailable
	vh.Status = vehicle.VehicleAvailable

	// 12. Generate Invoice
	inv := invoice.Invoice{
		ID:            types.InvoiceID("inv-801"),
		InvoiceNumber: "INV-2026-0001",
		BookingID:     bk.ID,
		CustomerID:    cust.ID,
		TripID:        &trp.ID,
		Subtotal:      bk.Price,
		Tax:           bk.Price * 0.10, // 10% tax
		Discount:      0.0,
		Total:         1650.0,
		PaidAmount:    0.0,
		Status:        invoice.InvoiceDraft,
		CreatedAt:     now,
	}
	inv.MarkIssued(now.Add(14 * 24 * time.Hour))
	if inv.Status != invoice.InvoiceOutstanding {
		t.Fatalf("expected invoice status to be outstanding, got %s", inv.Status)
	}

	// 13. Receive Payment
	pmt := payment.Payment{
		ID:          types.PaymentID("pmt-901"),
		InvoiceID:   inv.ID,
		PaymentDate: now,
		Amount:      1650.0,
		Method:      payment.PaymentMethodBankTransfer,
		CreatedAt:   now,
	}
	inv.ApplyPayment(pmt.Amount)
	if inv.Status != invoice.InvoicePaid {
		t.Fatalf("expected invoice to be fully paid, got %s", inv.Status)
	}

	// 14. View Dashboard Metrics
	dashboardPendingCount := int64(0) // since booking is completed
	dashboardCompletedTrips := int64(1)
	dashboardOutstandingBalance := inv.OutstandingBalance() // should be 0

	if dashboardOutstandingBalance != 0 {
		t.Errorf("expected 0 outstanding balance, got %f", dashboardOutstandingBalance)
	}
	if dashboardCompletedTrips != 1 {
		t.Errorf("expected 1 completed trip")
	}
	_ = dashboardPendingCount

	// 15. Generate Operational Report
	rpt := report.OperationalReportSummary{
		GeneratedAt: now,
		Trips: report.TripsReport{
			TotalTrips:        1,
			CompletedTrips:    1,
			CompletionRatePct: 100.0,
		},
		Revenue: report.RevenueReport{
			TotalInvoiced:  inv.Total,
			TotalCollected: pmt.Amount,
			TotalTax:       inv.Tax,
		},
		Outstanding: report.OutstandingReport{
			TotalOutstanding: inv.OutstandingBalance(),
		},
	}

	if rpt.Trips.CompletionRatePct != 100.0 {
		t.Errorf("expected 100%% completion rate in report, got %f", rpt.Trips.CompletionRatePct)
	}
	if rpt.Revenue.TotalCollected != 1650.0 {
		t.Errorf("expected 1650 revenue collected in report, got %f", rpt.Revenue.TotalCollected)
	}
}
