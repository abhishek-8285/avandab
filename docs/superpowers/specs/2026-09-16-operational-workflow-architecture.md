# Operational Workflow Architecture

## Goal

Make booking-to-settlement behavior consistent across web, mobile, agent, and workers without rewriting the database or splitting the system into services.

## Decision

Keep the current modular monolith. Business actions must run through application use cases. Presentation adapters may parse requests and map responses, but may not execute business SQL directly.

## First slice

Implement and verify this workflow first:

```text
booking → trip → driver/vehicle assignment → execution → delivery → payment/settlement
```

Existing booking and trip application use cases remain the initial seam. We will add missing orchestration only where actual call paths require it.

## Module responsibilities

- `booking`: booking lifecycle and customer order state.
- `trip`: trip and stop state machine, POD, completion.
- `dispatch`: assignment proposals, offers, planner workflow, exceptions.
- `fleet`: driver, vehicle, eligibility, maintenance constraints.
- `billing`: invoice, payment, e-way bill, tax state.
- `settlement`: immutable driver ledger and payout state.
- `tracking`: telemetry, geofence, ETA facts and events.

## Transaction rules

Every mutation must:

1. derive tenant from context;
2. validate authorization and state transition;
3. execute through a unit of work;
4. persist domain state, audit record, and outbox event atomically;
5. return persistence errors to the caller;
6. support idempotent retry where the operation can be retried.

External provider calls are recorded as retryable operations and are not held inside database transactions.

## Event ownership

The module that owns a state transition emits its event. Other modules consume events and update their own projections or ledgers. Tracking observes telemetry and requests trip transitions through the trip application interface; it does not update trip tables directly.

## Migration strategy

No database rewrite and no microservices. Freeze new direct-SQL handler features, add regression tests at application seams, migrate one mutation path at a time, and delete legacy paths only after equivalent contract tests pass.

## First implementation targets

1. Audit booking/trip/dispatch call paths and document actual seams.
2. Add failure tests for ignored persistence errors in dispatch, payment webhook bookkeeping, settlement ledger writes, and geofence outbox writes.
3. Fix error propagation and rollback behavior.
4. Add one shared dispatch application seam used by API, web, agent, and worker callers.
5. Verify with build, vet, internal tests, migration tests, and security gate.

## Non-goals

- No full rewrite of existing aggregates.
- No new microservices.
- No migration renumbering or edits to existing migrations.
- No feature expansion before the first workflow slice is reliable.
