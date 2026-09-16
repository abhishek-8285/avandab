package handlers

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Bookings board must render Hindi labels when the hi bundle is active.
func TestBookingsBoardHindiRenders(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	tmplHI, err := parseTemplatesLang(&mockAuthSvc{}, "hi")
	require.NoError(t, err)
	data := buildTemplateData(PageData{Title: "Board"})
	data["Columns"] = []map[string]interface{}{
		{"Status": "pending", "Cards": []map[string]interface{}{}},
	}
	var bufHI bytes.Buffer
	require.NoError(t, tmplHI.ExecuteTemplate(&bufHI, "bookings_board.html", data))
	outHI := bufHI.String()
	assert.Contains(t, outHI, "बुकिंग बोर्ड")
	assert.Contains(t, outHI, "कार्ड यहां छोड़ें")
	assert.NotContains(t, outHI, "Bookings Board")
}
