package handlers

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
)

// Assistant page must render Hindi when the hi bundle is active and keep
// English as the default. Guards the {{t}} wiring in assistant.html.
func TestAssistantHindiRenders(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	tmplHI, err := parseTemplatesLang(&mockAuthSvc{}, "hi")
	require.NoError(t, err)
	var bufHI bytes.Buffer
	data := buildTemplateData(PageData{Title: "AI Assistant", User: &auth.SessionData{UserID: "u", Name: "Test"}})
	require.NoError(t, tmplHI.ExecuteTemplate(&bufHI, "assistant.html", data))
	outHI := bufHI.String()
	assert.Contains(t, outHI, "AI संचालन सहायक")
	assert.Contains(t, outHI, "भेजें")
	assert.NotContains(t, outHI, "AI Operations Assistant")

	tmplEN, err := parseTemplatesLang(&mockAuthSvc{}, "en")
	require.NoError(t, err)
	var bufEN bytes.Buffer
	require.NoError(t, tmplEN.ExecuteTemplate(&bufEN, "assistant.html", data))
	outEN := bufEN.String()
	assert.Contains(t, outEN, "AI Operations Assistant")
	assert.Contains(t, outEN, "Send")
}
