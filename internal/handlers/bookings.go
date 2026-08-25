package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/apperr"
	"transport-app/internal/auth"
	bookingapp "transport-app/internal/booking/application"
	bookingagg "transport-app/internal/booking/domain/aggregate"
	"transport-app/internal/domain"
	"transport-app/internal/httpx"
	"transport-app/internal/logging"
	"transport-app/internal/middleware"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
	clock "transport-app/internal/shared/clock"
	id "transport-app/internal/shared/id"
	uow "transport-app/internal/shared/uow"
)

func bookingActionFailed(h *BookingHandlers, w http.ResponseWriter, r *http.Request, err error, title string) {
	msg := "Something went wrong while processing this booking. Please try again."
	if ae, ok := apperr.From(err); ok {
		msg = ae.UserMsg
	}
	slog.ErrorContext(r.Context(), "booking action failed",
		slog.String("path", r.URL.Path),
		slog.String("error", logging.Redact(err.Error())),
	)
	session, _ := h.getUserFromContext(r)
	h.renderError(w, http.StatusBadRequest, title, msg, session)
}

// BookingHandlers handles booking management.
type BookingHandlers struct {
	*App
	createUC   *bookingapp.CreateBookingUseCase
	confirmUC  *bookingapp.ConfirmBookingUseCase
	cancelUC   *bookingapp.CancelBookingUseCase
	getUC      *bookingapp.GetBookingUseCase
	listUC     *bookingapp.ListBookingsUseCase
	updateUC   *bookingapp.UpdateBookingUseCase
	deleteUC   *bookingapp.DeleteBookingUseCase
	completeUC *bookingapp.CompleteBookingUseCase
}

func (h *BookingHandlers) init() {
	if h.createUC == nil {
		uowImpl := uow.NewSQLUnitOfWork(h.DB)
		clockImpl := clock.NewRealClock()
		idGenImpl := id.NewUUIDGenerator()

		h.createUC = bookingapp.NewCreateBookingUseCase(uowImpl, idGenImpl, clockImpl)
		h.confirmUC = bookingapp.NewConfirmBookingUseCase(uowImpl, clockImpl)
		h.cancelUC = bookingapp.NewCancelBookingUseCase(uowImpl, clockImpl)
		h.getUC = bookingapp.NewGetBookingUseCase(uowImpl)
		h.listUC = bookingapp.NewListBookingsUseCase(uowImpl)
		h.updateUC = bookingapp.NewUpdateBookingUseCase(uowImpl)
		h.deleteUC = bookingapp.NewDeleteBookingUseCase(uowImpl)
		h.completeUC = bookingapp.NewCompleteBookingUseCase(uowImpl, clockImpl)
	}
}

func (h *BookingHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "bookings", "read")).Get("/", h.List)
	r.With(middleware.ResourcePermission(h.AuthSrv, "bookings", "create")).Get("/new", h.New)
	r.With(middleware.ResourcePermission(h.AuthSrv, "bookings", "create")).Post("/new", h.Create)
	r.With(middleware.ResourcePermission(h.AuthSrv, "bookings", "read")).Get("/{id}", h.View)
	r.With(middleware.ResourcePermission(h.AuthSrv, "bookings", "update")).Get("/{id}/edit", h.Edit)
	r.With(middleware.ResourcePermission(h.AuthSrv, "bookings", "update")).Post("/{id}/edit", h.Update)
	r.With(middleware.ResourcePermission(h.AuthSrv, "bookings", "delete")).Post("/{id}/delete", h.Delete)
	r.With(middleware.ResourcePermission(h.AuthSrv, "bookings", "approve")).Post("/{id}/confirm", h.Confirm)
	r.With(middleware.ResourcePermission(h.AuthSrv, "bookings", "cancel")).Post("/{id}/cancel", h.Cancel)
	r.With(middleware.ResourcePermission(h.AuthSrv, "bookings", "update")).Post("/{id}/complete", h.Complete)
}

func (h *BookingHandlers) List(w http.ResponseWriter, r *http.Request) {
	h.init()
	session, _ := h.getUserFromContext(r)
	pp := parsePaginationParams(r)

	tenantID := shared.TenantIDFromContext(r.Context())

	res, err := h.listUC.Execute(r.Context(), bookingapp.ListBookingsQuery{
		TenantID: tenantID,
		Page:     pp.Page,
		Limit:    pp.Limit,
		Search:   pp.Query,
		Status:   pp.Status,
		DateFrom: pp.DateFrom,
		DateTo:   pp.DateTo,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	pd := newPaginationData(pp, res.Total, "/bookings")
	pd.From = pp.DateFrom
	pd.To = pp.DateTo

	if isDatastarRequest(r) {
		h.renderFragment(w, "booking_list_table.html", map[string]interface{}{
			"Bookings":     res.Bookings,
			"Pagination":   pd,
			"Query":        pp.Query,
			"StatusFilter": pp.Status,
			"DateFrom":     pp.DateFrom,
			"DateTo":       pp.DateTo,
		})
		return
	}

	h.renderPage(w, r, "booking_list.html", PageData{
		Title: "Bookings",
		User:  session,
		Extra: map[string]interface{}{"Bookings": res.Bookings, "Pagination": pd, "Query": pp.Query, "StatusFilter": pp.Status, "DateFrom": pp.DateFrom, "DateTo": pp.DateTo, "KPIs": h.bookingKPIs(r.Context())},
	})
}

func (h *BookingHandlers) New(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	customers, _, _ := h.Services.Customers.ListCustomers(r.Context(), "", 1000, 0)
	routes, _, _ := h.Services.Routes.ListRoutes(r.Context(), "", 1000, 0)
	h.renderForm(w, r, "booking_edit.html", PageData{
		Title: "New Booking",
		User:  session,
		Extra: map[string]interface{}{"Customers": customers, "Routes": routes},
	})
}

func (h *BookingHandlers) Create(w http.ResponseWriter, r *http.Request) {
	h.init()
	if err := r.ParseForm(); err != nil {
		httpx.Error(w, r, apperr.New(apperr.CodeValidation).WithCause(err))
		return
	}

	customerID := r.PostFormValue("customer_id")
	routeID := r.PostFormValue("route_id")
	passengers, _ := strconv.ParseInt(r.PostFormValue("passengers"), 10, 64)
	cargoWeight, _ := strconv.ParseFloat(r.PostFormValue("cargo_weight"), 64)
	price, _ := strconv.ParseFloat(r.PostFormValue("price"), 64)
	tenantID := shared.TenantIDFromContext(r.Context())

	_, err := h.createUC.Execute(r.Context(), bookingapp.CreateBookingCommand{
		TenantID:    tenantID,
		CustomerID:  customerID,
		RouteID:     routeID,
		PickupDate:  r.PostFormValue("pickup_date"),
		VehicleType: r.PostFormValue("vehicle_type"),
		Passengers:  passengers,
		CargoWeight: &cargoWeight,
		Price:       price,
		Notes:       r.PostFormValue("notes"),
	})
	if err != nil {
		session, _ := h.getUserFromContext(r)
		flash := "We couldn't save this booking. Please review the details and try again."
		if ae, ok := apperr.From(err); ok {
			flash = ae.UserMsg
		}
		slog.ErrorContext(r.Context(), "booking create failed",
			slog.String("error", logging.Redact(err.Error())))
		h.renderForm(w, r, "booking_edit.html", PageData{Title: "New Booking", User: session, FlashError: flash})
		return
	}

	http.Redirect(w, r, "/bookings", http.StatusSeeOther)
}

func (h *BookingHandlers) View(w http.ResponseWriter, r *http.Request) {
	h.init()
	session, _ := h.getUserFromContext(r)
	id := chi.URLParam(r, "id")
	tenantID := shared.TenantIDFromContext(r.Context())

	booking, err := h.getUC.Execute(r.Context(), bookingapp.GetBookingQuery{
		BookingID: bookingagg.BookingID(id),
		TenantID:  tenantID,
	})
	if err != nil {
		h.renderError(w, http.StatusNotFound, "Booking Not Found", fmt.Sprintf("No booking found with ID %q.", id), session)
		return
	}

	var (
		relatedTrip    *domain.Trip
		relatedInvoice *repository.InvoiceWithJoins
		history        []repository.AuditLogWithUser
	)
	if t, err := h.Services.Trips.GetTripByBooking(r.Context(), domain.BookingID(id)); err == nil {
		relatedTrip = t
	} else if !errors.Is(err, sql.ErrNoRows) {
		slog.ErrorContext(r.Context(), "booking view: trip lookup failed",
			slog.String("booking_id", id),
			slog.String("error", logging.Redact(err.Error())))
	}
	if inv, err := h.Services.Invoices.GetInvoiceByBooking(r.Context(), domain.BookingID(id)); err == nil {
		relatedInvoice = &inv
	} else if !errors.Is(err, sql.ErrNoRows) {
		slog.ErrorContext(r.Context(), "booking view: invoice lookup failed",
			slog.String("booking_id", id),
			slog.String("error", logging.Redact(err.Error())))
	}

	if entries, err := h.Services.Audit.ListAuditLogsByRecord(r.Context(), "bookings", id, 10); err == nil {
		history = entries
	} else {
		slog.ErrorContext(r.Context(), "booking view: history lookup failed",
			slog.String("booking_id", id),
			slog.String("error", logging.Redact(err.Error())))
	}

	h.renderPage(w, r, "booking_view.html", PageData{
		Title: "View Booking",
		User:  session,
		Extra: map[string]interface{}{"Booking": booking, "RelatedTrip": relatedTrip, "RelatedInvoice": relatedInvoice, "History": history},
	})
}

func (h *BookingHandlers) Edit(w http.ResponseWriter, r *http.Request) {
	h.init()
	id := chi.URLParam(r, "id")
	session, _ := h.getUserFromContext(r)
	tenantID := shared.TenantIDFromContext(r.Context())

	booking, err := h.getUC.Execute(r.Context(), bookingapp.GetBookingQuery{
		BookingID: bookingagg.BookingID(id),
		TenantID:  tenantID,
	})
	if err != nil {
		h.renderError(w, http.StatusNotFound, "Booking Not Found", fmt.Sprintf("No booking found with ID %q.", id), session)
		return
	}
	customers, _, _ := h.Services.Customers.ListCustomers(r.Context(), "", 1000, 0)
	routes, _, _ := h.Services.Routes.ListRoutes(r.Context(), "", 1000, 0)
	h.renderForm(w, r, "booking_edit.html", PageData{
		Title: "Edit Booking",
		User:  session,
		Extra: map[string]interface{}{"Booking": booking, "Customers": customers, "Routes": routes},
	})
}

func (h *BookingHandlers) Update(w http.ResponseWriter, r *http.Request) {
	h.init()
	if err := r.ParseForm(); err != nil {
		httpx.Error(w, r, apperr.New(apperr.CodeValidation).WithCause(err))
		return
	}

	id := chi.URLParam(r, "id")
	customerID := r.PostFormValue("customer_id")
	routeID := r.PostFormValue("route_id")
	passengers, _ := strconv.ParseInt(r.PostFormValue("passengers"), 10, 64)
	cargoWeight, _ := strconv.ParseFloat(r.PostFormValue("cargo_weight"), 64)
	price, _ := strconv.ParseFloat(r.PostFormValue("price"), 64)
	tenantID := shared.TenantIDFromContext(r.Context())

	err := h.updateUC.Execute(r.Context(), bookingapp.UpdateBookingCommand{
		BookingID:   bookingagg.BookingID(id),
		TenantID:    tenantID,
		CustomerID:  customerID,
		RouteID:     routeID,
		PickupDate:  r.PostFormValue("pickup_date"),
		VehicleType: r.PostFormValue("vehicle_type"),
		Passengers:  passengers,
		CargoWeight: &cargoWeight,
		Price:       price,
		Notes:       r.PostFormValue("notes"),
	})
	if err != nil {
		booking, _ := h.getUC.Execute(r.Context(), bookingapp.GetBookingQuery{
			BookingID: bookingagg.BookingID(id),
			TenantID:  tenantID,
		})
		customers, _, _ := h.Services.Customers.ListCustomers(r.Context(), "", 1000, 0)
		routes, _, _ := h.Services.Routes.ListRoutes(r.Context(), "", 1000, 0)
		session, _ := h.getUserFromContext(r)
		flash := "We couldn't save your changes. Please review the details and try again."
		if ae, ok := apperr.From(err); ok {
			flash = ae.UserMsg
		}
		slog.ErrorContext(r.Context(), "booking update failed",
			slog.String("booking_id", id),
			slog.String("error", logging.Redact(err.Error())))
		h.renderForm(w, r, "booking_edit.html", PageData{
			Title:      "Edit Booking",
			User:       session,
			FlashError: flash,
			Extra:      map[string]interface{}{"Booking": booking, "Customers": customers, "Routes": routes},
		})
		return
	}
	http.Redirect(w, r, "/bookings/"+id, http.StatusSeeOther)
}

func (h *BookingHandlers) Complete(w http.ResponseWriter, r *http.Request) {
	h.init()
	id := chi.URLParam(r, "id")
	tenantID := shared.TenantIDFromContext(r.Context())

	err := h.completeUC.Execute(r.Context(), bookingapp.CompleteBookingCommand{
		BookingID: bookingagg.BookingID(id),
		TenantID:  tenantID,
	})
	if err != nil {
		bookingActionFailed(h, w, r, err, "Could Not Complete Booking")
		return
	}
	http.Redirect(w, r, "/bookings/"+id, http.StatusSeeOther)
}

func (h *BookingHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	h.init()
	id := chi.URLParam(r, "id")
	tenantID := shared.TenantIDFromContext(r.Context())

	if err := h.deleteUC.Execute(r.Context(), bookingapp.DeleteBookingCommand{
		BookingID: bookingagg.BookingID(id),
		TenantID:  tenantID,
	}); err != nil {
		bookingActionFailed(h, w, r, err, "Could Not Delete Booking")
		return
	}
	http.Redirect(w, r, "/bookings", http.StatusSeeOther)
}

func (h *BookingHandlers) Confirm(w http.ResponseWriter, r *http.Request) {
	h.init()
	id := chi.URLParam(r, "id")
	tenantID := shared.TenantIDFromContext(r.Context())

	err := h.confirmUC.Execute(r.Context(), bookingapp.ConfirmBookingCommand{
		BookingID: bookingagg.BookingID(id),
		TenantID:  tenantID,
	})
	if err != nil {
		bookingActionFailed(h, w, r, err, "Could Not Confirm Booking")
		return
	}
	http.Redirect(w, r, "/bookings/"+id, http.StatusSeeOther)
}

func (h *BookingHandlers) Cancel(w http.ResponseWriter, r *http.Request) {
	h.init()
	id := chi.URLParam(r, "id")
	tenantID := shared.TenantIDFromContext(r.Context())

	err := h.cancelUC.Execute(r.Context(), bookingapp.CancelBookingCommand{
		BookingID: bookingagg.BookingID(id),
		TenantID:  tenantID,
	})
	if err != nil {
		bookingActionFailed(h, w, r, err, "Could Not Cancel Booking")
		return
	}
	http.Redirect(w, r, "/bookings/"+id, http.StatusSeeOther)
}

// ── S5: Bookings kanban board (Spec 22 §2.4, §4.2) ──────────────────────

// boardCard is one kanban card (Spec 22 §2.4 shape, real status machine).
type boardCard struct {
	ID       string  `json:"id"`
	Number   string  `json:"number,omitempty"`
	Customer string  `json:"customer,omitempty"`
	Vehicle  string  `json:"vehicle,omitempty"`
	Driver   string  `json:"driver,omitempty"`
	Freight  float64 `json:"freight"`
	Deadline string  `json:"deadline,omitempty"`
}

type boardColumn struct {
	Status string      `json:"status"`
	Cards  []boardCard `json:"cards"`
}

// boardColumns maps the real booking state machine onto board lanes.
// Spec 22's new/assigned/en_route/delivered/settled lanes correspond to
// the existing pending/confirmed/completed lifecycle (+ cancelled side
// lane); transitions always go through the EXISTING confirm/complete/
// cancel endpoints — no new mutation endpoint (Spec 22 §2.4).
var boardColumns = []string{"pending", "confirmed", "completed", "cancelled"}

func (h *BookingHandlers) Board(w http.ResponseWriter, r *http.Request) {
	h.init()
	tenantID := shared.TenantIDFromContext(r.Context())

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT b.id, b.booking_number, COALESCE(c.name,''), b.status,
		       COALESCE(b.price,0), b.pickup_date,
		       COALESCE(v.vehicle_number,''),
		       COALESCE(d.first_name || ' ' || d.last_name, '')
		FROM bookings b
		LEFT JOIN customers c ON c.id = b.customer_id
		LEFT JOIN trips t ON t.booking_id = b.id
		     AND t.status IN ('assigned','started','reached_pickup','in_transit')
		LEFT JOIN vehicles v ON v.id = t.vehicle_id
		LEFT JOIN drivers d ON d.id = t.driver_id
		WHERE b.tenant_id = ?
		ORDER BY b.created_at DESC LIMIT 200`, tenantID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	defer func() { _ = rows.Close() }()

	cardsByStatus := make(map[string][]boardCard, len(boardColumns))
	for _, s := range boardColumns {
		cardsByStatus[s] = []boardCard{}
	}
	for rows.Next() {
		var (
			card   boardCard
			status string
			pickup sql.NullTime
		)
		if err := rows.Scan(&card.ID, &card.Number, &card.Customer, &status,
			&card.Freight, &pickup, &card.Vehicle, &card.Driver); err != nil {
			httpx.Error(w, r, err)
			return
		}
		if pickup.Valid {
			card.Deadline = pickup.Time.Format("2006-01-02")
		}
		if _, known := cardsByStatus[status]; !known {
			status = "pending"
		}
		cardsByStatus[status] = append(cardsByStatus[status], card)
	}
	if err := rows.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	columns := make([]boardColumn, 0, len(boardColumns))
	for _, s := range boardColumns {
		columns = append(columns, boardColumn{Status: s, Cards: cardsByStatus[s]})
	}

	if r.URL.Query().Get("format") == "json" {
		httpx.JSON(w, http.StatusOK, map[string]any{"columns": columns})
		return
	}

	session, _ := r.Context().Value(auth.ContextUser).(*auth.SessionData)
	h.renderPage(w, r, "bookings_board.html", PageData{
		Title: "Bookings Board",
		User:  session,
		Extra: map[string]interface{}{"Columns": columns},
	})
}
