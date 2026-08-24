# Scale Tiering — Implementation Spec v1

Status: draft (handoff from Spec 22 §10-S14 — to be hardened before build)
Depends-on: Spec 22 (Command Center), Spec 01 (telemetry ingestion),
Spec 04 (live map/SSE), Spec 05 (alerting), 00-migration-ownership-index.md
Migration owner: db/migrations/00100_*.sql onwards (reserved in index when
this spec is activated; nothing reserved today)

## 0. Verified ground truth

Verified 2026-08-24:

- Storage is single-node SQLite (`modernc.org/sqlite`) with WAL; all
  repositories go through `internal/repository/sqlite` + sqlc-generated
  queries (`db/generated/sqlite/`).
- Telemetry writes land in `telemetry_positions` / `telemetry_snapshots`
  (00040/00041); `vehicle_latest_position` is a derived latest-state row
  consumed by the console fleet strip (`internal/handlers/console.go:Fleet`).
- SSE fan-out is per-replica in-process (`go sseHub.Run(ctx)` in
  `cmd/server/main.go`); no cross-replica pub/sub exists.
- Background jobs are leader-elected via `internal/leader` on
  `worker_leases` (00079): outbox relay, geofence dwell, fuel engine,
  compliance radar sweep, alerts snooze sweep.
- Event bus (`internal/events`) is in-memory per process; durable side
  effects flow through the outbox relay.
- No read replicas, no table partitioning, no retention jobs exist today;
  `telemetry_snapshots` grows unbounded.

## 1. Overview / goal

Keep the product at its current operational simplicity until a real scale
trigger fires, then move in four bounded steps:

1. **Retention** — bound telemetry growth (biggest unbounded table first).
2. **Read scaling** — offload dashboards/reports from the write DB.
3. **Ingest sharding** — split telemetry ingest by vehicle when a single
   writer saturates.
4. **Postgres migration** — one-way door, only after multi-replica writes
   or reporting isolation is unavoidable.

Explicit non-goals: multi-region, k8s orchestration, per-tenant physical
sharding, Kafka-style streaming backbone. SQLite + WAL comfortably handles
the pilot's write volume (~tens of rows/s peak).

## 2. API contract

No new public APIs. Internal-only changes:
- Retention worker deletes in batches (≤5k rows/txn) — invisible to clients.
- Read-replica routing is repository-internal (`db.go` DSN selection);
  handlers stay unchanged.

## 3. DB contract

### 00100_telemetry_retention.sql (first migration when activated)
```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS retention_state (
    key        TEXT PRIMARY KEY,
    last_run   DATETIME,
    deleted_rows INTEGER NOT NULL DEFAULT 0
);
-- +goose Down
DROP TABLE IF EXISTS retention_state;
```
Deletion policy (config, not schema): keep raw positions 90d, snapshots
13 months, experiment_events 180d, notification_log 90d.

## 4. UI

None. Operational spec.

## 5. Business logic

Trigger thresholds (measure first, migrate second):
| Trigger | Metric | Action |
|---|---|---|
| T1 retention | telemetry tables > ~5 GB or > 50M rows | run retention worker |
| T2 read replica | dashboard p95 > 500ms while ingest p95 stable | Postgres read replica via logical replication or nightly dump-restore |
| T3 ingest shard | sustained >200 position rows/s or WAL contention errors | shard ingest by hash(vehicle_id) across N SQLite files behind the existing provider interface |
| T4 Postgres | any multi-writer requirement OR T3 shards ≥4 | port sqlc queries (dialect swap), goose dialect change, dual-write cutover |

Leader-elected workers already guarantee single-runner semantics per
replica set; retention joins the same pattern (`RunAsLeader`).

## 6. Config / env

| var | default | purpose |
|---|---|---|
| `RETENTION_ENABLED` | false | gates the retention worker |
| `RETENTION_POSITIONS_DAYS` | 90 | raw GPS retention |
| `RETENTION_SNAPSHOTS_DAYS` | 395 | snapshot retention |
| `READ_REPLICA_DSN` | empty | when set, read-heavy repos route here |

## 7. Tests

- Retention worker: batch boundary test, respects cutoff, leaves recent
  rows intact, records `retention_state`.
- Replica routing: unit-test DSN selection only (no live replica in CI).
- Shard router: hash distribution + per-shard failure isolation.
- Full `go build/vet/test` + hook suite as every step.

## 8. Future / provider

- TimescaleDB evaluation for GPS hypertables at T4.
- NATS/Redis pub/sub ONLY if cross-replica SSE fan-out becomes a real need.

## 9. Edge cases

1. Retention running during user query → WAL allows concurrent readers;
   batch small enough to avoid starvation.
2. Shard rebalance moves a vehicle mid-trip → drain old shard for that
   vehicle before switching (hash-sticky buffer).
3. Replica lag → dashboards may show stale positions; console already
   falls back to poll + shows `at` timestamps (Spec 04).
4. Migration 00100+ must remain SQLite-compatible until T4 lands.

## 10. Phased rollout

R0 measure (add row counts to founder KPIs) → R1 retention →
R2 replica reads → R3 ingest shards → R4 Postgres cutover.
Each phase gated on its trigger metric, reviewed in the weekly ops note.

## 11. Open items / VERIFY

1. Actual current row counts + DB size on production device (measure at R0).
2. Backup/restore story before ANY retention deletes run.
3. Postgres cutover cost estimate: count non-portable SQL (SQLite-specific
   date functions used in radar/KPI/settlement queries).

## 12. File list

CREATE (when activated): `db/migrations/00100_telemetry_retention.sql`,
`internal/service/retention_service.go`, `internal/repository/router.go`.
MODIFY: `cmd/server/main.go` (worker wiring), `internal/config/config.go`.
