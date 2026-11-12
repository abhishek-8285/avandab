# 05. Billing, GST Invoicing & Driver Settlements

> **Financial Accounting, Tax Compliance & Driver Payout Engine**
> Full Indian logistics compliance with automated GST tax breakdown, driver ledger balances, and Razorpay integration.

---

## 1. GST Compliant Invoicing (`internal/invoice/`)

### Status Lifecycle
```text
Pending → Partially Paid → Paid
   ↓
 Voided
```

### Tax Calculation Rules
- **Intra-State Shipments** (Origin State == Destination State):
  - Split tax equally into **CGST (9%)** + **SGST (9%)** = 18% Total GST.
- **Inter-State Shipments** (Origin State != Destination State):
  - Applied as single **IGST (18%)**.
- **Line Items**: Base Freight, Loading/Unloading Charges, Detention / Wait-time charges, Toll fees.
- **Idempotency**: Generating an invoice for the same booking/trip twice returns the existing invoice without duplicate numbers.
- **PDF Export**: Generates printable PDF invoices (`internal/pdf/invoice_pdf.go`) with embedded UPI Payment QR codes.

---

## 2. Payment Recording & Invariants (`internal/payment/`)

- **Supported Methods**: Cash (`cash`), UPI (`upi`), Bank Transfer (`bank_transfer`), Cheque (`cheque`).
- **Outstanding Balance Calculation**:
  $$\text{Outstanding} = \text{Invoice Total} - \text{Amount Paid}$$
- **Immutability**: Once recorded, a payment cannot be modified or deleted. Errors are corrected via credit/reversal entries.
- **Auto Status Sync**:
  - `Outstanding == 0` $\to$ `Paid`
  - `Outstanding > 0 && AmountPaid > 0` $\to$ `Partially Paid`
  - `AmountPaid == 0` $\to$ `Pending`

---

## 3. Driver Settlement & Kharcha Accounting (`internal/settlement/`)

Trips settle via an immutable double-entry ledger calculation:

$$\text{Net Driver Payout} = \text{Agreed Driver Rate} + \text{Approved Tolls/Kharcha} - \text{Trip Advance} - \text{Fuel Deductions} - \text{Penalties}$$

### Approval Workflow:
1. **Driver Submits Receipt**: Driver snaps photo of fuel bill or speaks amount in mobile app.
2. **Kharcha Audit Engine**: Compares submitted fuel liters against telemetry fuel sensor drop.
3. **Manager Approval Gate**: Accountant approves or rejects expense at `/kharcha`.
4. **Settlement Slip**: Final approved amount is automatically added to driver's balance ledger.

### Payout Rail (RazorpayX, fail-closed)

Wallet debits only become real money through the payout rail; without it, `InitiatePayout` fails closed and writes nothing.

- **Dispatch** (`internal/settlement/application/payout_rail.go`, `internal/settlement/payout/razorpayx`): after the payout instruction + ledger debit commit, the instruction is sent to RazorpayX (`POST /v1/payouts`, IMPS, paise, `reference_id` = payout id). Provider `processing/paid/failed/reversed` is persisted on `payout_instructions`; provider failure or reversal credits the wallet back **exactly once** (compensating `PAYOUT_REVERSAL` entry).
- **Penny-drop verification**: `POST /api/v1/drivers/me/payout-account/verify` (driver) or the admin settlement view starts `POST /v1/fund_accounts/validations`; the `fund_account.validation.*` webhook flips `driver_payout_accounts.verification_status` to `verified`/`rejected`.
- **Webhook reconciliation**: `POST /api/v1/webhooks/payouts/razorpay` is public but signature-verified (HMAC-SHA256, mandatory), idempotent by `X-Razorpay-Event-Id`, and derives tenancy from the stored payout — never from the callback. Amount/currency are checked against the local instruction before any status change.
- **Enablement**: `PAYOUT_RAIL_ENABLED=true` **and** `PAYOUT_PROVIDER=razorpayx` with account + keys; otherwise startup warns and payouts stay disabled. Bank account numbers are AES-256-GCM encrypted at rest and decrypted only for the provider call (`PAYOUT_ENCRYPTION_KEY`).
- **UI**: admin settlement view shows account status, recent payouts (amount/status/UTR) and a penny-drop button.
- **Stuck-payout safety**: a leadered sweep alerts (settlement.payout_stuck, rule_payout_stuck) when a payout stays in initiated/processing past `PAYOUT_STUCK_AFTER` (default 24h) so a lost provider webhook cannot park money silently.

---

## 4. Razorpay Payment Gateway Integration (`internal/payment/razorpay/`)

- **Online Invoice Collection**: Generates Razorpay Order IDs for freight customers.
- **Cryptographic Webhook Verification**: Validates HMAC-SHA256 signature on payment capture:
  ```go
  client.VerifyPaymentSignature(orderID, paymentID, signature)
  ```
- **Instant Status Sync**: On valid signature verification, flips invoice status from `UNPAID` $\to$ `PAID` within the same database transaction.
