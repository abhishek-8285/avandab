package handlers

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Login form must render Hindi when the hi bundle is active.
func TestLoginFormHindiRenders(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	tmplHI, err := parseTemplatesLang(&mockAuthSvc{}, "hi")
	require.NoError(t, err)
	data := map[string]interface{}{
		"Redirect": "/", "Email": "", "GoogleEnabled": true,
	}
	var bufHI bytes.Buffer
	require.NoError(t, tmplHI.ExecuteTemplate(&bufHI, "login_form.html", data))
	outHI := bufHI.String()
	assert.Contains(t, outHI, "अपने कंसोल में साइन इन करें")
	assert.Contains(t, outHI, "साइन इन")
	assert.NotContains(t, outHI, "Sign in to your console")
}
