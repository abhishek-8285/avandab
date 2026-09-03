package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/domain"
	"transport-app/internal/logging"
	"transport-app/internal/middleware"
	"transport-app/internal/repository"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// CustomerHandlers handles customer management.
type CustomerHandlers struct {
	*App
}

func (h *CustomerHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "customers", "read")).Get("/", h.List)
	r.With(middleware.ResourcePermission(h.AuthSrv, "customers", "create")).Get("/new", h.New)
	r.With(middleware.ResourcePermission(h.AuthSrv, "customers", "create")).Post("/new", h.Create)
	r.With(middleware.ResourcePermission(h.AuthSrv, "customers", "read")).Get("/{id}", h.View)
	r.With(middleware.ResourcePermission(h.AuthSrv, "customers", "update")).Get("/{id}/edit", h.Edit)
	r.With(middleware.ResourcePermission(h.AuthSrv, "customers", "update")).Post("/{id}/edit", h.Update)
	r.With(middleware.ResourcePermission(h.AuthSrv, "customers", "delete")).Post("/{id}/delete", h.Delete)
	r.With(middleware.ResourcePermission(h.AuthSrv, "customers", "update")).Post("/{id}/portal-users", h.GrantPortalAccess)

	// Consolidated Monthly Freight Invoicing & Customer Statement of Account (Khata)
	r.With(middleware.ResourcePermission(h.AuthSrv, "customers", "read")).Get("/{id}/unbilled-trips", h.UnbilledTrips)
	r.With(middleware.ResourcePermission(h.AuthSrv, "invoices", "create")).Post("/{id}/invoices/consolidate", h.ConsolidateInvoices)
	r.With(middleware.ResourcePermission(h.AuthSrv, "customers", "read")).Get("/{id}/statement", h.Statement)
}

// GrantPortalAccess links (or creates) a user account to this customer so
// they can sign in to the shipper portal. Migration 00073 defines the
// customer_users link table and the 'customer' role, but until now nothing
// wrote either outside that migration — the portal was unreachable without
// manual SQL.
func (h *CustomerHandlers) GrantPortalAccess(w http.ResponseWriter, r *http.Request) {
	customerID := chi.URLParam(r, "id")
	if customerID == "" {
		http.Error(w, "missing customer id", http.StatusBadRequest)
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	name := strings.TrimSpace(r.PostFormValue("name"))
	phone := strings.TrimSpace(r.PostFormValue("phone"))
	password := r.PostFormValue("password")
	if email == "" || password == "" || phone == "" {
		http.Error(w, "email, phone and password are required", http.StatusBadRequest)
		return
	}
	if name == "" {
		name = email
	}

	ctx := r.Context()
	user, err := h.Services.Users.GetUserByEmail(ctx, email)
	if err != nil {
		// Repo returns sql.ErrNoRows for unknown emails; provision a new
		// portal user with the seeded 'customer' role.
		roleID := h.customerRoleID(ctx)
		if roleID == 0 {
			http.Error(w, "customer role not seeded; run migrations", http.StatusInternalServerError)
			return
		}
		tenantID := string(shared.TenantIDFromContext(ctx))
		if tenantID == "" {
			tenantID = string(shared.DefaultTenant)
		}
		user, err = h.Services.Users.CreateUserWithPassword(ctx, email, name, phone, password, roleID, domain.UserStatusActive, tenantID)
		if err != nil {
			http.Error(w, "failed to create portal user: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	_ = h.AuthSrv.AddRoleForUser(user.ID.String(), string(domain.RoleCustomer))

	if h.DB != nil {
		_, _ = h.DB.ExecContext(ctx,
			`INSERT OR IGNORE INTO customer_users (id, customer_id, user_id) VALUES (?, ?, ?)`,
			uuid.NewString(), customerID, user.ID.String())
	}

	http.Redirect(w, r, "/customers/"+customerID, http.StatusSeeOther)
}

func (h *CustomerHandlers) customerRoleID(ctx context.Context) int64 {
	roles, err := h.Services.Users.ListRoles(ctx)
	if err != nil {
		return 0
	}
	for _, role := range roles {
		if role.Name == domain.RoleCustomer {
			return role.ID
		}
	}
	return 0
}

func (h *CustomerHandlers) List(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	pp := parsePaginationParams(r)

	list, total, err := h.Services.Customers.ListCustomers(r.Context(), pp.Query, pp.Limit, pp.Offset)
	if err != nil {
		http.Error(w, "Failed to list customers", http.StatusInternalServerError)
		return
	}

	pd := newPaginationData(pp, total, "/customers")

	if isDatastarRequest(r) {
		h.renderFragment(w, "customer_list_table.html", map[string]interface{}{
			"Customers":    list,
			"Pagination":   pd,
			"Query":        pp.Query,
			"StatusFilter": pp.Status,
		})
		return
	}

	h.renderPage(w, r, "customer_list.html", PageData{
		Title: "Customers",
		User:  session,
		Extra: map[string]interface{}{"Customers": list, "Pagination": pd, "Query": pp.Query, "StatusFilter": pp.Status},
	})
}

func (h *CustomerHandlers) New(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	h.renderForm(w, r, "customer_edit.html", PageData{Title: "New Customer", User: session})
}

func (h *CustomerHandlers) Create(w http.ResponseWriter, r *http.Request) {
	// Support both multipart (file upload) and urlencoded (tests / simple forms)
	if strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Handle logo file upload — like Settings Choose File
	photoURL := strings.TrimSpace(r.PostFormValue("photo_url"))
	if file, _, err := r.FormFile("logo"); err == nil {
		defer func() { _ = file.Close() }()
		uploadDir := ""
		if h.Config != nil {
			uploadDir = h.Config.UploadDir
		}
		if uploadDir == "" {
			uploadDir = "uploads"
		}
		if saved, err := saveCustomerLogo(file, uploadDir); err == nil {
			photoURL = "/uploads/" + saved
		} else {
			session, _ := h.getUserFromContext(r)
			h.renderForm(w, r, "customer_edit.html", PageData{Title: "New Customer", User: session, FlashError: "Logo upload failed: " + err.Error()})
			return
		}
	}

	// Fleetbase parity fields
	pts, _ := strconv.Atoi(r.PostFormValue("payment_terms_days"))
	if pts < 0 {
		pts = 0
	}
	_, err := h.Services.Customers.CreateCustomerFull(r.Context(), serviceCreateReqFromFormWithPhoto(r, pts, photoURL))
	if err != nil {
		session, _ := h.getUserFromContext(r)
		h.renderForm(w, r, "customer_edit.html", PageData{Title: "New Customer", User: session, FlashError: err.Error()})
		return
	}

	if isDatastarRequest(r) {
		w.Header().Set("Location", "/customers")
		w.WriteHeader(http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/customers", http.StatusSeeOther)
}

func serviceCreateReqFromFormWithPhoto(r *http.Request, pts int, photoURL string) service.CreateCustomerRequest {
	return service.CreateCustomerRequest{
		CustomerCode:     strings.TrimSpace(r.PostFormValue("customer_code")),
		Name:             strings.TrimSpace(r.PostFormValue("name")),
		Title:            strings.TrimSpace(r.PostFormValue("title")),
		Company:          strings.TrimSpace(r.PostFormValue("company")),
		ContactPerson:    strings.TrimSpace(r.PostFormValue("contact_person")),
		Phone:            strings.TrimSpace(r.PostFormValue("phone")),
		Email:            strings.TrimSpace(r.PostFormValue("email")),
		GST:              strings.TrimSpace(r.PostFormValue("gst")),
		Address:          strings.TrimSpace(r.PostFormValue("address")),
		BillingAddress:   strings.TrimSpace(r.PostFormValue("billing_address")),
		InternalID:       strings.TrimSpace(r.PostFormValue("internal_id")),
		PhotoURL:         photoURL,
		PlaceUUID:        strings.TrimSpace(r.PostFormValue("place_uuid")),
		Meta:             strings.TrimSpace(r.PostFormValue("meta")),
		Type:             strings.TrimSpace(r.PostFormValue("type")),
		Status:           strings.TrimSpace(r.PostFormValue("status")),
		PaymentTermsDays: pts,
		Notes:            strings.TrimSpace(r.PostFormValue("notes")),
	}
}

func (h *CustomerHandlers) View(w http.ResponseWriter, r *http.Request) {
	id := domain.CustomerID(chi.URLParam(r, "id"))
	customer, err := h.Services.Customers.GetCustomer(r.Context(), id)
	if err != nil {
		http.Error(w, "Customer not found", http.StatusNotFound)
		return
	}

	var (
		recentBookings []repository.BookingWithJoins
		recentInvoices []repository.InvoiceWithJoins
	)
	if bookings, err := h.Services.Bookings.ListBookingsByCustomer(r.Context(), id, 5); err == nil {
		recentBookings = bookings
	} else {
		slog.ErrorContext(r.Context(), "customer view: booking lookup failed",
			slog.String("customer_id", string(id)),
			slog.String("error", logging.Redact(err.Error())))
	}
	if invoices, err := h.Services.Invoices.ListInvoicesByCustomer(r.Context(), id, 5); err == nil {
		recentInvoices = invoices
	} else {
		slog.ErrorContext(r.Context(), "customer view: invoice lookup failed",
			slog.String("customer_id", string(id)),
			slog.String("error", logging.Redact(err.Error())))
	}

	session, _ := h.getUserFromContext(r)
	h.renderPage(w, r, "customer_view.html", PageData{Title: "View Customer", User: session, Extra: map[string]interface{}{
		"Customer":       customer,
		"RecentBookings": recentBookings,
		"RecentInvoices": recentInvoices,
	}})
}

func (h *CustomerHandlers) Edit(w http.ResponseWriter, r *http.Request) {
	id := domain.CustomerID(chi.URLParam(r, "id"))
	customer, err := h.Services.Customers.GetCustomer(r.Context(), id)
	if err != nil {
		http.Error(w, "Customer not found", http.StatusNotFound)
		return
	}
	session, _ := h.getUserFromContext(r)
	h.renderForm(w, r, "customer_edit.html", PageData{Title: "Edit Customer", User: session, Extra: map[string]interface{}{"Customer": customer}})
}

func (h *CustomerHandlers) Update(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	id := domain.CustomerID(chi.URLParam(r, "id"))
	// Handle logo file upload on edit — preserve existing if none
	existing, _ := h.Services.Customers.GetCustomer(r.Context(), id)
	photoURL := strings.TrimSpace(r.PostFormValue("photo_url"))
	if existing.PhotoURL != nil && photoURL == "" {
		// Keep existing if form didn't clear — will be handled via presence; don't override yet
	}
	if file, _, err := r.FormFile("logo"); err == nil {
		defer func() { _ = file.Close() }()
		uploadDir := ""
		if h.Config != nil {
			uploadDir = h.Config.UploadDir
		}
		if uploadDir == "" {
			uploadDir = "uploads"
		}
		if saved, err := saveCustomerLogo(file, uploadDir); err == nil {
			photoURL = "/uploads/" + saved
		} else {
			session, _ := h.getUserFromContext(r)
			cust, _ := h.Services.Customers.GetCustomer(r.Context(), id)
			h.renderForm(w, r, "customer_edit.html", PageData{Title: "Edit Customer", User: session, FlashError: "Logo upload failed: " + err.Error(), Extra: map[string]interface{}{"Customer": cust}})
			return
		}
	} else if photoURL == "" && existing.PhotoURL != nil {
		// No new file and no URL — keep via not marking present; handled below
	}

	req := serviceUpdateReqFromFormWithPhoto(r, photoURL)
	// Ensure photo presence correctly set when file uploaded
	if _, _, ferr := r.FormFile("logo"); ferr == nil {
		req.SetPresent("photo_url")
	}
	_, err := h.Services.Customers.UpdateCustomerFull(r.Context(), id, req)
	if err != nil {
		session, _ := h.getUserFromContext(r)
		cust, getErr := h.Services.Customers.GetCustomer(r.Context(), id)
		if getErr != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		h.renderForm(w, r, "customer_edit.html", PageData{
			Title: "Edit Customer", User: session, FlashError: err.Error(),
			Extra: map[string]interface{}{"Customer": cust},
		})
		return
	}
	http.Redirect(w, r, "/customers/"+id.String(), http.StatusSeeOther)
}

func serviceUpdateReqFromFormWithPhoto(r *http.Request, photoURL string) service.UpdateCustomerRequest {
	req := service.UpdateCustomerRequest{
		Name:           strings.TrimSpace(r.PostFormValue("name")),
		Title:          strings.TrimSpace(r.PostFormValue("title")),
		Company:        strings.TrimSpace(r.PostFormValue("company")),
		ContactPerson:  strings.TrimSpace(r.PostFormValue("contact_person")),
		Phone:          strings.TrimSpace(r.PostFormValue("phone")),
		Email:          strings.TrimSpace(r.PostFormValue("email")),
		GST:            strings.TrimSpace(r.PostFormValue("gst")),
		Address:        strings.TrimSpace(r.PostFormValue("address")),
		BillingAddress: strings.TrimSpace(r.PostFormValue("billing_address")),
		InternalID:     strings.TrimSpace(r.PostFormValue("internal_id")),
		PhotoURL:       photoURL,
		PlaceUUID:      strings.TrimSpace(r.PostFormValue("place_uuid")),
		Meta:           strings.TrimSpace(r.PostFormValue("meta")),
		Type:           strings.TrimSpace(r.PostFormValue("type")),
		Status:         strings.TrimSpace(r.PostFormValue("status")),
		CustomerCode:   strings.TrimSpace(r.PostFormValue("customer_code")),
		Notes:          strings.TrimSpace(r.PostFormValue("notes")),
	}
	// presence tracking: form had the key → treat empty as explicit clear
	for _, k := range []string{"title", "company", "contact_person", "email", "address", "billing_address", "internal_id", "photo_url", "place_uuid", "meta", "notes", "customer_code", "type", "status"} {
		if _, ok := r.PostForm[k]; ok {
			req.SetPresent(k)
		}
	}
	if v := strings.TrimSpace(r.PostFormValue("payment_terms_days")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			req.PaymentTermsDays = &n
			req.SetPresent("payment_terms_days")
		}
	}
	return req
}

func (h *CustomerHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id := domain.CustomerID(chi.URLParam(r, "id"))
	if err := h.Services.Customers.DeleteCustomer(r.Context(), id); err != nil {
		http.Error(w, "Failed to delete customer", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/customers", http.StatusSeeOther)
}

// customerLogoMimeExt mirrors Settings logoMimeExt but reused for customers.
var customerLogoMimeExt = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

func saveCustomerLogo(file io.Reader, uploadDir string) (string, error) {
	subdir := filepath.Join(uploadDir, "customers")
	if err := os.MkdirAll(subdir, 0o750); err != nil {
		return "", err
	}
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && err != io.ErrUnexpectedEOF {
		return "", err
	}
	head = head[:n]
	contentType := http.DetectContentType(head)
	if contentType == "image/svg+xml" {
		return "", fmt.Errorf("SVG logo uploads are not allowed")
	}
	ext, ok := customerLogoMimeExt[contentType]
	if !ok {
		return "", fmt.Errorf("unsupported logo file type: %s", contentType)
	}
	filename := uuid.NewString() + ext
	dest := filepath.Join(subdir, filename)
	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, io.MultiReader(bytes.NewReader(head), file)); err != nil {
		return "", err
	}
	return filepath.Join("customers", filename), nil
}

var _ = strconv.Itoa
