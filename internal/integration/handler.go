package integration

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/auth"
	"transport-app/internal/integration/accounting"
	"transport-app/internal/integration/ewaybill"
	"transport-app/internal/integration/fastag"
	"transport-app/internal/integration/gstn"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

// Handler exposes stub integration endpoints.
type Handler struct {
	ewaybill   ewaybill.Client
	gstn       gstn.Client
	fastag     fastag.Client
	accounting accounting.Client
	authSrv    auth.AuthorizationService
	db         *sql.DB
}

// NewHandler builds a Handler with clients created from cfg.
func NewHandler(cfg Config, authSrv auth.AuthorizationService, db ...*sql.DB) *Handler {
	var dbConn *sql.DB
	if len(db) > 0 {
		dbConn = db[0]
	}
	return &Handler{
		ewaybill:   ewaybill.NewClient(cfg.EWayBill),
		gstn:       gstn.NewClient(cfg.GSTN),
		fastag:     fastag.NewClient(cfg.FASTag, dbConn),
		accounting: accounting.NewClient(cfg.Accounting),
		authSrv:    authSrv,
		db:         dbConn,
	}
}

// Register mounts integration routes under /api/v1/integrations.
func (h *Handler) Register(r chi.Router) {
	r.Route("/api/v1/integrations", func(r chi.Router) {
		r.Route("/ewaybill", func(r chi.Router) {
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "ewaybill")).Post("/generate", h.GenerateEWayBill)
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "ewaybill")).Post("/part-a", h.GeneratePartA)
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "ewaybill")).Post("/part-b", h.AttachPartB)
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "ewaybill")).Post("/extend", h.ExtendEWayBill)
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "ewaybill")).Get("/get/{ewbNumber}", h.GetEWayBill)
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "ewaybill")).Get("/trip/{tripId}", h.GetEWayBillByTrip)
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "ewaybill")).Post("/cancel", h.CancelEWayBill)
		})
		r.Route("/gstn", func(r chi.Router) {
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "gstn")).Get("/validate/{gstin}", h.ValidateGSTIN)
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "gstn")).Get("/gstr1-summary", h.GSTR1Summary)
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "gstn")).Get("/gstr3b-summary", h.GSTR3BSummary)
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "gstn")).Post("/einvoice/irn", h.GenerateIRN)
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "gstn")).Post("/einvoice/push", h.PushEInvoice)
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "gstn")).Post("/einvoice/{invoiceID}/cancel", h.CancelIRN)
		})
		r.Route("/fastag", func(r chi.Router) {
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "fastag")).Get("/balance", h.GetFASTagBalance)
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "fastag")).Post("/deduct", h.DeductToll)
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "fastag")).Get("/transactions", h.ListFASTagTransactions)
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "fastag")).Post("/reconcile", h.ReconcileFASTag)
		})
		r.Route("/accounting", func(r chi.Router) {
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "accounting")).Post("/export-invoice", h.ExportInvoice)
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "accounting")).Post("/sync-contacts", h.SyncContacts)
			r.With(middleware.RequirePermission(h.authSrv, "integrations", "accounting")).Post("/push-journal-entry", h.PushJournalEntry)
		})
	})
}

func (h *Handler) GenerateEWayBill(w http.ResponseWriter, r *http.Request) {
	var req ewaybill.GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	res, err := h.ewaybill.Generate(r.Context(), req)
	if err != nil {
		http.Error(w, "E-Way Bill service unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(res)
}

func (h *Handler) GetEWayBill(w http.ResponseWriter, r *http.Request) {
	ewbNumber := chi.URLParam(r, "ewbNumber")
	res, err := h.ewaybill.Get(r.Context(), ewbNumber)
	if err != nil {
		http.Error(w, "E-Way Bill service unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *Handler) CancelEWayBill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EwbNumber string `json:"ewb_number"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	res, err := h.ewaybill.Cancel(r.Context(), req.EwbNumber, req.Reason)
	if err != nil {
		http.Error(w, "E-Way Bill service unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *Handler) ValidateGSTIN(w http.ResponseWriter, r *http.Request) {
	gstin := chi.URLParam(r, "gstin")
	res, err := h.gstn.ValidateGSTIN(r.Context(), gstin)
	if err != nil {
		http.Error(w, "GSTN service unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *Handler) GSTR1Summary(w http.ResponseWriter, r *http.Request) {
	gstin := r.URL.Query().Get("gstin")
	period := r.URL.Query().Get("period")
	res, err := h.gstn.FetchGSTR1Summary(r.Context(), gstin, period)
	if err != nil {
		http.Error(w, "GSTN service unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *Handler) GSTR3BSummary(w http.ResponseWriter, r *http.Request) {
	gstin := r.URL.Query().Get("gstin")
	period := r.URL.Query().Get("period")
	res, err := h.gstn.FetchGSTR3BSummary(r.Context(), gstin, period)
	if err != nil {
		http.Error(w, "GSTN service unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *Handler) GetFASTagBalance(w http.ResponseWriter, r *http.Request) {
	vehicle := r.URL.Query().Get("vehicle_number")
	tagID := r.URL.Query().Get("tag_id")
	res, err := h.fastag.GetBalance(r.Context(), vehicle, tagID)
	if err != nil {
		http.Error(w, "FASTag service unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *Handler) DeductToll(w http.ResponseWriter, r *http.Request) {
	var req fastag.DeductTollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	res, err := h.fastag.DeductToll(r.Context(), req)
	if err != nil {
		http.Error(w, "FASTag service unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *Handler) ListFASTagTransactions(w http.ResponseWriter, r *http.Request) {
	vehicle := r.URL.Query().Get("vehicle_number")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	res, err := h.fastag.ListTransactions(r.Context(), vehicle, limit)
	if err != nil {
		http.Error(w, "FASTag service unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"transactions": res})
}

func (h *Handler) ExportInvoice(w http.ResponseWriter, r *http.Request) {
	var req accounting.ExportedInvoice
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	res, err := h.accounting.ExportInvoice(r.Context(), req)
	if err != nil {
		http.Error(w, "Accounting service unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *Handler) SyncContacts(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Contacts []accounting.Contact `json:"contacts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	res, err := h.accounting.SyncContacts(r.Context(), req.Contacts)
	if err != nil {
		http.Error(w, "Accounting service unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *Handler) PushJournalEntry(w http.ResponseWriter, r *http.Request) {
	var req accounting.JournalEntry
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	res, err := h.accounting.PushJournalEntry(r.Context(), req)
	if err != nil {
		http.Error(w, "Accounting service unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *Handler) GenerateIRN(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InvoiceID string `json:"invoice_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InvoiceID == "" {
		http.Error(w, "Invalid request body or missing invoice_id", http.StatusBadRequest)
		return
	}

	tenantID := shared.TenantIDFromContext(r.Context())

	var invView gstn.InvoiceView
	invView.InvoiceID = req.InvoiceID

	if h.db != nil {
		var invNum, invDate, custGST, compGST, existingIRN sql.NullString
		var total, cgst, sgst, igst float64

		err := h.db.QueryRowContext(r.Context(), `
			SELECT i.invoice_number, DATE(i.created_at), i.total, i.cgst, i.sgst, i.igst, i.irn,
			       c.gst, COALESCE(
				       (SELECT gst_number FROM tenant_company_profiles WHERE tenant_id = i.tenant_id),
				       CASE WHEN i.tenant_id IS NULL OR i.tenant_id IN ('', '1')
					       THEN (SELECT gst_number FROM company_settings WHERE id = 1) END)
			FROM invoices i
			LEFT JOIN customers c ON i.customer_id = c.id
			WHERE i.id = ? AND i.tenant_id = ?
		`, req.InvoiceID, string(tenantID)).Scan(
			&invNum, &invDate, &total, &cgst, &sgst, &igst, &existingIRN,
			&custGST, &compGST,
		)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "Invoice not found", http.StatusNotFound)
				return
			}
			http.Error(w, "Database query error", http.StatusInternalServerError)
			return
		}

		if existingIRN.Valid && existingIRN.String != "" {
			http.Error(w, "IRN already generated for this invoice", http.StatusConflict)
			return
		}

		invView.InvoiceNumber = invNum.String
		invView.InvoiceDate = invDate.String
		invView.TotalValue = total
		invView.CGST = cgst
		invView.SGST = sgst
		invView.IGST = igst
		invView.RecipientGSTIN = custGST.String
		invView.SupplierGSTIN = compGST.String

		rows, err := h.db.QueryContext(r.Context(), `
			SELECT hsn_sac_code, description, unit, quantity, rate, taxable_value,
			       cgst_rate, sgst_rate, igst_rate, cgst_amount, sgst_amount, igst_amount, total
			FROM invoice_line_items
			WHERE invoice_id = ?
		`, req.InvoiceID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var item gstn.LineItemView
				var hsn, unit sql.NullString
				if err := rows.Scan(&hsn, &item.Description, &unit, &item.Quantity, &item.Rate, &item.TaxableValue,
					&item.CGSTRate, &item.SGSTRate, &item.IGSTRate, &item.CGSTAmount, &item.SGSTAmount, &item.IGSTAmount, &item.Total); err == nil {
					item.HSNSACCode = hsn.String
					item.Unit = unit.String
					invView.LineItems = append(invView.LineItems, item)
				}
			}
		}
	} else {
		invView.InvoiceNumber = "INV-" + req.InvoiceID
		invView.InvoiceDate = time.Now().Format("2006-01-02")
		invView.SupplierGSTIN = "27AABCU9603R1ZX"
		invView.RecipientGSTIN = "07AAACP0000M1Z9"
		invView.TotalValue = 1000.0
	}

	res, err := h.gstn.GenerateIRN(r.Context(), invView)
	if err != nil {
		http.Error(w, "GSTN service unavailable", http.StatusBadGateway)
		return
	}

	if h.db != nil {
		_, _ = h.db.ExecContext(r.Context(), `
			UPDATE invoices
			SET irn = ?, irn_ack_no = ?, irn_ack_date = ?, signed_qr = ?, updated_at = datetime('now')
			WHERE id = ? AND tenant_id = ?
		`, res.IRN, res.AckNo, res.AckDate, res.SignedQR, req.InvoiceID, string(tenantID))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(res)
}

func (h *Handler) PushEInvoice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InvoiceID string `json:"invoice_id"`
		IRN       string `json:"irn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InvoiceID == "" {
		http.Error(w, "Invalid request body or missing invoice_id", http.StatusBadRequest)
		return
	}

	tenantID := shared.TenantIDFromContext(r.Context())

	irn := req.IRN
	if h.db != nil {
		var dbIRN sql.NullString
		err := h.db.QueryRowContext(r.Context(), `
			SELECT irn FROM invoices WHERE id = ? AND tenant_id = ?
		`, req.InvoiceID, string(tenantID)).Scan(&dbIRN)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "Invoice not found", http.StatusNotFound)
				return
			}
			http.Error(w, "Database query error", http.StatusInternalServerError)
			return
		}
		if irn == "" {
			irn = dbIRN.String
		}
	}

	if irn == "" {
		http.Error(w, "IRN missing; generate IRN before pushing", http.StatusPreconditionFailed)
		return
	}

	res, err := h.gstn.PushEInvoice(r.Context(), req.InvoiceID, irn)
	if err != nil {
		http.Error(w, "GSTN service unavailable", http.StatusBadGateway)
		return
	}

	if h.db != nil {
		_, _ = h.db.ExecContext(r.Context(), `
			UPDATE invoices
			SET irn_ack_no = ?, irn_ack_date = ?, signed_qr = ?, updated_at = datetime('now')
			WHERE id = ? AND tenant_id = ?
		`, res.AckNo, res.AckDate, res.SignedQR, req.InvoiceID, string(tenantID))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res)
}

// irnCancelWindow is the GST rule: an IRN can only be cancelled within 24
// hours of its acknowledgement date.
const irnCancelWindow = 24 * time.Hour

// parseGSTTimestamp parses the timestamp formats stored in invoices columns:
// SQLite datetime('now') strings, the Go driver's RFC3339-with-offset form,
// and bare dates. Parsed values are treated as UTC.
func parseGSTTimestamp(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.999999999-07:00",
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func (h *Handler) CancelIRN(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CancelReason int    `json:"cancel_reason"`
		CancelRemark string `json:"cancel_remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.CancelReason < 1 || req.CancelReason > 4 {
		http.Error(w, "cancel_reason must be 1=Duplicate, 2=Order cancelled, 3=Data entry error, 4=Other", http.StatusBadRequest)
		return
	}

	invoiceID := chi.URLParam(r, "invoiceID")
	tenantID := shared.TenantIDFromContext(r.Context())

	var irn sql.NullString
	var ackDate, cancelledAt, createdAt sql.NullString
	err := h.db.QueryRowContext(r.Context(), `
		SELECT irn, COALESCE(irn_ack_date, ''), COALESCE(irn_cancelled_at, ''), COALESCE(created_at, '')
		FROM invoices WHERE id = ? AND tenant_id = ?
	`, invoiceID, string(tenantID)).Scan(&irn, &ackDate, &cancelledAt, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Invoice not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Database query error", http.StatusInternalServerError)
		return
	}

	if !irn.Valid || irn.String == "" {
		http.Error(w, "No IRN generated for this invoice; nothing to cancel", http.StatusBadRequest)
		return
	}
	if cancelledAt.Valid && cancelledAt.String != "" {
		http.Error(w, "IRN already cancelled for this invoice", http.StatusConflict)
		return
	}

	refTime, ok := parseGSTTimestamp(ackDate.String)
	windowSource := "ack_date"
	if !ok {
		refTime, ok = parseGSTTimestamp(createdAt.String)
		windowSource = "created_at"
		slog.Warn("[gstn] IRN ack date missing/unparseable; using invoice created_at for 24h cancel window",
			"invoice_id", invoiceID, "ack_date", ackDate.String, "created_at", createdAt.String)
	}
	if !ok {
		http.Error(w, "Cannot verify 24h cancellation window: IRN ack date and created_at are both missing or unparseable", http.StatusConflict)
		return
	}
	if time.Since(refTime) > irnCancelWindow {
		http.Error(w, "IRN cancellation window (24h from generation) has expired", http.StatusConflict)
		return
	}

	res, err := h.gstn.CancelIRN(r.Context(), gstn.CancelIRNRequest{
		IRN:          irn.String,
		CancelReason: req.CancelReason,
		CancelRemark: req.CancelRemark,
	})
	if err != nil {
		slog.Warn("[gstn] CancelIRN failed", "invoice_id", invoiceID, "window_source", windowSource, "error", err)
		http.Error(w, "GSTN service unavailable", http.StatusServiceUnavailable)
		return
	}

	if _, err := h.db.ExecContext(r.Context(), `
		UPDATE invoices
		SET irn_cancelled_at = datetime('now'), updated_at = datetime('now')
		WHERE id = ? AND tenant_id = ?
	`, invoiceID, string(tenantID)); err != nil {
		http.Error(w, "Failed to record IRN cancellation", http.StatusInternalServerError)
		return
	}

	if sess, _ := r.Context().Value(auth.ContextUser).(*auth.SessionData); sess != nil {
		_, _ = h.db.ExecContext(r.Context(), `
			INSERT INTO audit_logs (id, user_id, action, table_name, record_id, new_values, created_at)
			VALUES (?, ?, 'irn_cancelled', 'invoices', ?, ?, CURRENT_TIMESTAMP)
		`, uuid.NewString(), sess.UserID, invoiceID,
			fmt.Sprintf(`{"irn":%q,"cancel_reason":%d,"cancel_remark":%q,"window_source":%q}`,
				irn.String, req.CancelReason, req.CancelRemark, windowSource))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res)
}

func (h *Handler) GeneratePartA(w http.ResponseWriter, r *http.Request) {
	var req ewaybill.GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	res, err := h.ewaybill.GeneratePartA(r.Context(), req)
	if err != nil {
		http.Error(w, "E-Way Bill service unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(res)
}

func (h *Handler) AttachPartB(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EwbNumber     string `json:"ewb_number"`
		VehicleNumber string `json:"vehicle_number"`
		TransporterID string `json:"transporter_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	res, err := h.ewaybill.AttachPartB(r.Context(), req.EwbNumber, req.VehicleNumber, req.TransporterID)
	if err != nil {
		http.Error(w, "E-Way Bill service unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *Handler) ExtendEWayBill(w http.ResponseWriter, r *http.Request) {
	var req ewaybill.ExtendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	res, err := h.ewaybill.Extend(r.Context(), req.EwbNumber, req)
	if err != nil {
		http.Error(w, "E-Way Bill service unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *Handler) GetEWayBillByTrip(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "tripId")
	res, err := h.ewaybill.GetByTrip(r.Context(), tripID)
	if err != nil {
		http.Error(w, "E-Way Bill service unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *Handler) ReconcileFASTag(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VehicleNumber string `json:"vehicle_number"`
		From          string `json:"from"`
		To            string `json:"to"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	res, err := h.fastag.Reconcile(r.Context(), req.VehicleNumber, req.From, req.To)
	if err != nil {
		http.Error(w, "FASTag service unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
