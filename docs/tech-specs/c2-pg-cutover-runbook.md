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
- `sqlite2pg --dry-run`: require exit 0 inside rolled-back txn.
- `sqlite2pg` (live): require exit 0, `quarantined: 0`. Review `_migration_quarantine` on PG for anything unexpected.

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

Note: this rehearsal validated the empty-PG chain + tooling, not a live data
copy (the local sqlite DBs were v29/v137, older than head). Live data copy
still needs the production sqlite at head (§2–§3).

## Timescale decision
Deferred. Cut over to plain PG first; revisit only on proven hypertable
need (>5k-truck telemetry volume per ROADMAP trigger). No code changes for it now.

## History archival
Covered by the telemetry partition retention cleaner (built, ROADMAP Phase A);
no additional archival step for cutover. At head v182
`vehicle_latest_positions` exists on both engines (2026-11-09 rehearsal:
179/179 tables match), so it migrates normally — the older "absent on PG"
note only applied to the v149 chain.
