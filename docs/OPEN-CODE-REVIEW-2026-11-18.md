# Open Code Review — 2026-11-18

Scope: the existing Avandab / MVTMS Indian transport platform in this workspace
(Go server, mobile app, OpenAPI contract, migrations and tests). Goal: validate
the current code, review it using open coding guidelines, and fix real defects.

## Method and tool note

No Alibaba-specific review tool is installed or configured in this workspace
(checked `.mcp.json`, `opencode.json`, the available tool catalog and a
repo-wide search). "Alibaba open code review" was therefore applied as the open
Alibaba P3C guideline categories adapted to Go: correctness, concurrency
safety, error handling, fail-closed security, resource cleanup and naming.
Checks used: `go build`, `go vet`, `golangci-lint v2` (the project's own config)
and `go test`, plus manual review of the changed security-sensitive code.

## Validation results

| Check | Result |
| --- | --- |
| `go build ./...` | PASS (exit 0) |
| `go vet ./...` | PASS (exit 0) |
| `golangci-lint run --new-from-rev=HEAD ./...` | PASS — 0 issues in the changed code |
| `go test` on deviation, ewaybill, geofence/application, maintenance | PASS (exit 0) |
| F-03 regression test (`TestSafePublish*`) | PASS — failing bus logged, nil bus safe |
| `golangci-lint run ./...` (whole repo) | PASS — 0 issues (was 170: 128 errcheck, 42 gocritic; retired in the follow-up pass below) |
| `go test ./internal/... -timeout 60s` | 140 packages pass, 52 have no tests, 3 time out at 60s |
| The 3 slow packages (`agent`, `handlers`, `service`) | PASS with a 20m timeout: agent 431.9s, handlers 421.6s, service 91.7s, exit 0 |
| Craft Anything `rebuild_check` | PASS — build passed, own-tests passed |

The 60s timeouts were a harness limit, not a compile or assertion failure: each
of those packages rebuilds the full 179-migration SQLite fixture for its tests.
The toolchain wrapper below was needed because the check runner's minimal PATH
did not contain `go`.

## Findings

### F-01 — HIGH — one-time OTP could be replayed concurrently (FIXED)

`internal/auth/otp.go`, shared-cache path. `verifyCached` read the code record,
incremented the attempt counter, wrote it back, compared the code and finally
deleted the key. None of that is atomic, so two requests arriving together
could both read the same unconsumed code and both return success. A regression
test with 32 concurrent callers across 4 "replicas" sharing one cache saw 5–7
acceptances per run instead of exactly 1.

Fix: after the code matches, the caller takes an atomic claim using the cache's
`Increment` on a key derived from the code hash. Only the increment that
returns 1 may delete the key and accept. Backends without `Increment` fall back
to a local mutex. `internal/auth/otp_replay_test.go` is red on the old code and
green on the fix (10/10 runs).

### F-02 — HIGH — password-reset token could be redeemed concurrently (FIXED)

`internal/auth/reset_token.go`, same class of bug: two concurrent `Consume`
calls could both read the token and both succeed after both `Delete` calls
returned without error. The regression test saw 2 acceptances instead of 1.

Fix: expiry is checked first, then an atomic claim on a per-token-hash counter
decides the single winner before the purge; same local-mutex fallback.
`internal/auth/reset_replay_test.go` is red on the old code and green on the fix
(10/10 runs).

### F-03 — MEDIUM — event publish errors discarded (FIXED)

`internal/deviation/engine.go`, `internal/ewaybill/monitor.go`,
`internal/geofence/application/evaluator.go` and `internal/maintenance/worker.go`
ignored errors from `events.Publish`, so a failed publish could silently lose an
alert or automation trigger. Fix: each component now routes every publish
through a small `safePublish` helper that logs the event type and the error,
with a nil-logger fallback. No publisher contract or public signature changed,
and the outbox rows these components already persist are untouched. The
regression test `internal/deviation/publish_error_test.go` proves a failing bus
is logged and a nil bus is a safe no-op. Other packages still contain unchecked
publishes on outbox-backed paths; those are outside this targeted review.

### F-04 — LOW — `os.Exit` skips deferred cleanup (FIXED)

`cmd/server/main.go` (11 startup-failure branches), `cmd/gps-sim/main.go`
(socket close + ticker stop) and `cmd/sqlite2pg/main.go` (DB closes, context
cancel) called `os.Exit` while defers were pending. Fixed by moving each body
into `run() error` with a single `os.Exit` in `main`: every failure path now
returns through main, so deferred cleanup executes. `cmd/rag/main.go` and
`cmd/mqttpass/main.go` were re-checked — their exits happen before any defer is
registered, so they needed no change (contrary to the initial note).
Regression test `cmd/server/run_test.go` proves `run()` fails closed (returns
an error, no exit) on committed default secrets.

### F-05 — LOW — legacy lint debt (FIXED)

Re-baselined at 170 findings (128 errcheck, 42 gocritic). Now 0:

- 22 unchecked event-bus publishes fixed in code with logging helpers (below).
- 8 actionable unchecked errors fixed in code: RAG temp-file close and remove,
  RAG vector-store `db.Close` on constructor failure, dashboard SSE write,
  multipart upload closes.
- 42 gocritic findings fixed: 35 handler variables renamed away from the `id`
  package, plus `notifications`, `gstin`, `filepath`, `db` and `events` shadows,
  `typeDefFirst` (invoice use case) and `tooManyResults` (`dailyTotals` now
  returns a small struct).
- The remaining read-only `Close` findings (`*sql.Rows`, `*sql.Stmt`,
  `io.ReadCloser`) are excluded in `.golangci.yml` with a written rationale,
  consistent with the pre-existing `(*database/sql.Tx).Rollback` exclusion:
  the handle is released either way and `rows.Err()`/read errors already surface
  real I/O failures. Writable handles are still checked in code.

### F-06 — MEDIUM — `npm test` failed where Go is not on PATH (FIXED)

Craft Anything's check runner reported `own-tests: failed — sh: 1: go: not
found` even though the Go toolchain is installed at `/home/abhishek/go/bin/go`.
`package.json` called `go` directly and inherited a minimal PATH. Added
`scripts/go.sh`, which resolves the toolchain from PATH or the standard install
locations, and pointed the npm test scripts at it. `rebuild_check` now passes.

## Follow-up pass (same day)

Closed the remaining open items after the initial report.

### Silent publish failures closed everywhere

The initial pass logged publish failures in deviation, ewaybill, geofence and
maintenance. The follow-up covered every remaining unchecked publish found by
`errcheck`:

- `internal/service/service.go` — new `baseService.safePublish` (nil-bus no-op,
  nil-logger fallback to `slog.Default`, warns with event type + error); 19
  call sites switched in booking, trip, payment, telemetry, ops-alert, kharcha,
  invoice, compliance and driver-settlement services.
- `internal/payment/application/razorpay_webhook.go` — `safePublish` for the
  two failed-payment webhook events (the HTTP 200 ack makes losses invisible).
- `internal/telemetry/ingest.go` — post-commit notification publishes warn on
  failure.
- `internal/geofence/infrastructure/worker/dwell_worker.go` — zone-breach
  publish warns on failure.
- `internal/handlers/sos.go` — SOS publish warns on failure.

Test: `internal/service/safe_publish_test.go` proves a failing bus is logged
(with event type), a nil bus is a silent no-op, and a nil logger does not panic.

### Tooling

- `scripts/go.sh` made executable (direct `./scripts/go.sh` invocation no longer
  needs `sh`).
- `cmd/server/run_test.go` — regression test for F-04 fail-closed startup.

## What changed this review

- `internal/auth/otp.go` — atomic single-use claim in the cached verify path.
- `internal/auth/reset_token.go` — atomic single-use claim in the cached consume path.
- `internal/auth/otp_replay_test.go` — new concurrency regression test.
- `internal/auth/reset_replay_test.go` — new concurrency regression test.
- `scripts/go.sh` and `package.json` — make the project's test command work on runners without Go on PATH.
- `internal/deviation/engine.go`, `internal/ewaybill/monitor.go`,
  `internal/geofence/application/evaluator.go`, `internal/maintenance/worker.go` —
  publish failures are now logged instead of discarded (F-03).
- `internal/deviation/publish_error_test.go` — regression test for F-03.
- This report.

## Residual risk / next step

No check is currently failing: `go build ./...`, `go vet ./...`,
`go test ./...`, `golangci-lint run ./...` (0 issues) and the security gate are
green. F-01 through F-06 are all closed. Remaining highest-value next work is an
owner decision on which product area to extend next (e.g. a specific Indian
compliance workflow), or making event delivery durable end to end (the outbox
relay retries, but in-memory publish failures are now logged rather than
retried).

## Second review pass — fail-closed persistence + PII masking

Re-scanned the newest feature packages (cold chain `00177`, EV `00176`, privacy
`00179`) for the same silent-failure class as F-03/F-05, plus PII correctness.
Three more real defects found, all red-proven before the fix and green after.

### F-07 — HIGH — cold-chain excursion records silently lost (FIXED)

`internal/coldchain/service.go`: `IngestTelemetry` discarded the error from
every `CreateExcursion`/`UpdateExcursion` call. A temperature breach is a
regulated GDP/HACCP record for Indian pharma and vaccine logistics; if the
write failed (disk full, lock), the telemetry frame was still stored with no
audit trail for the breach. Fix: each write failure now returns a wrapped
error (`persist/update/resolve temperature excursion: %w`), so the ingest fails
closed and no orphan telemetry is stored. New test
`internal/coldchain/excursion_persistence_test.go` drives a failing repository
stub; it failed on the old code (error was nil) and passes on the fix, and also
asserts the healthy path still stores telemetry.

### F-08 — MEDIUM — EV charging-session writes silently lost (FIXED)

`internal/ev/service.go`: the same defect class in `CreateChargingSession` /
`UpdateChargingSession`. A dropped session write corrupts charging cost (INR
tariffs) and the carbon-displacement accounting that feeds ESG snapshots. Fix:
all three write sites return wrapped errors and the ingest fails closed. New
test `internal/ev/charging_session_persistence_test.go` proves red-on-old,
green-on-fix for create, update and healthy paths.

### F-09 — LOW — PII mask emitted invalid UTF-8 for Indian-language email IDs (FIXED)

`internal/privacy/mask.go`: `MaskEmail` took `local[0]` (one byte), so an email
local part beginning with a multibyte character (e.g. `रवि@…`, `அமித்@…`)
produced a broken byte (`à***@…`) instead of a valid masked address. Fix: index
by rune (`[]rune(local)[0]`). New subtest in `internal/privacy/mask_test.go`
covers Devanagari, Tamil and Latin-accented local parts and asserts the output
is valid UTF-8.

Verification for this pass: `go build ./...`, `go vet ./...` exit 0;
`go test ./internal/coldchain ./internal/ev ./internal/privacy/... -count=1`
all PASS; each regression test was run before its fix and failed as expected.
