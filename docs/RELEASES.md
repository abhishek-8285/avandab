# Release Notes

Append-only log of shipped releases. Each entry: scope, migrations,
behavior changes demanding operator attention, and proof.

---

## b83d328f — GPS hardening + onboarding completion + mobile dead-code purge (2026-09-08)

### Migrations (sqlite + pg mirror, all up/down-tested)
| # | Change | Data note |
|---|--------|-----------|
| 00131 | `drivers.license_number` / `license_expiry` nullable | `DL-PENDING` backfilled to NULL/NULL |
| 00132 | `vehicles.insurance_expiry` / `fitness_expiry` / `permit_expiry` nullable | NO backfill — fabricated +1y indistinguishable from real |
| 00133 | `users.email_verified_at` (NULL = unverified) | Badge only; nothing gates on it |
| 00134 | `telemetry_snapshots.ts_unix` epoch clock | Backfills strftime-parseable rows; Go-String rows stay NULL (honest, never silently wrong) |

sqlc regen: `TelemetrySnapshot.TsUnix`. Index head → `00134`, next free `00135`.

### Behavior changes (operator-visible)
- **Live map**: vehicles silent longer than 2× stale window drop off instead of lingering as permanent `no_signal` ghosts.
- **SSE stream** (`/api/v1/telemetry/stream`): now carries device-health alerts, SOS, and trip lifecycle events; **tenant isolation enforced** — previously any authenticated caller received every org's positions. Trip bus payloads without a tenant stamp (legacy) still pass; stamped events are strictly matched.
- **Sync batch**: mobile must send per-log `id` (it now does); server acks exact `synced_ids` and the client reconciles exactly — retries are dedup-safe.
- **AIS-140**: `*XX` checksum verified when present; corrupted frames rejected (missing checksum still tolerated).
- **Unknown mobile command types**: fail loud as FAILED with no network call (previously POSTed to a nonexistent endpoint and stalled the queue).
- **Onboarding**: unknown license/vehicle dates are NULL end-to-end (no fabricated `DL-PENDING` / +5y dates); email verification is badge-only; reviewer-only gates on license/vehicle verification and assignment.
- **Deleted (mobile, all zero-import proven)**: esign, documentVault, dataRights, fastag, ewaybill clients + EWayBillCard, FirstTimeSetupScreen (1077 lines, hardcoded `DL1LN9999` demo plate), phantom telemetry pipeline (sessions/events endpoints never existed) + second bg task + 3rd sqlite db.

### Rollout notes
- 00134 backfill is online-safe (plain ADD COLUMN + UPDATE, no rebuild).
- Existing mobile installs self-heal `offline_gps_logs` parity columns via upgrade ALTERs at startup.
- Auto trip transitions (`auto_reach_pickup`, `auto_start_transit`) remain default-OFF per tenant — deliberate rollout flags, not a bug.
- Deferred (roadmap, not gaps): ignition TripStart/TripStop (Phase 2), Timescale path, `ts_unix` adoption in readers, trip-event tenant for non-forwarded types.

### Proof
- `go build ./...`, `go vet ./...`, `gofmt` clean; full `./internal/...` + `./db/...` green; mobile `tsc` + 52/52 suites (336 tests).
- Security gate (`security-check.sh`) 8/8 green; pre-commit sqlc-integrity hook caught and fixed one regen miss before commit.
- Live HTTP A/B: pre-fix binary leaks a 2020 ghost as `no_signal`; fixed binary excludes it. Zero-ID sync probe: 3 fixes → 1 position pre-fix.

---

## 544247ca — Storage, scale & ops hardening cycle (2026-09-10)

### Migrations (sqlite + pg mirror; up/down recorded in ownership index, not re-verified this refresh)
| # | Change | Data note |
|---|--------|-----------|
| 00135 | `vehicles.registration_number` UNIQUE → UNIQUE(`tenant_id`, `registration_number`) + `vehicle_latest_position` UNIQUE(`tenant_id`,`vehicle_id`) | Plates become per-tenant (telemetry H3+L10) |
| 00136 | `telemetry_positions.vehicle_id` `''` → NULL backfill | idempotent; down is documented no-op |
| 00137 | `vehicle_commands` table + `av_operator` role | AV teleoperation deck |
| 00138/00139 | Partial + list scale indexes (outbox, alerts snooze, fuel audits, sessions, vehicles/drivers, invoices, payments, audit logs) | read-only perf |
| 00140 | `files.tenant_id` + triggers | **Operator-visible: file reads are now strictly tenant-scoped** — closes cross-tenant licence/RC/POD read via UUID |

### Behavior changes (operator-visible)
- **Files** (`00140`): viewer/`%:read` roles can no longer read another org's uploads by UUID. Existing rows backfilled to bootstrap tenant — verify multi-tenant file access after deploy.
- **Plates** (`00135`): two tenants may now hold the same registration number; live-map cache is tenant-unique.
- **Live telemetry**: etag-304 + spatial bbox + delta sync; ack-after-accept; cross-tenant device/vehicle/driver ID spoof rejected; SSE keep-alive; telemetry partition retention cleaner.
- **Ops**: Cloudflare Turnstile bot protection on public forms; egress origin shield + immutable static caching; PWA service worker + offline precache + client image compression; Google gl=IN live-map tiles, viewports locked to India; trips auto-seed default pickup/drop stops on `/trips/new`.
- **Deploy**: `scripts/deploy-vps.sh` — local stripped cross-compile + rsync + systemd swap + `/health` gate. Compiling on the 1 GB VPS is blocked (policy + AGENTS.md prohibition #6).

### Proof (2026-09-10 refresh at `544247ca`)
- `go build ./...` exit 0; `go vet ./...` exit 0.
- `go test ./internal/... ./db/... -count=1`: **120 packages ok, 0 FAIL** (`/tmp/gotest_refresh.log`).
- Mobile: 56/56 suites, 351/351 tests (`npx jest`).
- Security gate: `LINT_BASE=$(git rev-parse HEAD) ./scripts/security-check.sh` — all checks passed.
- Docs reconciled same day: `docs/09` survey refresh, `docs/ROADMAP.md` head `00140`, C5 rewrite of `AVANDAB_ZERO_COST_ARCHITECTURE.md`.
