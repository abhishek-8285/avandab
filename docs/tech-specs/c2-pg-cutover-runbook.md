# C2: PG cutover runbook — freeze → migrate → verify → flip → rollback

Executable checklist: `scripts/pg-cutover.sh [--check]`. Rehearsed end to end
2026-09-11 on scratch PG16 (evidence below). Preconditions: PG reachable,
`migrations_pg` at head (A7 two-phase startup auto-migrates), `sqlite2pg`
built (`go build -o sqlite2pg ./cmd/sqlite2pg`).

## 1. Freeze (stop writes)
`systemctl stop avandab` (stops API + TCP `:5023` GPS ingest + MQTT — one
binary, no drain mode exists). Confirm: `curl /login` → `000`.

## 2. Backup (online-safe)
Never `cp` a WAL-mode sqlite file (rehearsal produced a malformed copy that
way). Use the backup API:
`sqlite3 "file:transport.db?mode=ro" ".backup transport.db.backup-<stamp>"`
then `PRAGMA integrity_check` → `ok`. The live migrator opens sqlite
read-only and never modifies it — the pre-freeze file is itself a fallback.

## 3. Migrate
> **Do not run a bare `goose -dir db/migrations_pg up`.** Migrations from
> 00137 onward insert seed rows whose ids come from identity sequences that
> earlier seed migrations (explicit ids) left out of sync — a plain chain fails
> with `duplicate key value violates unique constraint "roles_pkey"` at 00137.
> The server startup handles this (A7 two-phase: `UpTo(72)` →
> `ResyncIdentitySequences` → `Up`); any external migrator must replicate it.
> Confirmed by the 2026-11-09 rehearsal below.

- `sqlite2pg --check`: require `sqlite vN vs pg vN -> MATCH`, `dirty INTEGER
  columns: 0`, `orphan FK rows: 0`. Seed collisions (`-> merged by name`)
  are by design (runtime seeds differ by engine).
- **Trigger-enforced tenant FKs are invisible to the check.** Migrations
  00103+ enforce tenant isolation with triggers, not FK constraints, so
  `PRAGMA foreign_key_check` reports `orphan FK rows: 0` even when users point
  at missing tenants. Run the explicit prechecks first and fix/accept any rows:
  ```bash
  sqlite3 "$SQLITE_DB" "SELECT 'users', COUNT(*) FROM users WHERE tenant_id NOT IN (SELECT id FROM tenants)
    UNION ALL SELECT 'sessions', COUNT(*) FROM sessions WHERE user_id NOT IN (SELECT id FROM users)
    UNION ALL SELECT 'user_roles', COUNT(*) FROM user_roles WHERE user_id NOT IN (SELECT id FROM users);"
  ```
- Legacy sqlite DBs may still carry a `workspaces` table (workspace-era
  tenancy) with no PG migration owning it — the live copy quarantines those
  rows. Nothing in the head code references `workspaces`; archive/drop it
  before cutover rather than migrating it.
- `sqlite2pg --dry-run`: require exit 0 inside rolled-back txn.
- `sqlite2pg` (live): require exit 0 and **review every quarantined row** in
  `_migration_quarantine` (expected 0; known-legacy classes above are the only
  acceptable ones, and only after a row-level review).

## 4. Verify (on PG, before flip)
Boot with `DATABASE_DRIVER=postgres DATABASE_URL=...`: expect `Database
migrated successfully`, `GET /login` → 200, `POST /api/v1/auth/token`
(existing user) → 200, authenticated `GET /api/v1/trips` → 200. Spot-check
`roles`/`permissions` counts vs `--check` report.

## 5. Flip
Set `DATABASE_DRIVER=postgres` + `DATABASE_URL` in the service environment
(env-only change, `config.go:356`) and `systemctl start avandab`. Keep the
sqlite backup for one retention window.

## 6. Rollback
`systemctl stop avandab`, unset `DATABASE_DRIVER` (sqlite file untouched
throughout), `systemctl start avandab`, re-run §4 smokes against sqlite.

## Rehearsal evidence (2026-09-11, PG 16.15 scratch, ports 18081/18082)
| Step | Result |
|---|---|
| sqlite boot 138 → auto-migrate 149, viewer register | `/login` 200, token issued |
| freeze | `/login` 000 |
| backup API copy | `integrity_check` ok, v149, 104 users |
| empty-PG boot (two-phase startup) | `Database migrated successfully`, v149, 9 roles |
| `--check` | versions MATCH, 0 dirty, 0 orphans |
| live migrate | exit 0, 44/44 tables, 2332 rows, 0 quarantined |
| PG-live smokes | `/login` 200, token 200 (migrated user), `GET /trips` 200 |
| rollback (backup boot) | `/login` 200, token 200 |

## Rehearsal evidence (2026-11-09, PG 18.2 Termux local, port 55432, head v182)
| Step | Result |
|---|---|
| empty-PG chain (two-phase helper: UpTo 72 → resync → Up) | v182, 176 migrations applied (67 pre + 109 post), 0 errors |
| schema parity vs fresh sqlite v182 (179 tables) | table-name sets identical, 0 missing on either side |
| seed parity | roles 9, permissions 144, tenants 2, alert_rules 17 on both engines |
| full down-migration (DownTo 0) | 176 migrations rolled back, 0 public tables remaining |
| re-up after down | v182 again, 0 errors (chain is reversible + repeatable) |
| server boot on PG (`DATABASE_DRIVER=postgres`, port 18080) | `Database migrated successfully`; `/login` 200, `/healthz` 200, `/status` 200; multi-tenant resolver active |
| availability sampler against PG | 1 healthy sample written to `availability_samples` |
| `sqlite2pg --check` (fresh sqlite v182 vs PG v182) | `MATCH`, 0 dirty, 0 orphan FK rows, 0 missing tables, seed collisions 0 |
| `sqlite2pg --dry-run` | exit 0, 14/14 tables, 0 quarantined, sequences reset 5 |

### Data-copy rehearsal (2026-11-09, local dev mvtms.db copy, v137 → v182)
| Step | Result |
|---|---|
| online backup copy → integrity_check | ok, v137 |
| sqlite chain on the copy (v137 → v182) | 45 migrations applied, 0 errors (real rows preserved) |
| `sqlite2pg --check` | versions MATCH, 0 dirty, 0 orphan FK rows, 52 seed collisions merged by name |
| `sqlite2pg --dry-run` | exit 0, 24 tables, 0 quarantined (rolled back) |
| `sqlite2pg` live copy | exit 0; 21 tables processed, rows copied for audit_logs/drivers/experiment_events/outbox_events/pnl_daily/role_permissions/worker_leases; **8 users + 8 sessions + 8 user_roles + 8 workspaces rows quarantined** |
| row parity (non-quarantined tables) | roles 9, permissions 144, role_permissions 422, drivers 1, audit_logs 1, experiment_events 9, pnl_daily 6, outbox_events 1, worker_leases 17, company_config 46 — all match |
| quarantine root cause | dev DB retained 8 workspace-era users (`users.tenant_id = ws_<hash>`, no matching `tenants` row); sessions/user_roles cascade-quarantined; `workspaces` has no PG owner and no code references at head |

This validates the copy pipeline on real rows. It is **not** a production-data
rehearsal: the local sqlite was dev/test data (10 users, 1 driver, no
trips/vehicles/invoices). The live cutover still needs the production sqlite at
head (§2–§3) and a row-level review of any quarantine.

## Timescale decision
Deferred. Cut over to plain PG first; revisit only on proven hypertable
need (>5k-truck telemetry volume per ROADMAP trigger). No code changes for it now.

## History archival
Covered by the telemetry partition retention cleaner (built, ROADMAP Phase A);
no additional archival step for cutover. At head v182
`vehicle_latest_positions` exists on both engines (2026-11-09 rehearsal:
179/179 tables match), so it migrates normally — the older "absent on PG"
note only applied to the v149 chain.
