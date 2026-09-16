# Operational Workflow Architecture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the booking-to-settlement workflow propagate failures correctly and expose one reliable application seam across existing adapters.

**Architecture:** Preserve the modular monolith and existing booking/trip application use cases. First repair confirmed silent-failure paths, then introduce only the smallest dispatch orchestration seam required by actual callers.

**Tech Stack:** Go, Chi, SQLite/Postgres, sqlc, Goose, unit-of-work transactions, outbox events, Testify.

**Spec:** `docs/superpowers/specs/2026-09-16-operational-workflow-architecture.md`

## Global Constraints

- Do not edit existing migrations; add only new migrations when schema change is proven necessary.
- Derive tenant identity from request/application context; never hardcode tenant IDs.
- Do not discard production mutation errors.
- Preserve existing public API response contracts unless a failing test proves they are incorrect.
- Run `go build ./...`, `go vet ./...`, `go test ./internal/...`, migration checks, and `LINT_BASE=$(git rev-parse HEAD) ./scripts/security-check.sh` before claiming completion.

---

### Task 1: Establish baseline and mutation inventory

**Files:**
- Create: `docs/superpowers/audits/2026-09-16-operational-workflow-inventory.md`
- Read: `internal/booking/application/*.go`
- Read: `internal/trip/application/*.go`
- Read: `internal/driver/application/dispatch_service.go`
- Read: `internal/payment/application/razorpay_webhook.go`
- Read: `internal/settlement/application/settlement_service.go`
- Read: `internal/geofence/application/evaluator.go`

**Interfaces:**
- Consumes: existing use-case constructors, unit-of-work interfaces, repository interfaces, and event/outbox writers.
- Produces: exact caller-to-use-case map and list of mutation errors currently discarded.

- [ ] Trace web, API, agent, and worker entrypoints for booking, trip, assignment, payment, settlement, and geofence mutations.
- [ ] Record transaction owner, tenant source, audit behavior, outbox behavior, and error behavior for each mutation.
- [ ] Add only confirmed findings to the audit file with file and line references.
- [ ] Review the audit for scope before changing code.

### Task 2: Lock dispatch error propagation with failing tests

**Files:**
- Modify: `internal/driver/application/dispatch_service.go`
- Test: `internal/driver/application/services_test.go` or a focused new dispatch failure test beside the implementation.

**Interfaces:**
- Consumes: `DriverAppService.ProcessDriverCommand` and its existing repository/executor dependencies.
- Produces: errors from offer state updates, audit writes, and related command persistence reach the caller and roll back the transaction.

- [ ] Add a failing test using a repository/executor double that returns an update error during accept/reject/complete command handling.
- [ ] Run the focused test and verify it fails because the current implementation discards the error.
- [ ] Replace discarded mutation errors with returned errors; retain ignored cleanup errors only where cleanup is best-effort and documented.
- [ ] Run the focused dispatch tests and verify pass.
- [ ] Run adjacent driver application tests.

### Task 3: Lock payment webhook bookkeeping failures

**Files:**
- Modify: `internal/payment/application/razorpay_webhook.go`
- Test: `internal/payment/application/razorpay_webhook_test.go`

**Interfaces:**
- Consumes: `RazorpayWebhookUseCase.Execute` and existing unit-of-work/payment repositories.
- Produces: webhook bookkeeping transaction errors return from `Execute`; payment state cannot appear successful when provider-event persistence fails.

- [ ] Add a failing test that injects a provider-event or webhook-record persistence failure.
- [ ] Run the focused webhook test and verify failure is observable.
- [ ] Propagate the transaction error and preserve existing duplicate-event/idempotency behavior.
- [ ] Run all payment application tests.

### Task 4: Make settlement ledger writes atomic and observable

**Files:**
- Modify: `internal/settlement/application/settlement_service.go`
- Read: `internal/settlement/infrastructure/persistence/sql/settlement_repository.go`
- Test: `internal/settlement/application/settlement_service_test.go`

**Interfaces:**
- Consumes: settlement service methods and `AppendLedgerEntry` repository interface.
- Produces: ledger append errors abort the owning operation and are visible to callers; no partial payout state is reported as complete.

- [ ] Identify each ignored ledger append in the service and map it to its owning transaction.
- [ ] Add a failing test for each distinct mutation pattern, not each line.
- [ ] Return ledger errors and verify rollback/no-success response.
- [ ] Run settlement tests, including dual-write tests.

### Task 5: Make geofence event/outbox persistence fail closed

**Files:**
- Modify: `internal/geofence/application/evaluator.go`
- Test: `internal/geofence/application/evaluator_test.go`

**Interfaces:**
- Consumes: `Evaluator.EvaluateFix` and existing database/event-bus dependencies.
- Produces: geofence transition, event log, and outbox writes either commit consistently or return an error without publishing a false success.

- [ ] Add a failing test for event persistence failure and a separate test for outbox persistence failure.
- [ ] Run focused evaluator tests and verify they fail before the fix.
- [ ] Enforce transaction error propagation and prevent downstream publication when persistence fails.
- [ ] Run the complete geofence application test set.

### Task 6: Define the smallest shared dispatch application seam

**Files:**
- Create: `internal/dispatch/application/service.go`
- Create: `internal/dispatch/application/service_test.go`
- Create: `internal/dispatch/domain/errors.go`
- Modify: `internal/driver/application/dispatch_service.go` only where adapter delegation is required.
- Modify: `cmd/server/main.go` to construct the seam.

**Interfaces:**
- Consumes: existing trip assignment use cases, driver offer logic, tenant context, and audit/outbox ports.
- Produces: a dispatch application interface with explicit operations for propose, accept, reject, and confirm assignment; callers do not coordinate multiple repositories themselves.

- [ ] Define command/result types with tenant ID, actor ID, target trip/driver/vehicle IDs, and idempotency key where applicable.
- [ ] Add service tests for authorization/state validation, tenant isolation, idempotent retry, and transaction failure.
- [ ] Implement using existing use cases and ports; do not duplicate trip state rules.
- [ ] Wire API and agent callers to the service only after tests pass.
- [ ] Run dispatch, trip, driver, and agent tests.

### Task 7: Verification and security gate

**Files:**
- No source changes unless verification exposes a defect.

- [ ] Run `go build ./...`.
- [ ] Run `go vet ./...`.
- [ ] Run `go test ./internal/...` with local socket access when required.
- [ ] Run migration up/down tests.
- [ ] Run `LINT_BASE=$(git rev-parse HEAD) ./scripts/security-check.sh`.
- [ ] Inspect `git diff` and confirm unrelated user files remain untouched.
- [ ] Record exact outputs and known limitations in the final verification report.
