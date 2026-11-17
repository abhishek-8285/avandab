package handlers

// Regression test for the multipart request cap on the driver expense API:
// ParseMultipartForm's maxMemory is only a spill threshold, so the handler
// must bound the body itself (audit G120 follow-up).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"transport-app/internal/auth"
	"transport-app/internal/shared"
)

func TestCreateExpenseAPI_RejectsOversizedBody(t *testing.T) {
	dbConn, app, _ := setupMobileAPITestEnv(t)
	defer dbConn.Close()

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
			ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "u-drv-1", Role: "driver"})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Post("/api/v1/kharcha/expense", app.Kharcha.CreateExpenseAPI)

	// 11MB body: above the 10MB cap, below the point where the multipart
	// parser would accept a field.
	body := strings.NewReader("--x\r\nContent-Disposition: form-data; name=\"trip_id\"\r\n\r\n" +
		strings.Repeat("A", 11<<20) + "\r\n--x--\r\n")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kharcha/expense", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, "oversized multipart body must be refused")
}
