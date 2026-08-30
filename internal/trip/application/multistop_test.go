package application

import (
	"context"
	"testing"
	"time"

	"transport-app/internal/shared"
	clock "transport-app/internal/shared/clock"
	id "transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
	tripagg "transport-app/internal/trip/domain/aggregate"
	"transport-app/internal/trip/infrastructure/persistence/sql"
)

func TestMultiStopTrip_FullLifecycleAndInvariants(t *testing.T) {
	db := newTripTestDB(t)

	uowImpl := uow.NewSQLUnitOfWork(db)
	clockImpl := clock.NewRealClock()
	idGen := id.NewUUIDGenerator()

	createTripUC := NewCreateTripUseCase(uowImpl, idGen, clockImpl)
	startTripUC := NewStartTripUseCase(uowImpl, clockImpl)
	reachStopUC := NewReachStopUseCase(uowImpl, clockImpl)
	submitStopPODUC := NewSubmitStopPODUseCase(uowImpl, clockImpl)
	completeStopUC := NewCompleteStopUseCase(uowImpl, clockImpl)
	completeTripUC := NewCompleteTripUseCase(uowImpl, clockImpl)

	tenantID := shared.TenantID("tenant-1")
	ctx := context.Background()

	// Seed route, vehicle, driver
	_, _ = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, standard_fare) VALUES ('rt_multi_1', 'tenant-1', 'Delhi', 'Udaipur', 650, 25000)`)
	seedTestDriver(t, db, "drv_multi_1")
	seedTestVehicle(t, db, "veh_multi_1", false)

	// 1. Create Trip
	tripIDStr, err := createTripUC.Execute(ctx, CreateTripCommand{
		TenantID:      tenantID,
		RouteID:       "rt_multi_1",
		DepartureTime: time.Now().UTC(),
		Remarks:       "Delhi -> Jaipur -> Ajmer -> Udaipur",
	})
	if err != nil {
		t.Fatalf("CreateTrip failed: %v", err)
	}
	tripID := tripagg.TripID(tripIDStr)

	// 2. Add 3 Stops to Trip
	repo := sql.NewTripRepository(db)
	trip, err := repo.Find(ctx, tripID, tenantID)
	if err != nil {
		t.Fatalf("Find trip failed: %v", err)
	}

	stop1 := tripagg.TripStop{
		ID:           "stop_delhi_pickup",
		TenantID:     tenantID,
		TripID:       tripID,
		StopSequence: 1,
		StopType:     tripagg.StopTypePickup,
		LocationName: "Delhi Warehouse",
		Address:      "Okhla Phase III, New Delhi",
		OTPRequired:  false,
		PODRequired:  false,
		Status:       tripagg.StopStatusPending,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	stop2 := tripagg.TripStop{
		ID:             "stop_jaipur_drop",
		TenantID:       tenantID,
		TripID:         tripID,
		StopSequence:   2,
		StopType:       tripagg.StopTypeDrop,
		LocationName:   "Jaipur Hub",
		Address:        "Sitapura Industrial Area, Jaipur",
		ConsigneeName:  "Jaipur Retailers",
		ConsigneePhone: "+91-9811111111",
		OTPRequired:    true,
		OTPCode:        "4321",
		PODRequired:    true,
		Status:         tripagg.StopStatusPending,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	stop3 := tripagg.TripStop{
		ID:             "stop_udaipur_drop",
		TenantID:       tenantID,
		TripID:         tripID,
		StopSequence:   3,
		StopType:       tripagg.StopTypeDrop,
		LocationName:   "Udaipur Terminal",
		Address:        "MIA Industrial Area, Udaipur",
		ConsigneeName:  "Udaipur Logistics",
		ConsigneePhone: "+91-9822222222",
		OTPRequired:    false,
		PODRequired:    true,
		Status:         tripagg.StopStatusPending,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	trip.AddStop(stop1)
	trip.AddStop(stop2)
	trip.AddStop(stop3)

	if err := repo.Save(ctx, trip); err != nil {
		t.Fatalf("Save trip with stops failed: %v", err)
	}

	// 3. Start Trip
	_ = trip.AssignDriver("drv_multi_1", time.Now().UTC())
	_ = trip.AssignVehicle("veh_multi_1", time.Now().UTC())
	_ = repo.Save(ctx, trip)

	if err := startTripUC.Execute(ctx, StartTripCommand{TripID: tripID, TenantID: tenantID}); err != nil {
		t.Fatalf("StartTrip failed: %v", err)
	}

	// 4. Invariant: Out-of-order sequence check (Cannot reach Stop 3 before Stop 1 & Stop 2)
	err = reachStopUC.Execute(ctx, ReachStopCommand{TripID: tripID, StopID: "stop_udaipur_drop", TenantID: tenantID})
	if err == nil {
		t.Fatalf("Invariant failed: reaching Stop 3 out-of-order must be rejected")
	}

	// 5. Execute Stop 1 (Pickup)
	if err := reachStopUC.Execute(ctx, ReachStopCommand{TripID: tripID, StopID: "stop_delhi_pickup", TenantID: tenantID}); err != nil {
		t.Fatalf("ReachStop 1 failed: %v", err)
	}
	if err := completeStopUC.Execute(ctx, CompleteStopCommand{TripID: tripID, StopID: "stop_delhi_pickup", TenantID: tenantID}); err != nil {
		t.Fatalf("CompleteStop 1 failed: %v", err)
	}
	reachPickupUC := NewReachPickupUseCase(uowImpl, clockImpl)
	startTransitUC := NewStartTransitUseCase(uowImpl, clockImpl)
	_ = reachPickupUC.Execute(ctx, ReachPickupCommand{TripID: tripID, TenantID: tenantID})
	_ = startTransitUC.Execute(ctx, StartTransitCommand{TripID: tripID, TenantID: tenantID})

	// 6. Invariant: Cannot complete overall trip when Stops 2 & 3 are incomplete
	err = completeTripUC.Execute(ctx, CompleteTripCommand{TripID: tripID, TenantID: tenantID})
	if err == nil {
		t.Fatalf("Invariant failed: completing trip with incomplete stops must be rejected")
	}

	// 7. Execute Stop 2 (Jaipur Drop with OTP & POD)
	if err := reachStopUC.Execute(ctx, ReachStopCommand{TripID: tripID, StopID: "stop_jaipur_drop", TenantID: tenantID}); err != nil {
		t.Fatalf("ReachStop 2 failed: %v", err)
	}

	// Invariant: Complete Stop 2 without OTP/POD must fail
	err = completeStopUC.Execute(ctx, CompleteStopCommand{TripID: tripID, StopID: "stop_jaipur_drop", TenantID: tenantID})
	if err == nil {
		t.Fatalf("Invariant failed: completing Stop 2 without OTP/POD must fail")
	}

	// Submit Stop 2 POD with invalid OTP -> should fail
	err = submitStopPODUC.Execute(ctx, SubmitStopPODCommand{
		TripID:   tripID,
		StopID:   "stop_jaipur_drop",
		TenantID: tenantID,
		PODURL:   "https://s3.example.com/pod_jaipur.jpg",
		OTP:      "9999",
	})
	if err == nil {
		t.Fatalf("Invariant failed: invalid OTP must be rejected")
	}

	// Submit Stop 2 POD with correct OTP -> succeeds
	err = submitStopPODUC.Execute(ctx, SubmitStopPODCommand{
		TripID:       tripID,
		StopID:       "stop_jaipur_drop",
		TenantID:     tenantID,
		PODURL:       "https://s3.example.com/pod_jaipur.jpg",
		SignatureURL: "https://s3.example.com/sig_jaipur.png",
		Notes:        "Received 50 boxes in good condition",
		OTP:          "4321",
	})
	if err != nil {
		t.Fatalf("SubmitStopPOD 2 failed: %v", err)
	}

	// Complete Stop 2
	if err := completeStopUC.Execute(ctx, CompleteStopCommand{TripID: tripID, StopID: "stop_jaipur_drop", TenantID: tenantID}); err != nil {
		t.Fatalf("CompleteStop 2 failed: %v", err)
	}

	// 8. Execute Stop 3 (Udaipur Drop with POD)
	if err := reachStopUC.Execute(ctx, ReachStopCommand{TripID: tripID, StopID: "stop_udaipur_drop", TenantID: tenantID}); err != nil {
		t.Fatalf("ReachStop 3 failed: %v", err)
	}
	if err := submitStopPODUC.Execute(ctx, SubmitStopPODCommand{
		TripID:       tripID,
		StopID:       "stop_udaipur_drop",
		TenantID:     tenantID,
		PODURL:       "https://s3.example.com/pod_udaipur.jpg",
		SignatureURL: "https://s3.example.com/sig_udaipur.png",
	}); err != nil {
		t.Fatalf("SubmitStopPOD 3 failed: %v", err)
	}
	if err := completeStopUC.Execute(ctx, CompleteStopCommand{TripID: tripID, StopID: "stop_udaipur_drop", TenantID: tenantID}); err != nil {
		t.Fatalf("CompleteStop 3 failed: %v", err)
	}

	// 9. Deliver and Complete Trip
	trip, _ = repo.Find(ctx, tripID, tenantID)
	if !trip.AllStopsCompleted() {
		t.Fatalf("expected all stops to be completed")
	}

	if err := trip.Deliver(time.Now().UTC()); err != nil {
		t.Fatalf("Deliver trip failed: %v", err)
	}
	_ = repo.Save(ctx, trip)

	if err := completeTripUC.Execute(ctx, CompleteTripCommand{TripID: tripID, TenantID: tenantID}); err != nil {
		t.Fatalf("CompleteTrip failed: %v", err)
	}

	// 10. Verify persisted trip and stops in database
	finalTrip, err := repo.Find(ctx, tripID, tenantID)
	if err != nil {
		t.Fatalf("Final trip find failed: %v", err)
	}
	if finalTrip.Status != tripagg.TripCompleted {
		t.Fatalf("expected trip status to be completed, got %s", finalTrip.Status)
	}
	if len(finalTrip.Stops) != 3 {
		t.Fatalf("expected 3 stops loaded, got %d", len(finalTrip.Stops))
	}
	for i, s := range finalTrip.Stops {
		if s.Status != tripagg.StopStatusCompleted {
			t.Fatalf("expected stop %d status to be completed, got %s", i+1, s.Status)
		}
	}
}
