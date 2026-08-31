package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
)

func setupFilesAPITest(t *testing.T, allowed map[string]bool) (*App, http.Handler, *sql.DB) {
	t.Helper()

	name := fmt.Sprintf("test_files_api_%d", time.Now().UnixNano())
	dbConn, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })

	migrationsDir := "../../db/migrations"
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		for _, cand := range []string{"db/migrations", "../db/migrations", "../../db/migrations"} {
			if _, err := os.Stat(cand); err == nil {
				migrationsDir = cand
				break
			}
		}
	}
	goose.SetLogger(goose.NopLogger())
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(dbConn, migrationsDir))

	cfg := &config.Config{AppEnv: "testing", UploadDir: t.TempDir()}
	authSvc := &mockAuthSvc{allowed: allowed}

	repo := sqlite.NewRepository(dbConn)
	services := service.NewServices(repo, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), events.NewInMemoryBus())

	app := &App{DB: dbConn, Config: cfg, Services: services, AuthSrv: authSvc}
	filesAPI := NewFilesAPIHandlers(app, services.Files, authSvc)

	rtr := chi.NewRouter()
	filesAPI.Mount(rtr)
	return app, rtr, dbConn
}

func filesAPIContext(r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), auth.ContextUser, &auth.SessionData{UserID: "u-admin-1", Role: "admin"})
	return r.WithContext(ctx)
}

// pngBytes returns a minimal valid PNG header so magic-byte detection passes.
// A function (not a package-level var) keeps test state isolated per call.
func pngBytes() []byte {
	return []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	}
}

func multipartFileRequest(t *testing.T, url string, filename string, content []byte, fields map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	fw, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = fw.Write(content)
	require.NoError(t, err)
	for k, v := range fields {
		require.NoError(t, writer.WriteField(k, v))
	}
	require.NoError(t, writer.Close())

	req, err := http.NewRequest(http.MethodPost, url, &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestFilesAPIUploadRoundTrip(t *testing.T) {
	app, rtr, dbConn := setupFilesAPITest(t, nil) // nil map => mockAuthSvc allows everything

	// Upload
	req := multipartFileRequest(t, "/api/v1/files", "proof.png", pngBytes(), map[string]string{
		"uploadable_type": "trip_pod",
		"uploadable_id":   "trip-123",
	})
	rec := httptest.NewRecorder()
	rtr.ServeHTTP(rec, filesAPIContext(req))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var uploaded map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &uploaded))
	assert.Equal(t, "trip_pod", uploaded["uploadable_type"])
	assert.Equal(t, "trip-123", uploaded["uploadable_id"])
	assert.Equal(t, "image/png", uploaded["content_type"])
	fileID, ok := uploaded["id"].(string)
	require.True(t, ok)
	require.NotEmpty(t, fileID)

	// Blob stored under trips/ subdir inside UploadDir
	storedPath := filepath.Join(app.Config.UploadDir, "trips")
	entries, err := os.ReadDir(storedPath)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	// Metadata GET
	rec = httptest.NewRecorder()
	getReq, err := http.NewRequest(http.MethodGet, "/api/v1/files/"+fileID, nil)
	require.NoError(t, err)
	rtr.ServeHTTP(rec, filesAPIContext(getReq))
	require.Equal(t, http.StatusOK, rec.Code)
	var meta map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &meta))
	assert.Equal(t, fileID, meta["id"])
	assert.Contains(t, meta["url"], "/files/")

	// List by entity
	rec = httptest.NewRecorder()
	listReq, err := http.NewRequest(http.MethodGet, "/api/v1/files?uploadable_type=trip_pod&uploadable_id=trip-123", nil)
	require.NoError(t, err)
	rtr.ServeHTTP(rec, filesAPIContext(listReq))
	require.Equal(t, http.StatusOK, rec.Code)
	var list []map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 1)
	assert.Equal(t, fileID, list[0]["id"])

	// Delete then verify gone
	rec = httptest.NewRecorder()
	delReq, err := http.NewRequest(http.MethodDelete, "/api/v1/files/"+fileID, nil)
	require.NoError(t, err)
	rtr.ServeHTTP(rec, filesAPIContext(delReq))
	require.Equal(t, http.StatusNoContent, rec.Code)

	var count int
	require.NoError(t, dbConn.QueryRow(`SELECT count(*) FROM files WHERE id = ?`, fileID).Scan(&count))
	assert.Zero(t, count)
	assert.Empty(t, entriesOnDisk(t, storedPath), "blob must be removed from disk")

	rec = httptest.NewRecorder()
	rtr.ServeHTTP(rec, filesAPIContext(getReq))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func entriesOnDisk(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	return entries
}

func TestFilesAPIValidation(t *testing.T) {
	_, rtr, _ := setupFilesAPITest(t, nil) // nil map => mockAuthSvc allows everything

	// Invalid uploadable_type rejected
	req := multipartFileRequest(t, "/api/v1/files", "x.png", pngBytes(), map[string]string{
		"uploadable_type": "not_a_type",
		"uploadable_id":   "e1",
	})
	rec := httptest.NewRecorder()
	rtr.ServeHTTP(rec, filesAPIContext(req))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// Missing file field rejected
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	require.NoError(t, writer.WriteField("uploadable_type", "general"))
	require.NoError(t, writer.Close())
	req2, err := http.NewRequest(http.MethodPost, "/api/v1/files", &buf)
	require.NoError(t, err)
	req2.Header.Set("Content-Type", writer.FormDataContentType())
	rec = httptest.NewRecorder()
	rtr.ServeHTTP(rec, filesAPIContext(req2))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// List without params rejected
	listReq, err := http.NewRequest(http.MethodGet, "/api/v1/files", nil)
	require.NoError(t, err)
	rec = httptest.NewRecorder()
	rtr.ServeHTTP(rec, filesAPIContext(listReq))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestFilesAPIPermissionsDenied(t *testing.T) {
	_, rtr, _ := setupFilesAPITest(t, map[string]bool{
		"u-admin-1:files:read": true,
		// deliberately missing files:create and files:delete
	})

	// Permission denied for upload (no files:create)
	req3 := multipartFileRequest(t, "/api/v1/files", "y.png", pngBytes(), map[string]string{
		"uploadable_type": "general",
	})
	rec := httptest.NewRecorder()
	rtr.ServeHTTP(rec, filesAPIContext(req3))
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// Permission denied for delete (no files:delete)
	delReq, err := http.NewRequest(http.MethodDelete, "/api/v1/files/some-id", nil)
	require.NoError(t, err)
	rec = httptest.NewRecorder()
	rtr.ServeHTTP(rec, filesAPIContext(delReq))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}
