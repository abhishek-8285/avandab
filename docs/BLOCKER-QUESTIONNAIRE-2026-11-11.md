# Blocker Questionnaire — 8 Decisions (2026-11-11)

> Companion to `docs/PLAN-product-details-2026-11-11.md` §4.
> Purpose: one page the owner can fill in ~20 minutes. Each answer converts a
> whole band of register rows from "unknown" to "needed or deferred".
>
> How to answer: put A/B/C (or "defer + date") in §9. Short answers fine.
> "Defer" is a valid answer only with a date and a named owner.
> Recommendations below are engineering's suggestion, clearly marked — they are
> not existing product decisions.

---

## B1. Segment — who is the product for?

**Question:** primary segment for the next 6–12 months?

- **A. Mid-market road transporter** (5–200 trucks, own fleet operations).
  Software-first; hardware optional; strongest fit for TMS + kharcha + GST.
- **B. Large 3PL / enterprise shipper** (200+ trucks, multi-leg, customer portals).
  Needs hardware/video, compliance rails, white-label portal, longer sales cycle.
- **C. Both, same product, different packaging.** Highest scope; slowest.

**Changes:** P3 module order, P4 hardware decision, pricing tier emphasis,
compliance spend (B forces EWB/FASTag/video earlier), pilot selection.

**Engineering recommendation:** A first. The built surface (trip/detention/kharcha/
GST/fuel/tyre) matches mid-market operations; B's hardware/video path is still
seam-only. Revisit B after A pilot revenue.

**Owner:** founder. **Unblocks:** MKT-1, MKT-3, UX-5, PIL-1.

---

## B2. Hardware — is hardware in scope at all?

**Question:** how do vehicle positions/telemetry reach the system?

- **A. Cloud-to-cloud only.** Consume OEM/aggregator APIs; no devices we own.
  Cheapest, fastest, no RMA/install burden; dependent on OEM coverage.
- **B. Resell a certified AIS-140 tracker.** Branded device, vendor handles FOTA;
  real hardware story, medium cost/ops.
- **C. Own device.** Certification, supply chain, install network, RMA. Only if
  hardware margin is the business.
- **D. Defer** — ship software with existing GT06/AIS-140 TCP ingest only.

**Changes:** P4 roadmap, device lifecycle/FOTA/SIM work, support model, margin
model, telemetry provider integrations (`TELEMETRY_WEBHOOK_SECRET_*` parity).

**Engineering recommendation:** A or D for launch; B only after a paying fleet
asks. `vehicle_commands` currently writes DB and calls nothing — do not claim
remote commands until a transport exists.

**Owner:** founder. **Unblocks:** INT-16, MKT-7, OPS-2.

---

## B3. Compliance rails — which are commercially required in 90 days?

**Question:** rank these four; put "not now" on the rest.

1. E-way bill generation (GSP/NIC) — produces mock data today.
2. GSTN IRP e-invoice (IRN + signed QR) — for GST-registered B2B above threshold.
3. Vahan/ULIP RC verification — onboarding speed vs manual document upload.
4. FASTag toll reconciliation — 4–12 week lead time, highest effort.

**Changes:** Gate 2 scope, provider contracts, cash outlay, pilot feature claims.
Do not claim any rail as live until sandbox round-trip is proven.

**Engineering recommendation:** (1) + (3) first; (2) only if pilot customers
cross the e-invoice threshold; (4) only with a sponsor-bank/aggregator path
chosen and after A pilot density justifies it.

**Owner:** founder. **Unblocks:** CMP-8, INT-2/3/4/5, P2.

---

## B4. Legal entity + launch geography

**Question:** what exact legal identity and where do we sell/operate?

Need: legal name, CIN, registered office, GSTIN, MSME no, care email/phone,
states served, invoice state code + place-of-supply default.

**Changes:** footer/privacy/terms/compliance pages, invoice template, GST
engine configuration, DPDP notices, support hours. Currently placeholders.

**Engineering recommendation:** provide the entity details even if states are
"pan-India"; placeholders on legal pages block any paid pilot.

**Owner:** founder + legal. **Unblocks:** CMP-1, MKT-2, COM-5.

---

## B5. Deployment model

**Question:** how is it hosted per customer?

- **A. Shared multi-tenant SaaS** (current default, `MULTI_TENANT_ENABLED=true`).
- **B. Dedicated instance per customer** (single-tenant deployments).
- **C. On-prem / VPC for enterprise** (customer-hosted).
- **D. Hybrid** — SaaS default, dedicated only above a contract size.

**Changes:** tenancy assumptions, PG cutover shape, ops toil, support cost,
pricing floor, data-residency answers, backup/restore scope.

**Engineering recommendation:** A now, D later. B/C multiply ops work before
there is a single proven tenant.

**Owner:** founder. **Unblocks:** OPS-1, OPS-2, MKT-6, CMP-6.

---

## B6. Pilot + "done"

**Question:** what is the first real customer and what proves success?

Need: tenant name/segment, vehicle count, modules in scope, duration, success
metrics (e.g. 2 weeks live telemetry with zero loss, 20 trips closed, invoice
generated, 0 Sev1), who signs off.

**Changes:** acceptance script scope, data migration scope, onboarding effort,
launch date credibility. Without this, "launch-ready" is undefined.

**Engineering recommendation:** one midsize transporter, 10–25 vehicles, 4 weeks,
core flows only (booking→trip→POD→invoice→payment→kharcha); leave AI, ESG,
video out of pilot claims.

**Owner:** founder. **Unblocks:** PIL-1…PIL-5, MKT-4.

---

## B7. Pricing + metering freeze

**Question:** is the public price list final and what exactly is metered?

Confirm: trial length, ₹499 Professional / ₹899 Enterprise per vehicle per month,
GST inclusive/exclusive, annual discount, minimum commitment, and the meter —
active vehicles at billing cutoff? registered? trips? plus module add-ons.

**Changes:** quota enforcement semantics, invoice lines, dunning, sales
collateral, `/pricing` claims. Changing meter after invoices exist requires
credit notes and customer comms.

**Engineering recommendation:** meter = active vehicles at month end; modules
included in tier, not individually priced, for launch. Trial 14 days.
Leave prior-price discount display off until a discount actually launches.

**Owner:** founder + finance. **Unblocks:** COM-1/2/3, MKT-7.

---

## B8. Credential/contract owners + dates

**Question:** who owns each external relationship, and when is each requested?

Need names for: commercial owner, technical owner, ops owner, sign-off owner,
plus requested date per integration (see `docs/INTEGRATION-REGISTER-2026-11-11.md`).

**Changes:** everything external. Longest lead times (FASTag 4–12 weeks, DLT
2–4 weeks, GSP/IRP 2–4 weeks) must start now or be explicitly deferred.

**Engineering recommendation:** assign one commercial owner and one technical
owner today; request EWB/GSTN + DLT + RazorpayX this week; defer FASTag and
hardware explicitly.

**Owner:** founder. **Unblocks:** G1 exit.

---

## 9. Answer sheet (fill and date)

| # | Decision | Answer (A/B/C or defer + date) | Decided by | Date | Notes |
|---|---|---|---|---|---|
| B1 | Segment |  |  |  |  |
| B2 | Hardware |  |  |  |  |
| B3 | Compliance rails (90d) |  |  |  |  |
| B4 | Legal entity + geography |  |  |  |  |
| B5 | Deployment model |  |  |  |  |
| B6 | Pilot + done criteria |  |  |  |  |
| B7 | Pricing + metering |  |  |  |  |
| B8 | Owners + requested dates |  |  |  |  |

When filled: record material choices with `record_decision(decision, reason)`,
update `docs/PLAN-product-details-2026-11-11.md` §4 status, and treat B1–B8 as
closed only when the linked artifact exists (contract, page, spec, or pilot
agreement).

## 10. What each answer mechanically unblocks

| Answer | Unblocks |
|---|---|
| B1 | P3 order, UX-5, PIL-1, pricing emphasis |
| B2 | INT-16, P4, telemetry/OEM integrations, support model |
| B3 | P2 contracts (INT-2/3/4/5), Gate 2, cash plan |
| B4 | CMP-1/5/9, COM-5, footer/invoice/legal pages |
| B5 | CMP-6, OPS-1/2, MKT-6, backup scope |
| B6 | PIL-1…5, data migration scope, launch date |
| B7 | COM-1/2/3, quota enforcement, `/pricing` truth |
| B8 | G1 exit, all INT rows, lead-time schedule |
