package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderPage must serve Hindi content when the lang cookie is hi —
// content templates come from the same lang-aware set as the layout.
// Regression proof: content used to render from the English set always.
func TestRenderPageServesHindiContent(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	authSvc := &mockAuthSvc{}
	tmplEN, err := parseTemplates(authSvc)
	require.NoError(t, err)
	tmplHI, err := parseTemplatesLang(authSvc, "hi")
	require.NoError(t, err)
	app := &App{Templates: tmplEN, TemplatesHI: tmplHI, AuthSrv: authSvc}

	data := buildTemplateData(PageData{Title: "Board"})
	data["Columns"] = []map[string]interface{}{
		{"Status": "pending", "Cards": []map[string]interface{}{}},
	}

	reqHI := httptest.NewRequest(http.MethodGet, "/bookings/board", nil)
	reqHI.AddCookie(&http.Cookie{Name: "lang", Value: "hi"})
	// renderPage takes PageData; build one mirroring the test map.
	pd := PageData{Title: "Board", Extra: map[string]interface{}{"Columns": data["Columns"]}}
	wHI := httptest.NewRecorder()
	app.renderPage(wHI, reqHI, "bookings_board.html", pd)
	require.Equal(t, http.StatusOK, wHI.Code)
	assert.Contains(t, wHI.Body.String(), "बुकिंग बोर्ड")

	reqEN := httptest.NewRequest(http.MethodGet, "/bookings/board", nil)
	wEN := httptest.NewRecorder()
	app.renderPage(wEN, reqEN, "bookings_board.html", pd)
	require.Equal(t, http.StatusOK, wEN.Code)
	assert.Contains(t, wEN.Body.String(), "Bookings Board")
}
