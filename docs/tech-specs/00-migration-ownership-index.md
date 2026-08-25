# Migration Ownership Index

Single source of truth for `db/migrations/` version numbers. Repo head is
`00039_experiments.sql` (TAKEN — never edit). Every new migration appends
`00040` and up. **This table is authoritative; spec §3 numbers MUST match it.**

## Rules
- ONE feature owns ONE migration number. Never reuse, never renumber an
  existing `db/migrations/0000x_*.sql`.
- `company_config` / `company_settings`: created ONCE. Spec 02 creates it at
  `00042`. Every other spec only seeds rows or adds columns — never a second
  `CREATE TABLE`.
- Every new migration has correct `-- +goose Up` / `-- +goose Down`.
- `tenant_id` is a free-form `TEXT` (no `tenants` table exists). Do NOT add
  `FOREIGN KEY (tenant_id) REFERENCES tenants(id)` — it will fail `goose up`.

## Canonical allocation (verified non-overlapping)

| # | Owner | Spec |
|---|-------|------|
| 00039 | experiments (TAKEN — do not touch) | existing |
| 00040 | telemetry devices / raw_events / provider_poll_state / quarantine | 01 |
| 00041 | telemetry_positions / vehicle_latest_position / snapshots enrichment | 01 |
| 00042 | geofence engine + **canonical `company_config` create** | 02 |
| 00043 | fuel audit + driver scorecard (seeds `company_config` only) | 03 |
| 00044 | live map + share links + maintenance | 04 |
| 00045 | alerting pipeline (alert_rules, alert_events, alert_routes, notification_prefs) | 05 |
| 00046 | compliance reporting + files | 05 |
| 00047 | e-way bill lifecycle (eway_bills, eway_bill_events) | 07 |
| 00048 | GST e-invoice (line items, invoice_sequences, CGST/SGST/IGST, hsn_sac_master, company state code) | 07 |
| 00049 | FASTag (tags + transactions) | 07 |
| 00050 | Accounting sync log + mapping + gl rules | 08 |
| 00051 | Driver settlement engine (INSERT fix + settlement_lines + TDS) | 08 |
| 00052 | Document vault (driver_documents, vehicle_documents + expiry cols) | 08 |
| 00053 | Event bus / outbox correction (status/attempts/last_error) | 09 |
| 00054 | Booking hardening (reverse_fare, status history, tenant FK) | 09 |
| 00055 | Trip POD hardening (pod_otp_hash, converter/aggregate fields) | 09 |
| 00056 | Auth hardening (api_tokens, sessions tenant, drivers.user_id, enc token) | 10 |
| 00057 | Payment Razorpay fields (order_id, payment_id, signature, event_id) | 11 |
| 00058 | PNL / ops / experiments / founder (pnl_snapshots, notification_log, error_reports, incidents, experiment_assignments, login_audit, founder channels) | 16 |
| 00059 | telemetry_alerts rebuild (widened CHECK, 13 types) | 05 |
| 00060 | experiments RBAC permissions & role assignments | 16 |
| 00061 | founder RBAC permissions & role assignments | 16 |
| 00062 | user theme preferences (users.theme_preference) | 12 |
| 00063 | RAG multi-tenant vectors + provider registry | 10 |
| 00064 | org_admin role & RBAC permissions (tenant-scoped organization admin) | 10 |
| 00065 | Tenant backfill (normalize NULL/'' → '1' for 15 tables, idempotent) | 10 |
| 00066 | Route optimization jobs + constraints (Spec 18) | 18 |
| 00067 | ETA history + monthly aggregation (Spec 18) | 18 |
| 00068 | Backhaul matching (no DB — uses existing routes/trips) | 19 |
| 00069 | STO portal + load board listings (Spec 19) | 19 |
| 00070 | CX: customer tracking timeline (no DB — uses 00044 share) | 20 |
| 00071 | Fuel cards + accounting sync extension (Spec 20) | 20 |
| 00072 | ESG snapshots (Spec 20) | 20 |

> NOTE (2026-08-24): rows 00068–00072 are RESERVED-UNBUILT — specs 19/20
> own these numbers but no `.sql` files ship yet. Verified absent from
> `db/migrations/` on 2026-08-24 (spec 22 §11.5). Do NOT treat the gaps
> as free slots and do NOT renumber; they activate with their owning
> specs.| 00073 | Churn: compliance shipper portal (customer_users, dispatch_overrides, trip_feedback, POD columns) | 21 |
| 00074 | Churn: driver offline sync log | 21 |
| 00075 | Churn: vernacular i18n keys | 21 |
| 00076 | Churn: expense idempotency (idempotency_key unique partial index) | 21.1 |
| 00077 | Files: generic polymorphic upload types (trip_pod, expense_receipt, logo, general) + entity index | Fleetbase-style files API |
| 00078 | RBAC: rag:read / rag:write permissions for /api/rag/* gating | Security audit fix M2 |
| 00079 | Worker leader-election lease table (worker_leases) | Multi-instance safety |
| 00080 | Backfill: legacy POD photos company_logo → trip_pod (uploadable_id ∈ trips) | POD privacy fix |
| 00081 | Rebuild `trips` — status CHECK gains reached_pickup/in_transit/delivered (Spec 09 lifecycle) | Spec 13 mobile e-POD E2E fix |
| 00082 | driver_expenses geo capture (latitude/longitude) | Spec 13 mobile kharcha flow |
| 00083 | error_reports + incidents DDL (carried over from 00058 gap) — renumbered from duplicate 00081 | Spec 16 §3 |
| 00084 | error_reports correlation: request_id + metadata (JSON breadcrumbs) columns | Spec 16 §5.5 |
| 00096 | notification_log — alert delivery audit trail | Alerting follow-up |
| 00097 | money_ledger — append-only txn ledger (payment_recorded hook live) | Invoice/txn system wave 1 |
| 00098 | credit_debit_notes + note_sequences — GST post-issuance corrections | Invoice/txn system wave 2 |
| 00099 | invoices.irn_cancelled_at — IRN 24h cancel window | Spec 07 continuation |
| 00100+ | future specs | reserved |

> NOTE: Spec 13 briefly held 00084/00085 for these same migrations during a
> concurrent-session collision on 2026-08-22; renumbered to 00086/00087 per the
> "next free slot" rule. Nothing shipped on the old numbers.| 00086 | `driver_issues` table — mobile Report-Issues screen (FleetBase parity) | Spec 13 |
| 00087 | `trips.pod_scan_value` — barcode/QR POD proof column | Spec 13 |
| 00088 | driver_expenses rebuild — category CHECK gains rto/tyre/bhatta (mobile/server parity fix) | India milestone M1 |
| 00089 | feature_flags table + features:update permission (per-org feature registry) | Plugin/feature-flag milestone |
| 00090 | trips pod_otp + pod_otp_expires_at (e-POD OTP verification) | E2E completion milestone |
| 00091 | `route_locations` side-table — geocoded endpoint coordinates for routes (gap #46 standardized locations) | Location standardization milestone |
| 00092 | alert inbox hardening (ack_status / severity_rank / money_at_risk / snoozed_until + inbox index) | 22 |
| 00093 | `driver_advance_requests` (driver Paisa tab advance flow) | 22 |
| 00094 | `driver_expenses` verification (verification_state / flag_reason / ocr_amount / ocr_confidence) | 22 |
| 00095 | tenant_id columns for `driver_expenses` + `maintenance_records` (fixes PnL/money-strip silent zero aggregation) | 22 |

> NOTE: Spec 13 briefly held 00084/00085 for these same migrations during a
> concurrent-session collision on 2026-08-22; renumbered to 00086/00087 per the
> "next free slot" rule. Nothing shipped on the old numbers.

## Implementation rule
Pick the number from this table for your feature and update the spec's §3 to
match exactly. If two specs appear to need the same number, the FIRST spec in
the table wins and the other moves to the next free slot (update this index).
