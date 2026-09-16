package handlers

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Home page must render Hindi when the hi bundle is active.
func TestHomeHindiRenders(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	tmplHI, err := parseTemplatesLang(&mockAuthSvc{}, "hi")
	require.NoError(t, err)
	data := map[string]interface{}{
		"Version": "test", "Title": "T", "Lang": "hi",
		"SEODescription": "d", "CanonicalPath": "/", "NoIndex": false,
		"OGType": "website",
	}
	var bufHI bytes.Buffer
	require.NoError(t, tmplHI.ExecuteTemplate(&bufHI, "home.html", data))
	outHI := bufHI.String()
	assert.Contains(t, outHI, "फ्लीट संचालन")
	assert.Contains(t, outHI, "मुफ्त शुरू करें")
	assert.Contains(t, outHI, "<html lang=\"hi\">")
	assert.NotContains(t, outHI, "Fleet operations")
	// Shared partials render Hindi too.
	assert.Contains(t, outHI, "संचालन कॉकपिट")
	assert.Contains(t, outHI, "सहायता पाएं")
	assert.Contains(t, outHI, "उपयोग की शर्तें")
	assert.Contains(t, outHI, "आपकी गोपनीयता हमारे लिए अहम है")
	assert.Contains(t, outHI, "id=\"lang-toggle\"")
}
