# Integration Register — Owners & Lead Times (2026-11-11)

> Companion to `docs/PLAN-product-details-2026-11-11.md` §3-D.
> Purpose: turn the 19 integration rows (INT-1…INT-19) into an actionable
> procurement/provisioning register. **Credential values never go in this file** —
> only owner, request state, sandbox proof and env key names.
>
> Lead times are **planning estimates (est.)**, not vendor commitments. Replace
> each with the vendor's written timeline when the request is filed.

## Status legend

| Status | Meaning |
|---|---|
| NOT STARTED | No owner assigned, nothing requested |
| REQUESTED | Vendor/account request filed, waiting |
| CONTRACTED | Commercial terms agreed |
| CREDS STORED | Secret in production secret store, `.env.example` updated |
| SANDBOX PROVEN | Round-trip proven against vendor sandbox; evidence linked |
| PROD PROVEN | Live production transaction proven |

Rule: an integration is "live" only at PROD PROVEN. Adapter code existing is not
evidence.

---

## 1. Summary table

| ID | Integration | External dependency | Env keys (source of truth) | Owner | Est. lead time | Status |
|---|---|---|---|---|---|---|
| INT-1a | Razorpay payments | Merchant account + webhook secret | `RAZORPAY_KEY_ID`, `RAZORPAY_KEY_SECRET`, webhook secret | _unassigned_ | 3–7 days after KYC | NOT STARTED |
| INT-1b | RazorpayX payouts | Current account payout enablement + penny-drop | `PAYOUT_RAIL_ENABLED`, `RAZORPAYX_ACCOUNT_NUMBER`, `RAZORPAYX_KEY_ID`, `RAZORPAYX_KEY_SECRET`, `RAZORPAYX_WEBHOOK_SECRET`, `PAYOUT_ENCRYPTION_KEY` | _unassigned_ | 1–3 weeks (KYC + bank linkage) | NOT STARTED |
| INT-2 | E-way bill | GSP/NIC contract + sandbox | `INTEGRATION_EWAYBILL_ENDPOINT`, `INTEGRATION_EWAYBILL_API_KEY`, `INTEGRATION_EWAYBILL_ENABLED` | _unassigned_ | 2–4 weeks | NOT STARTED |
| INT-3 | GSTN IRP e-invoice | GSP/IRP app registration, JWS/JWE certs | `INTEGRATION_GSTN_ENDPOINT`, `INTEGRATION_GSTN_API_KEY`, `INTEGRATION_GSTN_ENABLED` | _unassigned_ | 2–4 weeks (often same GSP as INT-2) | NOT STARTED |
| INT-4 | Vahan/ULIP RC verify | ULIP onboarding or RC-verify vendor | none yet — provider seam `vahan.NewClient(provider)` is mock-only; key names land with the adapter | _unassigned_ | 1–2 days (vendor) / 3–6 weeks (ULIP) | NOT STARTED |
| INT-5 | FASTag | NPCI NETC sponsorship or certified aggregator | `INTEGRATION_FASTAG_ENDPOINT`, `INTEGRATION_FASTAG_API_KEY`, `INTEGRATION_FASTAG_ENABLED` | _unassigned_ | **4–12 weeks — longest pole** | NOT STARTED |
| INT-6 | SMS/OTP | Gateway + **DLT entity, sender ID, template approval** | `SMS_PROVIDER`, `SMS_MSG91_AUTH_KEY`, `SMS_MSG91_SENDER_ID`, `TWILIO_*`, `FAST2SMS_*` | _unassigned_ | 2–4 weeks (TRAI DLT) | NOT STARTED |
| INT-7 | WhatsApp | BSP + WABA + template approval | `WHATSAPP_*` / BSP keys | _unassigned_ | 1–3 weeks | NOT STARTED |
| INT-8 | Expense OCR | Vendor key | `OCR_*` (http seam) | _unassigned_ | 1–3 days | NOT STARTED |
| INT-9 | FCM push | Firebase project + service account | `FCM_PROJECT_ID`, `FCM_SERVICE_ACCOUNT` (path only) | _unassigned_ | 1 day | NOT STARTED |
| INT-10 | Email delivery | Brevo/Resend keys + SPF/DKIM/DMARC | `BREVO_*`, `RESEND_*`, `SMTP_*`, `EMAIL_PROVIDERS_JSON` | _unassigned_ | 1–2 days + DNS propagation | NOT STARTED |
| INT-11a | Google OAuth | OAuth client + consent screen | `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_REDIRECT_URL` | _unassigned_ | 1–3 days (basic scopes) | NOT STARTED |
| INT-11b | Turnstile | Cloudflare site/secret keys | `TURNSTILE_SITE_KEY`, `TURNSTILE_SECRET_KEY` | _unassigned_ | hours | NOT STARTED |
| INT-12 | Object storage / off-host backup | R2 bucket+keys or S3 `ap-south-1`; backup passphrase | `R2_BUCKET_NAME`, `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `BACKUP_ENCRYPTION_PASSPHRASE` | _unassigned_ | 1–2 days | NOT STARTED |
| INT-13 | Map tiles | Google Maps Platform key **or** OSM tile provider | `MAP_TILE_PROVIDER`, `MAP_GL`, `MAP_OSM_URL` | _unassigned_ | 1 day + ToS decision | NOT STARTED |
| INT-14 | Routing/ETA | Self-host OSRM (2–4 CPU, 8 GB, India OSM ~2 GB) or managed | `ROUTING_PROVIDER`, `OSRM_URL` | _unassigned_ | 2–5 days (host) | NOT STARTED |
| INT-15 | LLM + embeddings | Provider account, budget, data terms | `AGENT_API_KEY`, `AGENT_BASE_URL`, `AGENT_MODEL`, `RAG_EMBEDDING_*` | _unassigned_ | 1–3 days + DPA | NOT STARTED |
| INT-16 | Hardware path | OEM cloud API / AIS-140 reseller / own device | n/a (decision) | _unassigned_ | **weeks–months** | NOT STARTED |
| INT-17 | Accounting sync | Zoho Books / QuickBooks app creds (Tally live already) | `INTEGRATION_ACCOUNTING_*` | _unassigned_ | 1–2 weeks | NOT STARTED |
| INT-18 | Video media server | Partner + media server URL | `VIDEO_MEDIA_SERVER_URL` | _unassigned_ | 2–8 weeks | NOT STARTED |
| INT-19 | Cloudflare + VPS | Account, tunnel, DNS, host, backups target | deploy-time config (systemd/env) | _unassigned_ | 1–2 days | NOT STARTED |

---

## 2. Request checklists (what to actually ask the vendor for)

**INT-1 Razorpay / RazorpayX**
- Merchant KYC: entity PAN, GSTIN, bank proof, website/terms URLs.
- Webhook secret for payments; separate webhook secret for payouts.
- RazorpayX: current account number to debit, payout product enablement, penny-drop API scope.
- Sandbox: ₹1 payout + reversal; webhook signature verification sample.
- Keys: `PAYOUT_ENCRYPTION_KEY` is generated locally (`openssl rand -base64 32`), not issued by vendor.

**INT-2/INT-3 E-way bill + GSTN IRP**
- GSP empanelment contract; sandbox credentials; API docs for auth, AES/RSA payload crypto, error codes.
- IRP: app key, JWS/JWE certs, IRN + signed-QR sandbox access.
- Ask explicitly: sandbox rate limits, IP allowlisting, certificate rotation window.

**INT-4 Vahan/ULIP**
- Decide vendor-vs-ULIP first (vendor = fast, ULIP = slower but official).
- Request: RC lookup endpoint, consent/authorisation model, per-call cost, cache rules.

**INT-5 FASTag**
- Decide sponsor-bank/aggregator route; ask for NETC certification scope, settlement/recon file format, dispute handling.
- This gates `AutoKharcha`; until proven, provider mode stays mock and is clearly labelled.

**INT-6 SMS/OTP**
- DLT: entity registration, sender ID (6 chars), template registration for OTP + alerts.
- Gateway: MSG91 / Twilio / Fast2SMS account; fail-loud on HTTP-200 error bodies (already coded).

**INT-7 WhatsApp**
- BSP selection; WABA business verification; template pre-approval (order, trip, EWB, POD classes).
- Ask about per-conversation pricing tiers.

**INT-9 FCM**
- Firebase project, Android/iOS app registration, service-account JSON stored as file path only (`config/firebase-service-account.json`), never committed.

**INT-10 Email**
- Brevo + Resend accounts, SMTP creds, quota confirmation (9k/3k monthly free tiers are planning assumptions).
- DNS: SPF, DKIM, DMARC records for `avandab.com`; `from` address ownership.

**INT-11 Google OAuth**
- Client ID/secret; consent screen branding; redirect URI per environment.
- Note: basic profile/email scopes usually need no verification; sensitive scopes would add weeks.

**INT-12 Storage**
- R2: account ID, bucket, access keys, bucket jurisdiction; **confirm whether jurisdiction meets RBI/DPDP localisation posture** — if not, use S3 `ap-south-1` (region allowlist already coded).
- Off-host restore proof requires a staging target and a named drill owner.

**INT-13/INT-14 Maps + routing**
- Decide Google Maps Platform (billing account, API key restrictions) vs OSM tiles (tile provider ToS).
- OSRM: host spec, India OSM extract, update cadence; or managed routing vendor.
- Fix the ToS-gray unofficial `vt` tile usage noted in `docs/ROADMAP.md` before public launch.

**INT-15 LLM + embeddings**
- Provider, model, monthly budget cap, region/residency, no-training-on-customer-data clause.
- Embedding provider for RAG; hash fallback is not a production quality answer.

**INT-16 Hardware**
- Decision first (see blocker B2). Then: OEM/aggregator API terms, device certification (AIS-140), SIM/M2M APN, FOTA ownership, install/RMA process.

**INT-17 Accounting**
- Zoho Books / QuickBooks developer app, OAuth creds, sandbox company; decide whether Tally-only is acceptable for launch.

**INT-18 Video**
- Partner/media server selection; `VIDEO_MEDIA_SERVER_URL`; camera binding + evidence upload contract.

**INT-19 Cloudflare + VPS**
- Account owner, tunnel token, DNS control for `avandab.com`, TLS mode; VPS provider/region/spec and SSH access; backup target.

---

## 3. Credential handling rules

1. Never commit values. `.env.example` holds placeholders only; real values live in the production secret store (systemd `EnvironmentFile` / host secret store).
2. Record only: owner, request date, secret-store reference name, last rotation date.
3. Rotation: `COOKIE_SECRET`, `API_SECRET`, webhook secrets, payouts key, backup passphrase — document cadence in `docs/08` runbook once owner assigned.
4. Provider keys get least privilege + IP allowlist where vendor supports it.
5. Any vendor failure must fail closed (repo already does for payments/payouts/email/report delivery); do not add a "demo mode" fallback to unblock a pilot.

---

## 4. Owner assignment (fill before G1)

| Role | Name | Responsibility |
|---|---|---|
| Commercial owner | _blank_ | Contracts, KYC, vendor pricing |
| Technical owner | _blank_ | Sandbox proofs, secret storage, adapter config |
| Ops owner | _blank_ | DNS, VPS, Cloudflare, backups, monitoring |
| Sign-off | _blank_ | Accepts each row as PROD PROVEN |

Exit criterion for G1: every INT row has an owner and a requested date; rows
without a vendor decision (INT-4, INT-13/14, INT-16, INT-17) are explicitly
deferred with a date.
