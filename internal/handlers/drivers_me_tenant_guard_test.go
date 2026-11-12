package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"transport-app/internal/auth"
)

// A missing tenant means a bad/expired session: the driver self-service
// handlers must answer 401, never panic into a 500 via Recoverer
// (AGENTS.md security gate: no MustTenantID on a request path).
func TestDriverMe_MissingTenantAnswers401(t *testing.T) {
	h := &DriverHandlers{App: &App{}}
	user := &auth.SessionData{UserID: "u-tenantless"}

	calls := map[string]http.HandlerFunc{
		"GetMe":    h.GetMe,
		"UpdateMe": h.UpdateMe,
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/drivers/me", strings.NewReader(`{}`))
			r = r.WithContext(context.WithValue(r.Context(), auth.ContextUser, user))
			w := httptest.NewRecorder()

			call(w, r)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})
	}
}
