package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
	"transport-app/internal/handlers"
	"transport-app/internal/shared"
)

func TestUserThemePreferences(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	repo := NewTestRepo(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// 1. Create a test user
	u, err := svcs.Users.CreateUserWithPassword(ctx, "theme_test@example.com", "Theme Tester", "9876543210", "StrongPassword123!", 1, domain.UserStatusActive, string(shared.DefaultTenant))
	require.NoError(t, err)
	assert.Equal(t, "system", u.ThemePreference)

	// 2. Test UserService.UpdateThemePreference directly
	updated, err := svcs.Users.UpdateThemePreference(ctx, u.ID, "dark")
	require.NoError(t, err)
	assert.Equal(t, "dark", updated.ThemePreference)

	// Verify persistence in DB
	fromDB, err := repo.GetUserByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, "dark", fromDB.ThemePreference)

	// Reject invalid theme value
	_, err = svcs.Users.UpdateThemePreference(ctx, u.ID, "neon_blue")
	assert.Error(t, err)

	// 3. Test HTTP handlers
	cfg := loadTestConfig()
	sessionStore := auth.NewSessionStore(cfg.CookieSecret, false)
	authSrv := &stubAuthSvc{}
	app := handlers.NewApp(svcs, cfg, sessionStore, db, authSrv, auth.NewResetTokenStore(0))

	r := chi.NewRouter()
	r.Get("/api/v1/users/me/preferences", app.Users.GetMyPreferences)
	r.Patch("/api/v1/users/me/preferences", app.Users.UpdateMyPreferences)

	// A. Unauthenticated GET returns 401
	unauthReq := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/preferences", nil)
	unauthRec := httptest.NewRecorder()
	r.ServeHTTP(unauthRec, unauthReq)
	assert.Equal(t, http.StatusUnauthorized, unauthRec.Code)

	// B. Authenticated GET returns current theme
	authCtx := context.WithValue(ctx, auth.ContextUser, &auth.SessionData{
		UserID: string(u.ID),
		Role:   "admin",
	})
	authCtx = shared.ContextWithTenantID(authCtx, "1")

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/preferences", nil).WithContext(authCtx)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)
	assert.Equal(t, http.StatusOK, getRec.Code)

	var getResp map[string]interface{}
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &getResp))
	assert.Equal(t, "dark", getResp["theme_preference"])

	// C. Authenticated PATCH updates to "light"
	patchBody, _ := json.Marshal(map[string]string{"theme": "light"})
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/v1/users/me/preferences", bytes.NewReader(patchBody)).WithContext(authCtx)
	patchReq.Header.Set("Content-Type", "application/json")
	patchRec := httptest.NewRecorder()
	r.ServeHTTP(patchRec, patchReq)
	assert.Equal(t, http.StatusOK, patchRec.Code)

	var patchResp map[string]interface{}
	require.NoError(t, json.Unmarshal(patchRec.Body.Bytes(), &patchResp))
	assert.Equal(t, "light", patchResp["theme_preference"])

	// D. Authenticated PATCH updates to "system"
	patchBodySys, _ := json.Marshal(map[string]string{"theme": "system"})
	patchReqSys := httptest.NewRequest(http.MethodPatch, "/api/v1/users/me/preferences", bytes.NewReader(patchBodySys)).WithContext(authCtx)
	patchReqSys.Header.Set("Content-Type", "application/json")
	patchRecSys := httptest.NewRecorder()
	r.ServeHTTP(patchRecSys, patchReqSys)
	assert.Equal(t, http.StatusOK, patchRecSys.Code)

	// Verify DB updated to system
	fromDBSys, err := repo.GetUserByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, "system", fromDBSys.ThemePreference)

	// E. Authenticated PATCH with invalid theme returns 400
	badBody, _ := json.Marshal(map[string]string{"theme": "invalid_mode"})
	badReq := httptest.NewRequest(http.MethodPatch, "/api/v1/users/me/preferences", bytes.NewReader(badBody)).WithContext(authCtx)
	badReq.Header.Set("Content-Type", "application/json")
	badRec := httptest.NewRecorder()
	r.ServeHTTP(badRec, badReq)
	assert.Equal(t, http.StatusBadRequest, badRec.Code)

	// 4. Verify all HTML templates include theme_head and theme.js
	templatesToCheck := []string{
		"../internal/templates/layout.html",
		"../internal/templates/auth_layout.html",
		"../internal/templates/home.html",
		"../internal/templates/feature.html",
		"../internal/templates/privacy.html",
		"../internal/templates/terms.html",
		"../internal/templates/refunds.html",
		"../internal/templates/share_public.html",
		"../internal/templates/share_pin_form.html",
	}
	for _, tplPath := range templatesToCheck {
		data, err := os.ReadFile(tplPath)
		require.NoError(t, err, "Failed to read template %s", tplPath)
		content := string(data)
		assert.Contains(t, content, `theme_head.html`, "Template %s must include theme_head.html", tplPath)
		assert.Contains(t, content, `theme.js`, "Template %s must include theme.js", tplPath)
	}

	// 5. Verify public_header.html and layout.html have 3-way dropdown options
	headerData, err := os.ReadFile("../internal/templates/partials/public_header.html")
	require.NoError(t, err)
	headerStr := string(headerData)
	assert.Contains(t, headerStr, `data-theme-choice="light"`)
	assert.Contains(t, headerStr, `data-theme-choice="dark"`)
	assert.Contains(t, headerStr, `data-theme-choice="system"`)

	layoutData, err := os.ReadFile("../internal/templates/layout.html")
	require.NoError(t, err)
	layoutStr := string(layoutData)
	assert.Contains(t, layoutStr, `data-theme-choice="light"`)
	assert.Contains(t, layoutStr, `data-theme-choice="dark"`)
	assert.Contains(t, layoutStr, `data-theme-choice="system"`)
}
