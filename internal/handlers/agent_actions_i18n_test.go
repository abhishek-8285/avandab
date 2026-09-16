package handlers

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Agent actions queue must render Hindi when the hi bundle is active.
func TestAgentActionsHindiRenders(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	tmplHI, err := parseTemplatesLang(&mockAuthSvc{}, "hi")
	require.NoError(t, err)
	data := buildTemplateData(PageData{Title: "Actions"})
	data["Actions"] = []interface{}{}
	var bufHI bytes.Buffer
	require.NoError(t, tmplHI.ExecuteTemplate(&bufHI, "agent_actions.html", data))
	outHI := bufHI.String()
	assert.Contains(t, outHI, "एजेंट मंजूरी कतार")
	assert.Contains(t, outHI, "कतार साफ है")
	assert.NotContains(t, outHI, "Agent Approval Queue")
}
