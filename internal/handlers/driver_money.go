package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/httpx"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// DriverMoneyHandlers serves the driver Paisa tab (Spec 22 §2.6).
// Driver self endpoints resolve the driver from the authenticated user;
// the decision endpoint is admin-side (kharcha:approve).
type DriverMoneyHandlers struct {
	app *App
	db  *sql.DB
	svc *service.DriverBalanceService
}

func NewDriverMoneyHandlers(app *App, db *sql.DB, svc *service.DriverBalanceService) *DriverMoneyHandlers {
	return &DriverMoneyHandlers{app: app, db: db, svc: svc}
}

// driverIDForUser maps an authenticated user to their drivers.id using the
// same identity rule as GET /api/v1/drivers/me (id match or email match).
func (h *DriverMoneyHandlers) driverIDForUser(r *http.Request) (string, bool) {
	session, ok := h.app.getUserFromContext(r)
	if !ok || session == nil || session.UserID == "" {
		return "", false
	}
	var id string
	err := h.db.QueryRowContext(r.Context(), `
		SELECT id FROM drivers
		WHERE id = ? OR email = (SELECT email FROM users WHERE id = ?)
		LIMIT 1`, session.UserID, session.UserID).Scan(&id)
	if err != nil {
		return "", false
	}
	return id, true
}

// Balance handles GET /api/driver/balance.
func (h *DriverMoneyHandlers) Balance(w http.ResponseWriter, r *http.Request) {
	driverID, ok := h.driverIDForUser(r)
	if !ok {
		httpx.JSON(w, http.StatusNotFound, map[string]string{"error": "driver_not_linked"})
		return
	}
	bal, err := h.svc.GetBalance(r.Context(), driverID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, bal)
}

// Settlements handles GET /api/driver/settlements.
func (h *DriverMoneyHandlers) Settlements(w http.ResponseWriter, r *http.Request) {
	driverID, ok := h.driverIDForUser(r)
	if !ok {
		httpx.JSON(w, http.StatusNotFound, map[string]string{"error": "driver_not_linked"})
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, trip_id, gross_fare, deductions, net_payout,
		       status, COALESCE(tds_amount,0), paid_at
		FROM driver_settlements WHERE driver_id = ?
		ORDER BY created_at DESC LIMIT 50`, driverID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	defer func() { _ = rows.Close() }()

	settlements := []map[string]any{}
	for rows.Next() {
		var id, status string
		var period sql.NullString
		var gross, deductions, net, tds float64
		var paidAt sql.NullTime
		if err := rows.Scan(&id, &period, &gross, &deductions, &net, &status, &tds, &paidAt); err != nil {
			httpx.Error(w, r, err)
			return
		}
		item := map[string]any{
			"id":         id,
			"period":     period.String,
			"gross":      gross,
			"deductions": deductions,
			"net":        net,
			"status":     status,
			"tds":        tds,
		}
		if paidAt.Valid {
			item["paid_at"] = paidAt.Time.UTC().Format(time.RFC3339)
		}
		settlements = append(settlements, item)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"settlements": settlements})
}

type advanceRequestBody struct {
	TripID string  `json:"trip_id"`
	Amount float64 `json:"amount"`
	Reason string  `json:"reason"`
}

// RequestAdvance handles POST /api/driver/advances.
func (h *DriverMoneyHandlers) RequestAdvance(w http.ResponseWriter, r *http.Request) {
	driverID, ok := h.driverIDForUser(r)
	if !ok {
		httpx.JSON(w, http.StatusNotFound, map[string]string{"error": "driver_not_linked"})
		return
	}
	var body advanceRequestBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || body.Amount <= 0 {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "amount must be a positive number"})
		return
	}
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	adv, err := h.svc.RequestAdvance(r.Context(), tenantID, driverID, body.TripID, body.Amount, body.Reason)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": adv.ID, "status": adv.Status})
}

// ListAdvances handles GET /api/driver/advances.
func (h *DriverMoneyHandlers) ListAdvances(w http.ResponseWriter, r *http.Request) {
	driverID, ok := h.driverIDForUser(r)
	if !ok {
		httpx.JSON(w, http.StatusNotFound, map[string]string{"error": "driver_not_linked"})
		return
	}
	advances, err := h.svc.ListAdvances(r.Context(), driverID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"advances": advances})
}

type decisionBody struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

// DecideAdvance handles POST /api/driver/advances/{id}/decision (admin side).
func (h *DriverMoneyHandlers) DecideAdvance(w http.ResponseWriter, r *http.Request) {
	userID := contextUserID(r)
	var body decisionBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.svc.DecideAdvance(r.Context(), id, body.Decision, userID, body.Note); err != nil {
		if err.Error() == "advance not found or already decided" {
			httpx.JSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		httpx.Error(w, r, err)
		return
	}
	writeAuditLog(r, h.db, "driver_advance."+body.Decision, "driver_advance_requests", id, map[string]any{
		"note": body.Note,
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
