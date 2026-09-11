# C4: Provider go-live runbook (one per integration)

All four providers ship mock-by-default and flip via env only — no code or
migration per go-live. Live HTTP paths are contract-pinned by
`TestLiveContract_{EWayBill,FASTag,GSTN}` (httptest stand-ins assert path +
`X-API-Key` + `*_unavailable` taxonomy); mock ID prefixes are pinned by
`TestMockHonesty_SyntheticIDsCarryMockPrefix`.

Flip procedure (each provider): point endpoint at the sandbox, set prod
creds, set `USE_MOCK=false`, exercise that provider's routes below, watch
for `mock:true` warn logs (must be absent) and `*_unavailable` errors, then
promote the endpoint to prod. Rollback is the reverse env flip. Non-prod
must stay mock — synthetic IDs (`EWB-MOCK-`, `MOCK-`, `JE-MOCK-`,
`EXT-MOCK-`, `<PROV>-MOCK-`) plus `(mock)` messages and warn logs mark demo
data. Exception: GSTN IRNs are format-locked hashes (warn logs + `mock_qr_`).

## EWB (NIC e-waybill)
- Env: `INTEGRATION_EWAYBILL_ENDPOINT` (default `https://ewaybill.nic.in/api`),
  `INTEGRATION_EWAYBILL_API_KEY` (or `EWB_API_KEY`), `INTEGRATION_EWAYBILL_ENABLED`,
  `INTEGRATION_EWAYBILL_USE_MOCK` (default true; `INTEGRATION_EWB_USE_MOCK` alias).
- Live client requires `!UseMock && APIKey != ""`, else stub (honest errors).
- Exercise: `POST /api/v1/integrations/ewaybill/generate|part-a|part-b|cancel|extend`,
  `GET .../get/{ewbNumber}`, `GET .../trip/{tripId}` (`integrations:ewaybill`).
- Note: console one-tap extend additionally gates on `EWB_EXTEND_ENABLED`
  (default false shifts expiry locally without external call).

## GSTN (GSP + e-invoice)
- Env: `INTEGRATION_GSTN_ENDPOINT` (default `https://api.gstn.org`),
  `INTEGRATION_GSTN_API_KEY` (or `GSTN_API_KEY`), `INTEGRATION_GSTN_ENABLED`,
  `INTEGRATION_GSTN_USE_MOCK` (default true), `INTEGRATION_GSTN_USERNAME|
  PASSWORD|CLIENT_ID|CLIENT_SECRET` for GSP auth.
- Exercise: `GET /api/v1/integrations/gstn/validate/{gstin}|gstr1-summary|
  gstr3b-summary`, `POST .../einvoice/irn|push`, `POST .../einvoice/{id}/cancel`
  (`integrations:gstn`). Without creds every call fails closed naming the
  exact missing flag.

## FASTag (NETC)
- Env: `INTEGRATION_FASTAG_ENDPOINT` (default `https://api.fastag.org`),
  `INTEGRATION_FASTAG_API_KEY` (or `FASTAG_API_KEY`), `INTEGRATION_FASTAG_ENABLED`
  (default false — live tolling is doubly gated), `INTEGRATION_FASTAG_USE_MOCK`.
- Exercise: `GET /api/v1/integrations/fastag/balance|transactions`,
  `POST .../deduct|reconcile` (`integrations:fastag`). Live deduct persists
  to `fastag_transactions` and decrements tag balance; without DB + outside
  mock it refuses rather than fabricating SUCCESS.

## Accounting (Tally / Zoho / QuickBooks)
- Env: `INTEGRATION_ACCOUNTING_ENDPOINT`, `INTEGRATION_ACCOUNTING_API_KEY`,
  `INTEGRATION_ACCOUNTING_ENABLED`, `INTEGRATION_ACCOUNTING_PROVIDER`
  (`tally|zoho|quickbooks`, default `mock`), `INTEGRATION_ACCOUNTING_USE_MOCK`.
- Reality check: provider adapters are mock-only today — `!UseMock` returns
  `ErrNotImplemented`, never fake success. Go-live per provider means
  implementing its adapter first; the mock honesty carpet (`<PROV>-MOCK-`)
  and this runbook section stay valid until then.
- Exercise: `POST /api/v1/integrations/accounting/export-invoice|sync-contacts|
  push-journal-entry` (`integrations:accounting`).
