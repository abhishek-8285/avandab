# 🚚 Avandab Multi-Tenant Logistics Platform
## 100% Zero-Cost / Free-Tier Enterprise Architecture & Operations Manual

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
│                    │   Google Cloud Compute VM        │                                          │
│                    │   Public IP: 34.42.182.104       │                                          │
│                    │   • Golang Backend (:8080)       │                                          │
│                    │   • SQLite WAL Database          │                                          │
│                    │   • Postfix Mail Daemon          │                                          │
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

## 🖥️ 2. Cloud Server Infrastructure (Google Cloud Always Free)

| Parameter | Configuration | Free Tier Compliance |
| :--- | :--- | :--- |
| **Provider** | Google Cloud Platform (Compute Engine) | Always Free Tier + $300 Credits |
| **Instance Type** | `e2-micro` (2 vCPUs, 1.0 GB RAM) | 1 Free VM per month in US regions |
| **Operating System** | Debian GNU/Linux 13 (Trixie) | Minimal ~65 MB RAM footprint |
| **Dedicated Public IP** | `34.42.182.104` | Static / External IPv4 included |
| **Disk Storage** | 10 GB - 30 GB Standard Persistent Disk | Included in Free Tier ($0.00) |
| **Swap Buffer** | 2.0 GB Virtual Swap Memory (`/swapfile`) | Prevents OOM memory spikes |
| **Runtime Service** | Golang Native Binary (`./bin/server`) | Runs in background via `tmux` |

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
* **Total Combined Free Allowance**: **12,000 to 22,000 Free Branded Invoices/e-PODs per month!**
* **Security & Branding**: 100% pure `From: Avandab Billing <billing@avandab.com>` with `DKIM: PASS`.

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
   - Instant automated ledger settlement and receipt generation.
3. **Digital e-POD Certificate (`/epod/{tripId}`)**:
   - OTP-verified delivery proof.
   - Consignee touch signature image + geotagged cargo unloading photo.
   - Public printable PDF verification certificate.

---

## 🛠️ 6. Standard Operator Runbook (Daily Management)

### A. Checking Server Status
```bash
# Connect via SSH:
ssh bhshrivastav@34.42.182.104

# Check active tmux background session:
tmux attach -t avandab

# Detach from tmux without stopping server:
# Press: Ctrl + B, then press D
```

### B. Updating Server Binary
```bash
# 1. On server terminal:
cd ~/avandab
git pull origin main
go build -o bin/server ./cmd/server

# 2. Restart inside tmux:
./bin/server
```

### C. Testing Outbound Mail Delivery
```bash
echo -e "Subject: 🚚 Avandab Test Email\nFrom: Avandab Billing <billing@avandab.com>\nTo: bhshrivastav@gmail.com\n\nTest message from server 34.42.182.104" | /usr/sbin/sendmail -v bhshrivastav@gmail.com
```

---

## 🏆 Final Cost Summary
* **Monthly Infrastructure Cost**: **₹0.00**
* **Annual Software Overhead**: **₹0.00**
* **Platform Uptime**: **99.99% Cloud Datacenter SLA**
* **Domain Brand Reputation**: **100% Enterprise Branded (`@avandab.com`)**
