package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	privacyapp "transport-app/internal/privacy/application"
	"transport-app/internal/shared"
	"transport-app/internal/shared/id"
)

func newDSARTestHandlers(t *testing.T) (*DSARHandlers, *sql.DB, string) {
	t.Helper()
	name := fmt.Sprintf("test_dsar_h_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	migrationsDir := "../../../../../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })

	const tenant = "tenant_dsar_api"
	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ($1, $2, $3)`,
		tenant, tenant, tenant)
	require.NoError(t, err)

	svc := privacyapp.NewDSARService(db, id.NewUUIDGenerator())
	return NewDSARHandlers(svc), db, tenant
}

func dsarReqWithContext(req *http.Request, tenantID, userID, role string) *http.Request {
	ctx := context.WithValue(req.Context(), shared.TenantIDKey, shared.TenantID(tenantID))
	if userID != "" {
		ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{
			UserID: userID,
			Role:   role,
		})
	}
	return req.WithContext(ctx)
}

func TestDSARAPI_Export_Self(t *testing.T) {
	h, db, tenantID := newDSARTestHandlers(t)

	// Seed user
	userID := "usr_api_1"
	_, err := db.Exec(`
		INSERT INTO users (id, tenant_id, name, email, phone, password_hash, role_id, status)
		VALUES ($1, $2, 'Ananya Sen', 'ananya@example.com', '9811223344', 'hash123', 2, 'active')`,
		userID, tenantID)
	require.NoError(t, err)

	// Request: User exporting self
	req := httptest.NewRequest(http.MethodPost, "/api/v1/privacy/dsar/export", nil)
	req = dsarReqWithContext(req, tenantID, userID, "user")
	w := httptest.NewRecorder()

	h.ExportDSARAPI(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, tenantID, resp["tenant_id"])
	assert.Equal(t, userID, resp["requested_by"])
	userData, ok := resp["user"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "Ananya Sen", userData["name"])
	assert.Equal(t, "ananya@example.com", userData["email"])
}

func TestDSARAPI_Export_DownloadFormat(t *testing.T) {
	h, db, tenantID := newDSARTestHandlers(t)

	userID := "usr_api_dl"
	_, err := db.Exec(`
		INSERT INTO users (id, tenant_id, name, email, password_hash, role_id, status)
		VALUES ($1, $2, 'Download Tester', 'dl@example.com', 'hash', 2, 'active')`,
		userID, tenantID)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/privacy/dsar/export?format=download", nil)
	req = dsarReqWithContext(req, tenantID, userID, "user")
	w := httptest.NewRecorder()

	h.ExportDSARAPI(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	disposition := w.Header().Get("Content-Disposition")
	assert.Contains(t, disposition, "attachment;")
	assert.Contains(t, disposition, fmt.Sprintf("dsar_export_%s.json", userID))
}

func TestDSARAPI_Export_Admin_OnBehalf(t *testing.T) {
	h, db, tenantID := newDSARTestHandlers(t)

	adminID := "usr_admin"
	targetID := "usr_target"
	_, err := db.Exec(`
		INSERT INTO users (id, tenant_id, name, email, password_hash, role_id, status)
		VALUES ($1, $2, 'Target User', 'target@example.com', 'hash', 2, 'active')`,
		targetID, tenantID)
	require.NoError(t, err)

	body := strings.NewReader(fmt.Sprintf(`{"user_id": "%s"}`, targetID))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/privacy/dsar/export", body)
	req = dsarReqWithContext(req, tenantID, adminID, "admin")
	w := httptest.NewRecorder()

	h.ExportDSARAPI(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, adminID, resp["requested_by"])
	userData, ok := resp["user"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, targetID, userData["id"])
}

func TestDSARAPI_Export_NonAdmin_Forbidden(t *testing.T) {
	h, _, tenantID := newDSARTestHandlers(t)

	userA := "usr_a"
	userB := "usr_b"

	body := strings.NewReader(fmt.Sprintf(`{"user_id": "%s"}`, userB))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/privacy/dsar/export", body)
	req = dsarReqWithContext(req, tenantID, userA, "driver")
	w := httptest.NewRecorder()

	h.ExportDSARAPI(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDSARAPI_Erase_Self(t *testing.T) {
	h, db, tenantID := newDSARTestHandlers(t)

	userID := "usr_erase_self"
	_, err := db.Exec(`
		INSERT INTO users (id, tenant_id, name, email, password_hash, role_id, status)
		VALUES ($1, $2, 'To Be Erased', 'eraseself@example.com', 'hash', 2, 'active')`,
		userID, tenantID)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/privacy/dsar/erase", nil)
	req = dsarReqWithContext(req, tenantID, userID, "user")
	w := httptest.NewRecorder()

	h.EraseDSARAPI(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, true, resp["success"])
	assert.Equal(t, userID, resp["user_id"])
	assert.Contains(t, resp["retained_records_notice"], "CGST Act")
}

func TestDSARAPI_Erase_NonAdmin_Forbidden(t *testing.T) {
	h, _, tenantID := newDSARTestHandlers(t)

	userA := "usr_a"
	userB := "usr_b"

	body := strings.NewReader(fmt.Sprintf(`{"user_id": "%s"}`, userB))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/privacy/dsar/erase", body)
	req = dsarReqWithContext(req, tenantID, userA, "viewer")
	w := httptest.NewRecorder()

	h.EraseDSARAPI(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDSARAPI_RetentionSchedule(t *testing.T) {
	h, _, _ := newDSARTestHandlers(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/privacy/retention-schedule", nil)
	w := httptest.NewRecorder()

	h.RetentionScheduleAPI(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	sched, ok := resp["retention_schedule"].([]interface{})
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(sched), 5)
}

func TestDSARAPI_SubProcessors(t *testing.T) {
	h, _, _ := newDSARTestHandlers(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/privacy/sub-processors", nil)
	w := httptest.NewRecorder()

	h.SubProcessorsAPI(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	procs, ok := resp["sub_processors"].([]interface{})
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(procs), 5)
}

func TestDSARAPI_Unauthenticated(t *testing.T) {
	h, _, tenantID := newDSARTestHandlers(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/privacy/dsar/export", nil)
	// Without session user context
	ctx := context.WithValue(req.Context(), shared.TenantIDKey, shared.TenantID(tenantID))
	w := httptest.NewRecorder()

	h.ExportDSARAPI(w, req.WithContext(ctx))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
