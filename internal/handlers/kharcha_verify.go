package handlers

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/httpx"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// KharchaVerifyHandlers serve the S8 verification API surface (Spec 22 §2.7):
// on-demand OCR extraction and the flagged review queue. The async
// auto-verify itself runs off the event bus (KharchaVerifyService).
type KharchaVerifyHandlers struct {
	app *App
	svc *service.KharchaVerifyService
}

func NewKharchaVerifyHandlers(app *App, svc *service.KharchaVerifyService) *KharchaVerifyHandlers {
	return &KharchaVerifyHandlers{app: app, svc: svc}
}

// OCRExtract handles POST /api/expenses/{id}/ocr — server-side OCR on the
// receipt already on file; returns the extracted amount + confidence and
// persists ocr_amount/ocr_confidence. Same gate as expense creation
// (trips:update) so drivers can OCR their own claims.
func (h *KharchaVerifyHandlers) OCRExtract(w http.ResponseWriter, r *http.Request) {
	expenseID := chi.URLParam(r, "id")
	state, err := h.svc.VerifyExpense(r.Context(), expenseID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var amt, conf any
	err = h.app.DB.QueryRowContext(r.Context(),
		`SELECT ocr_amount, ocr_confidence FROM driver_expenses WHERE id = ?`, expenseID).
		Scan(&amt, &conf)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"verification_state": state,
		"ocr_amount":         amt,
		"ocr_confidence":     conf,
	})
}

// FlaggedQueue handles GET /api/expenses/flagged (kharcha:approve) —
// flagged first, then manual, with evidence fields for review.
func (h *KharchaVerifyHandlers) FlaggedQueue(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	expenses, err := h.svc.ListFlaggedExpenses(r.Context(), tenantID, 100)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"expenses": expenses})
}
