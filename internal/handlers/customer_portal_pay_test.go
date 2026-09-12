package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"transport-app/internal/auth"
	"transport-app/internal/shared"
)

func seedPortalPayFixtures(t *testing.T, app *App) {
	t.Helper()
	_, _ = app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-pay', 'Pay Corp', 'pay')`)
	_, err := app.DB.Exec(`
INSERT INTO customers (id, tenant_id, name, phone, status) VALUES
('cust-pay-1', 'tenant-pay', 'Pay Customer One', '+919000000001', 'active'),
('cust-pay-2', 'tenant-pay', 'Pay Customer Two', '+919000000002', 'active');
INSERT INTO customer_users (customer_id, user_id) VALUES ('cust-pay-1', 'usr-pay-1');
INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
VALUES ('rt-pay', 'Mumbai', 'Pune', 150, 4, 5000, 'tenant-pay');
INSERT INTO bookings (id, booking_number, customer_id, route_id, pickup_date, vehicle_type, passengers, price, status, tenant_id) VALUES
('bk-pay-1', 'BK-PAY-1', 'cust-pay-1', 'rt-pay', datetime('now', '-3 days'), 'truck', 1, 12000, 'confirmed', 'tenant-pay'),
('bk-pay-2', 'BK-PAY-2', 'cust-pay-2', 'rt-pay', datetime('now', '-2 days'), 'truck', 1, 15000, 'confirmed', 'tenant-pay');
INSERT INTO invoices (id, tenant_id, invoice_number, booking_id, customer_id, subtotal, tax, total, payment_status, status)
VALUES
('inv-unpaid', 'tenant-pay', 'INV-PAY-001', 'bk-pay-1', 'cust-pay-1', 1000.0, 180.0, 1180.0, 'pending', 'issued'),
('inv-paid', 'tenant-pay', 'INV-PAY-002', 'bk-pay-1', 'cust-pay-1', 500.0, 90.0, 590.0, 'paid', 'issued'),
('inv-other', 'tenant-pay', 'INV-PAY-003', 'bk-pay-2', 'cust-pay-2', 700.0, 126.0, 826.0, 'pending', 'issued');
`)
	require.NoError(t, err)
}

func portalPayRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	req := httptest.NewRequest("GET", target, nil)
	req.Header.Set("Accept", "application/json")
	ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID("tenant-pay"))
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "usr-pay-1", Role: "customer"})
	return req.WithContext(ctx)
}

func TestPortalInvoices_ScopedToOwnCustomer(t *testing.T) {
	app, r := setupZMOTMReportsTestApp(t)
	seedPortalPayFixtures(t, app)
	_ = app
	_ = r

	portal := &CustomerPortalHandlers{App: app}
	req := portalPayRequest(t, "/customer/invoices")
	w := httptest.NewRecorder()
	portal.ListMyInvoices(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Invoices []customerInvoiceRow `json:"invoices"`
		Total    int64                `json:"total"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, int64(2), resp.Total)
	ids := []string{resp.Invoices[0].ID, resp.Invoices[1].ID}
	assert.Contains(t, ids, "inv-unpaid")
	assert.Contains(t, ids, "inv-paid")
	assert.NotContains(t, ids, "inv-other", "other customer's invoice must not leak")
}

func TestInvoiceListTable_PortalPayLinks(t *testing.T) {
	app, _ := setupZMOTMReportsTestApp(t)
	tmpl := app.Templates.Lookup("invoice_list_table.html")
	require.NotNil(t, tmpl)

	rows := []customerInvoiceRow{
		{ID: "inv-unpaid", InvoiceNumber: "INV-PAY-001", CustomerName: "One", Subtotal: 1000, Tax: 180, Total: 1180, PaymentStatus: "pending"},
		{ID: "inv-paid", InvoiceNumber: "INV-PAY-002", CustomerName: "One", Subtotal: 500, Tax: 90, Total: 590, PaymentStatus: "paid"},
	}

	// Portal context: view links go to /pay, unpaid rows get a Pay button.
	var portal strings.Builder
	require.NoError(t, tmpl.Execute(&portal, map[string]interface{}{"Invoices": rows, "PortalPay": true}))
	phtml := portal.String()
	require.Contains(t, phtml, `href="/pay/inv-unpaid"`, "portal view must not 403 on staff route")
	assert.Contains(t, phtml, "Pay now")
	assert.NotContains(t, phtml, `/invoices/inv-unpaid`)
	// Paid row: no pay button.
	paidIdx := strings.Index(phtml, "INV-PAY-002")
	require.Greater(t, paidIdx, 0)
	assert.NotContains(t, phtml[paidIdx:], "Pay now", "paid invoice must not offer pay")

	// Staff context (no flag): unchanged staff links, no pay buttons.
	var staff strings.Builder
	require.NoError(t, tmpl.Execute(&staff, map[string]interface{}{"Invoices": rows}))
	shtml := staff.String()
	assert.Contains(t, shtml, `href="/invoices/inv-unpaid"`)
	assert.NotContains(t, shtml, "Pay now")
	assert.NotContains(t, shtml, "/pay/inv-unpaid")
}
