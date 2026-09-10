# 🚚 Avandab Multi-Tenant Logistics Platform
## 100% Zero-Cost / Free-Tier Enterprise Architecture & Operations Manual

> **Reconciliation note (2026-09-10, ROADMAP C5):** §§2/4B/6 rewritten to observed reality; §3/§5 caveats added. Operational authority for deployment is `docs/08`. The ₹0 claim is the *platform baseline* — usage-billed extras are listed honestly in the Final Cost Summary caveats.

---

## 📌 1. Executive Summary & Cost Philosophy
This architecture document outlines the complete zero-cost, high-reliability infrastructure design for the **Avandab Freight Management System**. The system operates with **₹0.00 monthly recurring software overhead** by leveraging enterprise-grade Always Free tiers and automated routing pipelines.

```
┌──────────────────────────────────────────────────────────────────────────────────────────────────┐
│                                 AVANDAB ZERO-COST ECOSYSTEM MAP                                  │
├──────────────────────────────────────────────────────────────────────────────────────────────────┤
│                                                                                                  │
│   [Shippers / Customers]         [GPS Hardware Trackers]          [Fleet Drivers / Mobile]       │
│             │                               │                                │                   │
│             ▼                               ▼                                ▼                   │
│   ┌──────────────────┐            ┌──────────────────┐             ┌──────────────────┐          │
│   │ Cloudflare Edge  │            │ TCP Port :5023   │             │ WhatsApp / SMS   │          │
│   │ (CDN / SSL / DNS)│            │ (Raw Telemetry)  │             │ (Direct Dispatch)│          │
│   └─────────┬────────┘            └─────────┬────────┘             └─────────┬────────┘          │
│             │                               │                                │                   │
│             └───────────────────────┬───────┴────────────────────────────────┘                   │
│                                     │                                                            │
│                                     ▼                                                            │
│                    ┌──────────────────────────────────┐                                          │
│                    │   1 GB amd64 VPS / TECNO Termux  │                                          │
│                    │   Ingress: Cloudflare Tunnel     │                                          │
│                    │   • Golang Backend (:8080)       │                                          │
│                    │   • SQLite WAL (+ PG parity)     │                                          │
│                    │   • comm_outbox mail relay       │                                          │
│                    └────────────────┬─────────────────┘                                          │
│                                     │                                                            │
│                                     ▼                                                            │
│                    ┌──────────────────────────────────┐                                          │
│                    │ Outbound Email Relay (Port 587)  │                                          │
│                    │ • Brevo (9,000/mo Free)          │ ──► [Primary Inbox Delivery]             │
│                    │ • Resend (3,000/mo Free)         │     `billing@avandab.com` (DKIM PASS)    │
│                    └──────────────────────────────────┘                                          │
└──────────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 🖥️ 2. Compute Infrastructure (observed reality — reconciled 2026-09-10)

| Parameter | Configuration | Notes |
| :--- | :--- | :--- |
| **Primary production target** | 1 GB amd64 VPS behind Cloudflare Tunnel | Deployed via `scripts/deploy-vps.sh` — local stripped cross-compile + rsync + `systemctl avandab.service` + `/health` gate |
| **Alternate / legacy target** | TECNO-LE7 Android phone in Termux | `deploy_avandab.sh` (ADB) / `deploy_remote.sh` (SSH, port 8090), watchdog `scripts/ensure-avandab-running.sh` |
| **Compile rule** | Binaries built locally (`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 -trimpath -ldflags "-s -w"`) | NEVER compile on the 1 GB target — OOM/swap-thrash (`docs/08` Option C) |
| **Database** | SQLite WAL primary, dual-engine PG parity maintained | `db/migrations_pg/` mirrors all 134 migrations; `cmd/sqlite2pg` + `internal/datamigrate` for cutover |
| **Swap Buffer** | 2.0 GB `/swapfile` on 1 GB instances | Prevents OOM memory spikes |
| **Runtime Service** | Golang native binary under `systemd` (VPS) or Termux background (phone) | Health: `http://localhost:8080/health` |

Earlier revisions of this section claimed a GCP `e2-micro` at `34.42.182.104` — that content was aspirational and never observed in the deploy scripts. Superseded by the table above (ROADMAP C5).

### Key Server Ports:
* **Port 8080 (TCP)**: Avandab Web Management Cockpit & API.
* **Port 5023 (TCP)**: Real-Time Hardware GPS Telemetry Ingestion (Teltonika / AIS-140).
* **Port 587 (TCP)**: Outbound Encrypted Mail Submission (STARTTLS).
* **Port 22 (SSH)**: Secure Remote Server Administration.

---

## 🌐 3. Edge Networking, DNS & Security (Cloudflare Free Tier)

* **Authoritative DNS**: Sub-millisecond global DNS resolution.
* **Edge CDN & Caching**: Static assets (CSS, JS, SVG logos) cached in Indian edge datacenters (Delhi, Mumbai, Chennai, Bangalore), speeding page loads by 10x.
* **Automatic SSL / TLS**: Free 256-bit encryption for `avandab.com` and all subdomains.
* **DDoS & Web Application Firewall (WAF)**: Mitigates malicious volumetric attacks at zero cost.
* **Live-map tiles caveat**: the web live map defaults to free OSM tiles (`MAP_TILE_PROVIDER`, default `auto`); some public share/playback templates embed unofficial Google `vt` tile URLs (`mt1.google.com`) — ToS-gray, not an official free API. Prefer OSM for strict zero-cost/ToS cleanliness.

---

## 📬 4. Enterprise Email Architecture (15+ Branded Inboxes)

### A. Incoming Email Pipeline (Cloudflare Email Routing - Free)
All incoming customer, driver, and partner emails are routed dynamically to the central administrator mailbox with zero per-user charges.

| # | Email Address | Department | Function |
| :--- | :--- | :--- | :--- |
| 1 | `billing@avandab.com` | Finance | GST Invoices & Razorpay Payment Receipts |
| 2 | `accounts@avandab.com` | Accounts | Transporter Settlements & TDS Ledger Reconciliation |
| 3 | `dispatch@avandab.com` | Fleet Ops | Trip Assignments & Route Coordination |
| 4 | `tracking@avandab.com` | Telematics | Live GPS Tracking Links & ETA Updates |
| 5 | `epod@avandab.com` | Delivery | Digital Signed Proof of Delivery & Cargo Photos |
| 6 | `support@avandab.com` | Helpdesk | General Customer Care & Shipper Assistance |
| 7 | `sos@avandab.com` | Emergency | 24/7 Highway Breakdown, Accident & SOS Radar |
| 8 | `driver-help@avandab.com` | Driver Care | Driver Welfare & Highway Toll Assistance |
| 9 | `fastag@avandab.com` | Toll Desk | FASTag Toll Double-Deduction & Blacklist Disputes |
| 10 | `claims@avandab.com` | Insurance | Cargo Damage Claims & Detention Disputes |
| 11 | `fuel@avandab.com` | Audit | Diesel Card Allocation & Fuel Theft Logs |
| 12 | `maintenance@avandab.com`| Workshop | Highway Mechanic Assistance & Repairs |
| 13 | `compliance@avandab.com` | Legal | NIC E-Way Bill Alerts & Section 194C TDS Docs |
| 14 | `legal@avandab.com` | Legal | Corporate Freight Contracts & Privacy Policies |
| 15 | `sales@avandab.com` | Enterprise | B2B Shipper Contracts & Dedicated Fleet Sales |
| 16 | `onboarding@avandab.com` | Fleet KYC | Transporter Registration & Driver Verification |
| 17 | `abhishek@avandab.com` | Leadership | Founder / Executive Office Official Email |
| 18 | `security@avandab.com` | Security | Vulnerability Disclosure & Data Protection |
| 19 | `no-reply@avandab.com` | Auth | Automated Login OTPs & System Security Alerts |

---

### B. Outgoing Email Pipeline (Port 587 Relay Pool - 12,000 to 22,000 Free Mails/Mo)

Instead of paying Google Workspace ₹1,500/user/month, outgoing transactional emails are dispatched over **Port 587** with full DKIM/SPF domain authorization:

* **Brevo Free Relay**: 9,000 emails / month (300 / day).
* **Resend Free Relay**: 3,000 emails / month (100 / day).
* **Total combined free allowance: ~12,000 branded mails/month** (the "22,000" figure in earlier revisions was never substantiated — dropped).
* **Security & Branding**: 100% pure `From: Avandab Billing <billing@avandab.com>` with `DKIM: PASS`.
* **Observed mail path (2026-09-10)**: outgoing mail flows through `comm_outbox` (`00118`) with the quota-aware provider pool (`00120`) — Brevo 300/day (9k/mo) + Resend 100/day (3k/mo), strategies `priority` / `cost_optimized` / `round_robin` (`EMAIL_PROVIDERS_JSON`). Over-quota providers fail over honestly; mail queues in the outbox, nothing silently drops.

---

## 💳 5. Digital Freight Invoicing, e-POD & Razorpay Settlement

1. **GST Tax Invoices & Bilty**:
   - Automated 3-Tier classification:
     - **Tier 1**: B2B Tax Invoice with 15-character GSTIN.
     - **Tier 2**: Section 31(3)(c) Bill of Supply for PAN-only operators (Sec 194C(6) TDS exempt).
     - **Tier 3**: Rule 54(3) Consignment Freight Bilty for micro transporters.
2. **Instant Online UPI Payment (`/pay/{invoiceId}`)**:
   - Razorpay 1-click checkout (Google Pay, PhonePe, Paytm, QR Code, NetBanking).
   - Zero setup fee, zero AMC.
   - Gateway per-transaction fees (MDR) still apply per Razorpay pricing — ₹0 refers to the platform integration, not payment processing.
   - Instant automated ledger settlement and receipt generation.
3. **Digital e-POD Certificate (`/epod/{tripId}`)**:
   - OTP-verified delivery proof.
   - Consignee touch signature image + geotagged cargo unloading photo.
   - Public printable PDF verification certificate.

---

## 🛠️ 6. Standard Operator Runbook (Daily Management)

### A. Checking Server Status
```bash
ssh avandab                          # VPS (systemd)
systemctl status avandab
curl -sf http://localhost:8080/health
# Android/legacy path: ssh <tecno-host> (Termux, port 8090)
```

### B. Updating Server Binary (correct flow)
```bash
# From local dev machine — compilation NEVER on the 1 GB server:
./scripts/deploy-vps.sh avandab
# → local go vet + stripped amd64 cross-compile + rsync + systemctl restart + /health check
```
Compiling `go build` on the server is forbidden — 1 GB RAM OOMs
(AGENTS.md prohibition #6; `docs/08` Option C).

### C. Testing Outbound Mail Delivery
```bash
echo -e "Subject: 🚚 Avandab Test Email\nFrom: Avandab Billing <billing@avandab.com>\nTo: bhshrivastav@gmail.com\n\nTest message from server 34.42.182.104" | /usr/sbin/sendmail -v bhshrivastav@gmail.com
```

---

## 🏆 Final Cost Summary (reconciled 2026-09-10)
* **Monthly baseline cost**: **₹0.00** — phone-or-VPS single node + Cloudflare free tier (DNS/CDN/SSL/email routing) + Brevo/Resend free relays + OSM tiles + routing via mock or public OSRM demo
* **Annual baseline**: **₹0.00**
* **Usage-billed extras NOT in the baseline (honest list)**: AI agent / RAG LLM tokens (defaults `api.openai.com`, `gpt-4o-mini` + `text-embedding-3-small` — point at a local/free OpenAI-compatible endpoint to keep ₹0); WhatsApp dispatch (Meta/Gupshup per-conversation fees); Razorpay MDR per transaction; S3/R2 storage beyond R2 10 GB free tier; mail beyond ~12k/mo
* **Platform uptime**: single-node — **no SLA claim** (5k-truck scale unproven pre PG-cutover, per `docs/ROADMAP.md` risks #1)
* **Domain Brand Reputation**: **100% Enterprise Branded (`@avandab.com`)**
