# PLAN — Product Details Needed (2026-11-11)

> Scope: **Avandab / MVTMS fleet SaaS** as it stands on branch `feat/trip-fuel-rollup`
> (2026-11-11 working tree). This is a **details-gathering plan**, not a feature
> roadmap. It answers: *what facts, decisions, contracts, credentials and
> acceptance criteria are still missing before the product can be called
> launch-ready?*
>
> Grounding: `docs/09` (codebase assessment), `docs/PLAN-fleetx-parity-2026-10-28.md`
> §F (decisions needed), `docs/10` §7 (open UX questions),
> `docs/COMPLIANCE-ECOMMERCE-AMENDMENT-2026-09.md` (still-open items),
> `docs/ROADMAP.md`, `.env.example`, and `docs/tech-specs/c4-provider-golive-runbook.md`.
> `docs/*.md` supersede on conflict.

---

## 1. What counts as a "detail"

Four classes. Each needs a different owner and a different artifact.

| Class | Meaning | Owner | Artifact |
|---|---|---|---|
| **D — Decision** | A business/positioning choice only the owner can make; code cannot answer it | Founder/product owner | Decision row in this register + `record_decision` |
| **C — Contract / credential** | External account, key, certificate, partner agreement | Founder/ops, with engineering support | Live sandbox proof + secrets in production store |
| **S — Spec** | Product behaviour, workflow, edge cases, acceptance criterion | Product + engineering | Module spec with Given/When/Then acceptance |
| **O — Operational fact** | Infra, on-call, legal entity, support, release process | Ops/engineering | Runbook or policy page |

Rule: no detail is "done" because it was discussed. Done = linked artifact
(decision recorded, sandbox call proven, spec merged, runbook committed).

---

## 2. Already answered — do not re-ask

The repo is unusually honest; these are settled and should not consume
discovery time.

| Area | Settled state | Evidence |
|---|---|---|
| Architecture & stack | Pure Go, Chi, SQLite WAL, dual-engine migrations, Expo RN | `docs/01`, `docs/09` §1 |
| Feature inventory | Booking→trip→geofence→dispatch, billing/GST, telemetry, fleet/people, platform, P3 modules 3.1–3.12 shipped or seam-ready | `docs/09` §4, PLAN §C |
| Migration state | 179 files/engine, head `00185`, next slot `00186`, PG rehearsal at v185 | `docs/ROADMAP.md`, `c2-pg-cutover-runbook.md` |
| Safety posture | fail-closed tenant default, mock providers fail closed, approval-gated agent mutations, encrypted backups | `docs/09` §2–3, `docs/07` |
| Trust surface | measured `/status`, SLA target 99.9%, `/pricing` "Starter trial / ₹499 Professional / ₹899 Enterprise" | PLAN §C P5, `docs/09` §1 |
| Compliance V1 | footer entity strip, grievance SLA, consent checkbox, `/consumer-compliance` page, terms/refunds V1.1 | `docs/COMPLIANCE-ECOMMERCE-AMENDMENT-2026-09.md` |
| Test/coverage floors | 61.2% Go (money 67.3%), mobile branch 76.31%, CI gates | `docs/09` §1 |

---

## 3. Detail register

### A. Market, segment, launch scope

| ID | Detail needed | Why it blocks / changes | Known today | Owner |
|---|---|---|---|---|
| MKT-1 | Primary segment: mid-market road transporter (software-first) vs large 3PL (hardware + video) | Reorders P3 vs P4; changes pricing, compliance spend, hardware decision | Explicitly open (PLAN §F1) | Founder |
| MKT-2 | Launch geography + states served | GST place-of-supply, state-specific compliance, language, support hours | India implied; no state list | Founder |
| MKT-3 | Target tenant count & fleet size at 6/12 months | Capacity plan; Timescale trigger is >5k trucks; single 1 GB VPS today | "5k-truck claims" flagged as unproven | Founder |
| MKT-4 | Named pilot customer(s), contract status, success criteria | Defines "launch-ready"; drives data migration + acceptance | None in repo | Founder |
| MKT-5 | Launch date + code freeze window | PG cutover needs a freeze window; P0–P5 gating | None | Founder |
| MKT-6 | Support model: self-serve vs assisted onboarding; hours; channels; who is Grievance Officer | Contact page already shows role-based SLAs; role must be filled | Grievance officer role exists in pages, person unset | Founder |
| MKT-7 | Provider budget (GSP, FASTag, SMS, WhatsApp, OCR, maps, LLM) | Determines which integrations are commercially viable in 90 days | None | Founder |

### B. Commercial model & pricing

| ID | Detail needed | Why it blocks | Known today | Owner |
|---|---|---|---|---|
| COM-1 | Final price list: trial length, ₹499/₹899 per-vehicle tiers, annual discount, GST inclusive/exclusive, minimum commitment | Billing/metering code exists; changing metering later is costly | Public page already claims these numbers | Founder |
| COM-2 | Metering definition: "active vehicle" vs registered; overage rule; module add-ons (video, ESG, fuel-card float) | Quota enforcement wired to booking create/complete/cancel | Quota gate exists, definition unconfirmed | Product + eng |
| COM-3 | Trial conversion + data policy at expiry (read-only? purge?) | Dunning sweep already moves PAST_DUE→GRACE→READ_ONLY | Dunning coded, trial policy not | Founder |
| COM-4 | Money movement rules: who pays RazorpayX fees, driver wallet limits, settlement cycle, freight-audit debit notes | Payout rail code complete; policy unset | Code only | Founder + finance |
| COM-5 | Invoice details: seller entity, GSTIN, invoice series, IRN threshold, place of supply | Invoice template + GST engine exist; legal fields still placeholder | CIN/GSTIN missing (compliance doc) | Founder |
| COM-6 | Discount/prior-price display policy (30-day lowest price) | Consumer amendment rule 6 applies when discounts go live | Not implemented | Founder |
| COM-7 | Refund/cancellation policy final sign-off | Terms V1.1 exists; needs legal review | Draft V1.1 | Founder |

### C. Compliance & legal

| ID | Detail needed | Why it blocks | Known today | Owner |
|---|---|---|---|---|
| CMP-1 | Legal entity: CIN, registered office, GSTIN, MSME no, care email/phone | Required on footer/privacy/compliance/invoice | **Still open** (compliance doc) | Founder |
| CMP-2 | NCH convergence registration (consumerhelpline.gov.in) | Consumer e-commerce amendment, effective 1 Jan 2027 | **Still open** | Ops |
| CMP-3 | DPDP rollout: consent manager (Rule 4 live 13 Nov 2026), notice text, DPO/grievance contact, main obligations 13 May 2027 | Consent UI + DSAR APIs exist; legal basis/text not signed | Partial | Founder + legal |
| CMP-4 | Sub-processor list + DPAs (Brevo, Resend, Razorpay, R2, LLM provider, OCR, SMS/BSP, FCM, maps) | Privacy page lists them; contracts must match, esp. data residency | Page exists, DPAs unverified | Ops |
| CMP-5 | Retention schedule sign-off: telemetry/video 90d, audit/breach 3y, consents/DSAR 5y, KYC post-tenure 3y, financial 8y | Worker + purgers already coded to these numbers | Coded, not legally signed | Founder + legal |
| CMP-6 | RBI localisation posture: India-only storage region allowlist, R2 jurisdiction for production bucket | S3/R2 region guard exists; bucket jurisdiction unset | Guard coded | Ops |
| CMP-7 | E-commerce amendment scope statement (marketplace clauses N/A) + dark-pattern self-audit certificate | Must be displayed prominently before 1 Jan 2027 | Scope noted in doc, certificate not produced | Founder |
| CMP-8 | Compliance rails required in next 90 days: E-way bill, GSTN IRP e-invoice, Vahan/ULIP RC, FASTag | Longest lead times; determines Gate 2 | Explicitly open (PLAN §F3) | Founder |
| CMP-9 | SLA/contract terms: 99.9% measurement window, credits, RTO/RPO, exclusions | Public `/pricing` claims 99.9%; must be contractually defensible | Claim live, contract unset | Founder + legal |
| CMP-10 | AI disclosure + processing consent (IT Amendment Rules 2026 label synthetic content) | Assistant header already discloses; consent text unset | Partial | Product + legal |

### D. Integrations, contracts, credentials

| ID | Detail needed | Why it blocks | Known today | Owner |
|---|---|---|---|---|
| INT-1 | Razorpay merchant account + webhook secret; RazorpayX current account, penny-drop, `PAYOUT_ENCRYPTION_KEY`; sandbox ₹1 payout + reversal | Live payout proof missing; payout UI already shipped | Code complete, creds missing | Founder + eng |
| INT-2 | GSP choice + contract for e-way bill (NIC/GSP), sandbox creds | P2.1; produces fake data today when mock | Mock default | Founder |
| INT-3 | GSTN IRP e-invoice app key, JWS/JWE certs, IRN + signed QR sandbox | P2.2 | Mock default | Founder |
| INT-4 | Vahan/ULIP RC verification provider contract | P2.3 `taxverify` provider unimplemented | Missing | Founder |
| INT-5 | FASTag: NPCI NETC sponsorship or certified aggregator | P2.4; `AutoKharcha` deliberately false without real provider | Mock | Founder |
| INT-6 | SMS gateway choice (MSG91/Twilio/Fast2SMS) + **DLT registration, sender ID, template approval** | OTP currently 503 when unconfigured; DLT lead time is weeks | Adapters coded | Ops |
| INT-7 | WhatsApp BSP + WABA + approved templates | P2.6 | Not started | Ops |
| INT-8 | OCR vendor key | Expense OCR is mock-error today | Mock | Founder |
| INT-9 | FCM project + service-account JSON; push token flow | Mobile push needs it | Missing | Eng |
| INT-10 | Email: Brevo/Resend keys, domain SPF/DKIM/DMARC for `avandab.com` | Pool coded with daily/monthly quotas | Missing | Ops |
| INT-11 | Google OAuth client/consent screen; Cloudflare Turnstile keys | Login + bot protection | Seams exist | Eng |
| INT-12 | Object storage: R2 bucket/account/keys + jurisdiction (or S3 `ap-south-1`) + backup passphrase | Off-host restore proof blocked on this | Missing | Ops |
| INT-13 | Map tile decision: official Google API vs OSM tiles (ToS-gray unofficial `vt` URLs noted) | Live map + share/playback pages | Caveat documented | Product |
| INT-14 | OSRM self-host target spec (2–4 CPU, 8 GB, India OSM ~2 GB) or managed routing | Routing provider default is `mock`; ETA quality | Decision + host | Ops |
| INT-15 | LLM provider/model/budget/region + data-processing terms for agent and RAG embeddings | Agent + assistant + RAG are credit-gated | Missing | Founder |
| INT-16 | Hardware path: OEM/aggregator cloud-to-cloud APIs **or** AIS-140 device reseller **or** own device; GT06/AIS-140 device models, SIM/APN | P4 business decision; `vehicle_commands` writes DB and calls nothing | Explicitly open (PLAN §F2) | Founder |
| INT-17 | Accounting: Zoho/QuickBooks live adapters vs Tally-only; partner app creds | Live adapters currently `ErrNotImplemented`; Tally works | Mock for Zoho/QB | Founder |
| INT-18 | Video telematics: hardware partner + `VIDEO_MEDIA_SERVER_URL` | Livestream fails closed until configured | Seam ready | Founder |
| INT-19 | Cloudflare account/tunnel, domain + DNS ownership, TLS, VPS provider/region/spec/access | Deploy path exists but accounts unrecorded | Scripts exist | Ops |

### E. Product & UX decisions

| ID | Detail needed | Why it blocks | Known today | Owner |
|---|---|---|---|---|
| UX-1 | Responsive target: is the web console phone-first for field use, or does the driver app own phones? | Changes P1-6 responsive work from "card fallback" to "block mobile" | **PROPOSED** A (two-surface contract) — [decision brief](DECISION-BRIEF-web-ux-2026-11-11.md) §1 | Product |
| UX-2 | Brand divergence: WhatsApp green in driver app deliberate or legacy drift (converge on ops blue)? | Design-token migration direction | **PROPOSED** A (documented divergence) — [decision brief](DECISION-BRIEF-web-ux-2026-11-11.md) §2 | Product |
| UX-3 | Browser floor: modern-browser-only vs older Android WebView support | Container queries already loaded; test matrix | **PROPOSED** A (Tailwind v4 baseline codified) — [decision brief](DECISION-BRIEF-web-ux-2026-11-11.md) §3 | Product + eng |
| UX-4 | Locale coverage: English + Hindi only? translation ownership, fallback rules | Mobile is bilingual; web locale parity ratchet exists | Partial | Product |
| UX-5 | Module GA vs beta list at launch; feature-flag defaults per tenant | 3.1–3.12 shipped/seam-ready; which are claimable? | Unknown | Product |
| UX-6 | Role/permission matrix final: 6 roles/129 permissions, shipper/STO/driver portal roles | RBAC exists; customer-facing role set unconfirmed | Coded | Product |
| UX-7 | Per-module acceptance criteria (trip states, detention/dwell billing, EWB lifecycle, POD OTP, kharcha approval) | Needed for pilot sign-off and regression scope | Tests exist, product acceptance unstated | Product |
| UX-8 | Notification matrix: which events go to SMS/WhatsApp/email/push/in-app | Comm outbox exists; routing policy unset | Code only | Product |
| UX-9 | Dark-mode + 968 raw-palette classes: accept as debt or fund migration | Ratcheted debt; affects design sign-off | Frozen baseline | Product |

### F. Mobile & release

| ID | Detail needed | Why it blocks | Known today | Owner |
|---|---|---|---|---|
| MOB-1 | Play Console account + listing, package `com.avandab.mobile`, closed-testing track | Cannot publish without account | Config exists | Ops |
| MOB-2 | Signing/upload keystore ownership + storage + EAS credentials | Losing keystore = new app identity | Docs in `mobile/README.md`, keys unset | Eng |
| MOB-3 | iOS scope: Apple Developer account? `bundleIdentifier` exists but no account | iOS release decision | Open | Founder |
| MOB-4 | Store privacy/data-safety forms; crash reporter (PostHog/Sentry) keys + DPA | Required for review | Reporter coded | Ops |
| MOB-5 | Distribution: Play Store vs direct APK vs MDM (fleet phone fleets often sideload) | Determines release process | Undecided | Founder |
| MOB-6 | Update policy: minimum supported version, force-update UX | Backward-compat with server | Undecided | Eng |

### G. AI / agent

| ID | Detail needed | Why it blocks | Known today | Owner |
|---|---|---|---|---|
| AI-1 | Approval policy: which tools gated, who approves, SLA, audit retention | Gate is the safety boundary; already default-on | Default `AGENT_REQUIRE_APPROVAL=true` | Product |
| AI-2 | LLM budget/region/provider + fallback; per-tenant rate limits | Cost + residency | Missing | Founder |
| AI-3 | RL data policy: retention, cross-tenant leakage prevention in few-shot examples, opt-out | High-reward episodes injected into prompts | Local SQLite, policy unset | Product + legal |
| AI-4 | Customer-facing assistant scope + human handoff + disclaimers | Assistant is admin-oriented today | Partial | Product |
| AI-5 | RAG corpus ownership + teaching process + embedding data residency | RAG has hash fallback; quality depends on corpus | Partial | Product |
| AI-6 | MCP server exposure: who can connect, read/write split, approval replay | MCP shipped; client policy unset | Code only | Product |

### H. Ops, security, scale

| ID | Detail needed | Why it blocks | Known today | Owner |
|---|---|---|---|---|
| OPS-1 | PG cutover window + freeze + rollback owner | Rehearsed at v185, not executed; prod on SQLite | Runbook ready | Founder + ops |
| OPS-2 | Infra target: VPS spec/region, HA budget, scale trigger confirmation | Single 1 GB VPS; 5k-truck claims unproven | Known limit | Founder |
| OPS-3 | Off-host restore proof: staging env + R2 artifact + drill owner | Only synthetic CI restore proven | Gap documented | Ops |
| OPS-4 | Monitoring: APM sink, pager/on-call rota, alert routing | p95 breaches log-only; no pager | Gap documented | Ops |
| OPS-5 | Incident process: severity matrix, status-page comms, SLA breach playbook | SLA claims need ops backing | Missing | Ops |
| OPS-6 | Secrets management: where prod secrets live, rotation schedule, who holds keys | systemd/env today; ownership unset | Missing | Ops |
| OPS-7 | Backup RPO/RTO confirmation + test cadence | Encrypted backups + monthly drill coded | Policy unset | Founder + ops |
| OPS-8 | Pentest / security review: scope, vendor, timing; vulnerability disclosure contact | Security gate is internal only | Missing | Founder |
| OPS-9 | Provider quota/rate-limit ceilings and abuse handling | Email pool quotas coded; others unset | Partial | Eng |

### I. Pilot & acceptance

| ID | Detail needed | Why it blocks | Known today | Owner |
|---|---|---|---|---|
| PIL-1 | Pilot scope: tenants, vehicles, modules, duration, acceptance script | "Launch-ready" is undefined without it | None | Founder |
| PIL-2 | Legacy data migration: source system, format, volume, cleansing owner | Blocks onboarding of any real customer | Import tooling unverified | Founder + eng |
| PIL-3 | Training/support material ownership (SOPs, help docs, onboarding wizard copy) | Assisted onboarding claim | Wizard exists | Product |
| PIL-4 | Success metrics: uptime, ingest loss, ticket SLA, activation, NPS | Go/no-go evidence | Partial (status metrics) | Founder |
| PIL-5 | Go/no-go criteria + sign-off owners | Prevents "soft launch by accident" | None | Founder |

---

## 4. Blocking details (answer before funding more build)

These eight gate everything else. Answering them converts most other rows from
"unknown" to "either needed or explicitly deferred".

1. **MKT-1 segment** — mid-market transporter vs large 3PL.
2. **INT-16 hardware scope** — cloud-to-cloud only vs reseller vs own device.
3. **CMP-8 90-day compliance rails** — which of EWB / GSTN IRP / Vahan / FASTag are commercially required now.
4. **CMP-1 + MKT-2 legal entity + launch geography** — unblocks footer, invoices, GST, DPDP.
5. **Deployment model** — shared SaaS only, or on-prem/white-label per customer (affects tenancy, PG cutover, support).
6. **MKT-4 + PIL-1 pilot and acceptance** — defines "done".
7. **COM-1/COM-2 pricing and metering freeze** — billing semantics are cheap to set now, expensive after invoices exist.
8. **Credential/contract owners + lead times** (INT-1…INT-19) — external lead time is the long pole; assign owners now.

---

## 5. Sequencing

| Gate | Timebox | Output | Exit criterion |
|---|---|---|---|
| **G0 — Decisions** | Week 0 | Answers to §4, recorded | 8 decisions recorded with owner + date |
| **G1 — External inventory** | Week 1 | Credential/contract register: owner, lead time, sandbox status | Every INT row has owner and requested date |
| **G2 — Specs & proofs** | Weeks 2–4 | Module acceptance criteria + sandbox proofs (EWB, GSTN, FASTag, RazorpayX, PG freeze window, R2 restore) | Sandbox round-trip proven per rail; PG cutover scheduled |
| **G3 — Pilot** | Weeks 4–8 | Pilot tenant live, acceptance script run, support runbook | PIL-1…PIL-5 signed off |
| **G4 — Launch** | after G3 | Launch checklist + go/no-go | Public claims (pricing, SLA, compliance) all backed by artifacts |

Ordering rule: G0 before G1 (owners unknown otherwise); G1 and G2 run in
parallel; no new feature work until G0 lands — `docs/PLAN-fleetx-parity` P3 is
already segment-dependent.

---

## 6. How the details get captured

- **One register** (this file) is the index; each row links to its artifact.
  Update status in place; do not fork a second tracking doc.
- **Decisions** recorded with `record_decision(decision, reason)` so they survive
  sessions; specs live in `docs/tech-specs/` per existing convention.
- **Credentials** never in docs or repo: request → store in production secret
  store → prove via sandbox call → reference by name only.
- **Spec template** per module: scope, actors/roles, states + transitions,
  Given/When/Then acceptance, data + retention, failure modes, provider seam.
- **Review cadence**: weekly 30-minute detail review against this register;
  every unanswered §4 row is a standing blocker.

---

## 7. Immediate next actions

1. Founder answers the eight blocking details (§4) — one page, no code.
2. Assign owners + requested dates to all INT rows (credential lead times dominate).
3. Pull `docs/10` §7 open questions into product decisions (UX-1…UX-3) — they are small but block sign-off.
4. Produce the pilot acceptance script (PIL-1) from `docs/09` §4 feature inventory.
5. Schedule PG cutover freeze window and R2 restore drill (OPS-1, OPS-3).

---

## 8. Limitations of this plan

- "Product" is assumed to be Avandab/MVTMS; if a different product was meant,
  §1–§3 is a reusable template but the register rows do not apply.
- Owners/dates are placeholders — none are assigned yet.
- Status was read from docs on 2026-11-11; working tree has uncommitted ADR
  Phase 3 refactors, which do not change product-detail gaps.
- Commercial figures (₹499/₹899) are the live public page claims, not validated
  pricing decisions.

---

## 9. Appendices (2026-11-11)

- [Integration register — owners & lead times](INTEGRATION-REGISTER-2026-11-11.md) — INT-1…INT-19 with request checklists and status legend.
- [Blocker questionnaire — 8 decisions](BLOCKER-QUESTIONNAIRE-2026-11-11.md) — answer sheet, options, recommendations, what each answer unblocks.
- [Pilot acceptance script](PILOT-ACCEPTANCE-SCRIPT-2026-11-11.md) — setup, 84 checks, severity, exit criteria, sign-off.
- [Web UX decision brief](DECISION-BRIEF-web-ux-2026-11-11.md) — UX-1…UX-3 options, evidence, recommendations, sign-off sheet.
