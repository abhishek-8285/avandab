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

## Timescale decision
Deferred. Cut over to plain PG first; revisit only on proven hypertable
need (>5k-truck telemetry volume per ROADMAP trigger). No code changes for it now.

## History archival
Covered by the telemetry partition retention cleaner (built, ROADMAP Phase A);
no additional archival step for cutover. `vehicle_latest_positions` is absent
on PG by design (purge-probe view; empty at rehearsal) — migrate manually if
ever non-empty (dry-run flags it).
