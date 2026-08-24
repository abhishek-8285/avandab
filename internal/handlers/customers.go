package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/domain"
	"transport-app/internal/logging"
	"transport-app/internal/middleware"
	"transport-app/internal/repository"
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
		user, err = h.Services.Users.CreateUserWithPassword(ctx, email, name, phone, password, roleID, domain.UserStatusActive)
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
		Extra: map[string]interface{}{"Customers": list, "Pagination": pd, "Query": pp.Query},
	})
}

func (h *CustomerHandlers) New(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	h.renderForm(w, r, "customer_edit.html", PageData{Title: "New Customer", User: session})
}

func (h *CustomerHandlers) Create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err := h.Services.Customers.CreateCustomer(
		r.Context(),
		r.PostFormValue("name"),
		r.PostFormValue("company"),
		r.PostFormValue("phone"),
		r.PostFormValue("email"),
		r.PostFormValue("gst"),
		r.PostFormValue("address"),
		r.PostFormValue("notes"),
	)
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id := domain.CustomerID(chi.URLParam(r, "id"))
	_, err := h.Services.Customers.UpdateCustomer(
		r.Context(), id,
		r.PostFormValue("name"),
		r.PostFormValue("company"),
		r.PostFormValue("phone"),
		r.PostFormValue("email"),
		r.PostFormValue("gst"),
		r.PostFormValue("address"),
		r.PostFormValue("notes"),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/customers/"+id.String(), http.StatusSeeOther)
}

func (h *CustomerHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id := domain.CustomerID(chi.URLParam(r, "id"))
	if err := h.Services.Customers.DeleteCustomer(r.Context(), id); err != nil {
		http.Error(w, "Failed to delete customer", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/customers", http.StatusSeeOther)
}

var _ = strconv.Itoa
