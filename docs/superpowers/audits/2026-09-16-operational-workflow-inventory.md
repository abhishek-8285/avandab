# Operational Workflow Inventory

## Confirmed application seams

- Booking API constructs and calls booking application use cases in `internal/booking/presentation/api/handlers/booking_handler.go`.
- Web booking handlers construct and call the same booking use cases in `internal/handlers/bookings.go`.
- Trip API calls assignment, scheduling, lifecycle, and completion use cases in `internal/trip/presentation/api/handlers/trip_handler.go`.
- Web trip handlers call trip application use cases in `internal/handlers/trips.go`.
- Server wiring constructs the shared booking/trip use cases in `cmd/server/main.go:596-648`.
- Booking and trip repositories use unit-of-work and outbox dependencies in their SQL adapters.

## Confirmed risk points

- Driver dispatch SQL mutations and audit writes ignore errors in `internal/driver/application/dispatch_service.go:141,176,190,205,213,333`.
- Razorpay webhook bookkeeping ignores unit-of-work errors in `internal/payment/application/razorpay_webhook.go:290,312`.
- Settlement ledger append errors are ignored in `internal/settlement/application/settlement_service.go:90,105,121,137,153,269,421`.
- Settlement provider-event persistence error is ignored in `internal/settlement/application/settlement_service.go:436`.
- Geofence event persistence errors are ignored in `internal/geofence/application/evaluator.go:312`.
- Route optimization status updates and result persistence errors are ignored in `internal/handlers/routes.go:129,138,145`.

## Boundary decision

Booking and trip already have usable application seams; they should be preserved. First repair error propagation in dispatch, payment webhook bookkeeping, settlement, and geofence. Do not introduce a duplicate booking/trip orchestration layer until those paths are verified.

## Scope for first code changes

1. Add failure-injection tests at existing application seams.
2. Propagate mutation errors and preserve transaction rollback.
3. Re-run focused tests before considering a new dispatch facade.

## Unverified areas

- Complete mobile-to-API call path.
- Production provider retry behavior.
- Full tenant-isolation audit across all legacy handlers.
- End-to-end dispatch planner workflow beyond current trip assignment and driver-offer paths.
