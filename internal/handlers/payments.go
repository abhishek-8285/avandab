package handlers

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/domain"
	"transport-app/internal/middleware"
	paymentapp "transport-app/internal/payment/application"
	paymentagg "transport-app/internal/payment/domain/aggregate"
	"transport-app/internal/payment/razorpay"
	"transport-app/internal/shared"
	clock "transport-app/internal/shared/clock"
	id "transport-app/internal/shared/id"
	uow "transport-app/internal/shared/uow"
)

// PaymentHandlers handles payment management.
type PaymentHandlers struct {
	*App
	recordUC *paymentapp.RecordPaymentUseCase
	listUC   *paymentapp.ListPaymentsUseCase
	getUC    *paymentapp.GetPaymentUseCase
	orderUC  *paymentapp.CreateRazorpayOrderUseCase
	verifyUC *paymentapp.VerifyRazorpayPaymentUseCase
}

func (h *PaymentHandlers) SetRazorpayUseCases(orderUC *paymentapp.CreateRazorpayOrderUseCase, verifyUC *paymentapp.VerifyRazorpayPaymentUseCase) {
	h.orderUC = orderUC
	h.verifyUC = verifyUC
}

func (h *PaymentHandlers) init() {
	if h.recordUC == nil {
		uowImpl := uow.NewSQLUnitOfWork(h.DB)
		clockImpl := clock.NewRealClock()
		idGenImpl := id.NewUUIDGenerator()

		h.recordUC = paymentapp.NewRecordPaymentUseCase(uowImpl, idGenImpl, clockImpl)
		h.listUC = paymentapp.NewListPaymentsUseCase(uowImpl)
		h.getUC = paymentapp.NewGetPaymentUseCase(uowImpl)
	}
	if h.orderUC == nil {
		uowImpl := uow.NewSQLUnitOfWork(h.DB)
		var keyID, keySecret string
		if h.Config != nil {
			keyID = h.Config.RazorpayKeyID
			keySecret = h.Config.RazorpayKeySecret
		}
		client := razorpay.NewRazorpayClient(keyID, keySecret)
		h.orderUC = paymentapp.NewCreateRazorpayOrderUseCase(uowImpl, client, keyID)
	}
	if h.verifyUC == nil {
		uowImpl := uow.NewSQLUnitOfWork(h.DB)
		clockImpl := clock.NewRealClock()
		var keyID, keySecret string
		if h.Config != nil {
			keyID = h.Config.RazorpayKeyID
			keySecret = h.Config.RazorpayKeySecret
		}
		client := razorpay.NewRazorpayClient(keyID, keySecret)
		h.verifyUC = paymentapp.NewVerifyRazorpayPaymentUseCase(uowImpl, h.recordUC, client, keySecret, clockImpl)
	}
}

func (h *PaymentHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "payments", "read")).Get("/", h.List)
	r.With(middleware.ResourcePermission(h.AuthSrv, "payments", "create")).Get("/new/{invoice_id}", h.New)
	r.With(middleware.ResourcePermission(h.AuthSrv, "payments", "create")).Post("/new/{invoice_id}", h.Create)
	r.With(middleware.ResourcePermission(h.AuthSrv, "payments", "read")).Get("/{id}", h.View)
	r.With(middleware.ResourcePermission(h.AuthSrv, "payments", "delete")).Post("/{id}/delete", h.Delete)
}

func (h *PaymentHandlers) List(w http.ResponseWriter, r *http.Request) {
	h.init()
	session, _ := h.getUserFromContext(r)
	pp := parsePaginationParams(r)

	method := r.URL.Query().Get("method")
	if method == "" {
		// filter_bar.html chips emit ?status=; map it to the payment method
		// filter so chip links keep filtering.
		method = pp.Status
	}
	res, err := h.listUC.Execute(r.Context(), paymentapp.ListPaymentsQuery{
		TenantID: shared.TenantIDFromContext(r.Context()),
		Page:     pp.Page,
		Limit:    pp.Limit,
		Method:   method,
		DateFrom: pp.DateFrom,
		DateTo:   pp.DateTo,
	})
	if err != nil {
		http.Error(w, "Failed to list payments", http.StatusInternalServerError)
		return
	}

	pd := newPaginationData(pp, res.Total, "/payments")
	pd.From = pp.DateFrom
	pd.To = pp.DateTo

	if isDatastarRequest(r) {
		h.renderFragment(w, "payment_list_table.html", map[string]interface{}{
			"Payments":     res.Payments,
			"Pagination":   pd,
			"Method":       method,
			"Query":        pp.Query,
			"StatusFilter": method,
			"DateFrom":     pp.DateFrom,
			"DateTo":       pp.DateTo,
		})
		return
	}

	h.renderPage(w, r, "payment_list.html", PageData{
		Title: "Payments",
		User:  session,
		Extra: map[string]interface{}{"Payments": res.Payments, "Pagination": pd, "Method": method, "Query": pp.Query, "StatusFilter": method, "DateFrom": pp.DateFrom, "DateTo": pp.DateTo, "KPIs": h.paymentKPIs(r.Context())},
	})
}

func (h *PaymentHandlers) New(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	invoiceID := chi.URLParam(r, "invoice_id")

	invoice, err := h.Services.Invoices.GetInvoice(r.Context(), domain.InvoiceID(invoiceID))
	if err != nil {
		http.Error(w, "Invoice not found", http.StatusNotFound)
		return
	}
	balance, errBal := h.Services.Invoices.GetBalance(r.Context(), domain.InvoiceID(invoiceID))
	if errBal != nil {
		slog.Error("Failed to calculate balance for payment form", "invoice_id", invoiceID, "error", errBal)
		balance = 0
	}

	h.renderForm(w, r, "payment_edit.html", PageData{
		Title:         "Record Payment",
		User:          session,
		RazorpayKeyID: h.Config.RazorpayKeyID,
		Extra: map[string]interface{}{
			"InvoiceID": invoiceID,
			"Invoice":   invoice,
			"Balance":   balance,
			"Now":       time.Now(),
		},
	})
}

func (h *PaymentHandlers) Create(w http.ResponseWriter, r *http.Request) {
	h.init()
	if err := r.ParseForm(); err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Invalid Form Submission")
		return
	}

	invoiceID := chi.URLParam(r, "invoice_id")
	amount, _ := strconv.ParseFloat(r.PostFormValue("amount"), 64)
	method := paymentagg.PaymentMethod(r.PostFormValue("method"))
	paymentDateStr := r.PostFormValue("payment_date")
	var paymentDate time.Time
	var err error
	if paymentDateStr != "" {
		paymentDate, err = time.Parse("2006-01-02", paymentDateStr)
	}
	if paymentDateStr == "" || err != nil {
		paymentDate = time.Now()
	}

	var reference *string
	if val := r.PostFormValue("reference"); val != "" {
		reference = &val
	}
	var remarks *string
	if val := r.PostFormValue("remarks"); val != "" {
		remarks = &val
	}

	_, err = h.recordUC.Execute(r.Context(), paymentapp.RecordPaymentCommand{
		TenantID:    shared.TenantIDFromContext(r.Context()),
		InvoiceID:   invoiceID,
		PaymentDate: paymentDate,
		Amount:      amount,
		Method:      method,
		Reference:   reference,
		Remarks:     remarks,
	})
	if err != nil {
		session, _ := h.getUserFromContext(r)
		invoice, _ := h.Services.Invoices.GetInvoice(r.Context(), domain.InvoiceID(invoiceID))
		balance, errBal := h.Services.Invoices.GetBalance(r.Context(), domain.InvoiceID(invoiceID))
		if errBal != nil {
			slog.Error("Failed to calculate balance for payment error form", "invoice_id", invoiceID, "error", errBal)
			balance = 0
		}

		h.renderForm(w, r, "payment_edit.html", PageData{
			Title:         "Record Payment",
			User:          session,
			FlashError:    err.Error(),
			RazorpayKeyID: h.Config.RazorpayKeyID,
			Extra: map[string]interface{}{
				"InvoiceID": invoiceID,
				"Invoice":   invoice,
				"Balance":   balance,
				"Now":       time.Now(),
			},
		})
		return
	}

	if isDatastarRequest(r) {
		w.Header().Set("Location", "/invoices/"+invoiceID)
		w.WriteHeader(http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/invoices/"+invoiceID, http.StatusSeeOther)
}

func (h *PaymentHandlers) View(w http.ResponseWriter, r *http.Request) {
	h.init()
	id := chi.URLParam(r, "id")
	payment, err := h.getUC.Execute(r.Context(), paymentapp.GetPaymentQuery{
		ID:       paymentagg.PaymentID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		http.Error(w, "Payment not found", http.StatusNotFound)
		return
	}
	session, _ := h.getUserFromContext(r)
	h.renderPage(w, r, "payment_view.html", PageData{Title: "View Payment", User: session, Extra: map[string]interface{}{"Payment": payment}})
}

func (h *PaymentHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id := domain.PaymentID(chi.URLParam(r, "id"))
	if err := h.Services.Payments.DeletePayment(r.Context(), id); err != nil {
		http.Error(w, "Failed to delete payment", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/payments", http.StatusSeeOther)
}
