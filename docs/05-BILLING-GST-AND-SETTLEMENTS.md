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

---

## 4. Razorpay Payment Gateway Integration (`internal/payment/razorpay/`)

- **Online Invoice Collection**: Generates Razorpay Order IDs for freight customers.
- **Cryptographic Webhook Verification**: Validates HMAC-SHA256 signature on payment capture:
  ```go
  client.VerifyPaymentSignature(orderID, paymentID, signature)
  ```
- **Instant Status Sync**: On valid signature verification, flips invoice status from `UNPAID` $\to$ `PAID` within the same database transaction.
