-- +goose Up
-- Migration 00110: Customer Quotes & Booking Pipeline (Phase 7)

CREATE TABLE IF NOT EXISTS customer_quotes (
    id               TEXT PRIMARY KEY,
    tenant_id        TEXT NOT NULL,
    customer_id      TEXT NOT NULL,
    origin           TEXT NOT NULL,
    destination      TEXT NOT NULL,
    cargo_type       TEXT NOT NULL,
    vehicle_type     TEXT NOT NULL,
    weight_kg        REAL NOT NULL DEFAULT 0.0,
    distance_km      REAL NOT NULL DEFAULT 0.0,
    base_rate        REAL NOT NULL DEFAULT 0.0,
    per_km_rate      REAL NOT NULL DEFAULT 0.0,
    estimated_toll   REAL NOT NULL DEFAULT 0.0,
    subtotal         REAL NOT NULL DEFAULT 0.0,
    gst_rate         REAL NOT NULL DEFAULT 0.05,
    gst_amount       REAL NOT NULL DEFAULT 0.0,
    discount_amount  REAL NOT NULL DEFAULT 0.0,
    total_price      REAL NOT NULL DEFAULT 0.0,
    status           TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active', 'converted', 'expired', 'cancelled')),
    expires_at       DATETIME NOT NULL,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_customer_quotes_tenant_cust
ON customer_quotes(tenant_id, customer_id, status);

CREATE TABLE IF NOT EXISTS customer_booking_details (
    booking_id           TEXT PRIMARY KEY,
    tenant_id            TEXT NOT NULL,
    quote_id             TEXT,
    idempotency_key      TEXT,
    pickup_address       TEXT NOT NULL,
    pickup_lat           REAL,
    pickup_lng           REAL,
    pickup_contact_name  TEXT,
    pickup_contact_phone TEXT,
    delivery_address     TEXT NOT NULL,
    delivery_lat         REAL,
    delivery_lng         REAL,
    delivery_contact_name TEXT,
    delivery_contact_phone TEXT,
    scheduled_at         DATETIME,
    cargo_description    TEXT,
    special_instructions TEXT,
    payment_status       TEXT NOT NULL DEFAULT 'pending' CHECK(payment_status IN ('pending', 'paid', 'partial', 'refunded')),
    payment_method       TEXT,
    cancellation_reason  TEXT,
    cancelled_by         TEXT,
    cancelled_at         DATETIME,
    created_at           DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at           DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    FOREIGN KEY (booking_id) REFERENCES bookings(id) ON DELETE CASCADE,
    FOREIGN KEY (quote_id) REFERENCES customer_quotes(id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_cust_bk_details_idemp
ON customer_booking_details(tenant_id, idempotency_key)
WHERE idempotency_key IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_cust_bk_details_idemp;
DROP TABLE IF EXISTS customer_booking_details;
DROP INDEX IF EXISTS idx_customer_quotes_tenant_cust;
DROP TABLE IF EXISTS customer_quotes;
