# Compliance Note - Consumer Protection (E-Commerce) (Amendment) Rules, 2026

Notified: 9 Sept 2026 (G.S.R. 789(E), Ministry of Consumer Affairs)
PIB: 10 Sept 2026 5:14PM
Effective: 1 Jan 2027
Amends: Consumer Protection (E-Commerce) Rules, 2020

## Must display on every e-commerce / service website

1. Entity: legal name, HQ + branch addresses, website details
2. Contact: customer-care email, landline, mobile + Grievance Officer details
3. Grievance: acknowledge 48h, share copy of recorded complaint, redress in 1 month, join NCH convergence
4. Search: no manipulation; disclose ranking factors in plain language, descending importance
5. Sponsored: clear prominent label, distinct from organic
6. Pricing: on discount show reduced + prior price; prior = lowest price in preceding 30 days
7. Invoice: seller name same font size as platform name
8. Dark patterns: follow Guidelines 2023, yearly self-audit, display compliance certificate prominently
9. Seller/product: business name, GSTIN, MSME no, address, care number, email, ratings, country of origin + importer details for imports, best-before, return/refund/exchange, warranty, delivery/shipment/return-shipping cost, payment modes; provide seller address post-purchase on request
10. Consent/fees: no secondary use of consumer info without express affirmative consent; no bundled fees for unrelated services (loyalty/membership excepted)

## Related Sept 2026 context
- DPDP Rules 2025 phased: Consent Manager Rule 4 live 13 Nov 2026, main obligations 13 May 2027
- IT Amendment Rules 2026: label synthetic/AI content
- NSE: accessibility audit (RPwD/WCAG) filing deadline 31 Oct 2026 via ENIT

## Action
Update footer, seller onboarding, checkout, invoice template, grievance SOP before 1 Jan 2027.

## Implemented (Sept 2026, working tree)
- Footer entity/grievance/NCH/certificate strip (`partials/footer.html`) + `/consumer-compliance` certificate page, route, sitemap/robots/llms
- Privacy: role-based Grievance Officer + phone/hours, dual DPDP (3d/30d) vs consumer (48h/1mo) SLAs
- Contact: SLA notice, ticket copy/print, server SLA state (ack-overdue / redress-overdue / on-track / redressed)
- Terms/Refunds V1.1: 30-day request, 5–7-day processing, 1-mo redress, NCH refs
- Invoice pay page: upfront-pricing note, grievance + policy links
- Register: explicit unchecked DPDP consent box, server `agree=yes` gate
- Migration 00156 `contact_submissions.acknowledged_at` (sqlite + pg); `POST /api/v1/support/tickets/{ticket}/status` + `GET /api/v1/support/tickets` behind `users:manage`; first touch stamps ack
- Assistant header carries AI-generated disclosure
- Scope: first-party B2B SaaS — marketplace ranking/sponsored/seller-registry clauses N/A by design

## Still open (needs human, not code)
- CIN + full registered-office address for footer/privacy/compliance placeholders
- NCH convergence registration on consumerhelpline.gov.in portal (operational, no public API)
- Prior-price display when public plans/discount page launches
- Admin inbox UI over the tickets API (API suffices until volume justifies)
