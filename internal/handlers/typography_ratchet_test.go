package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// maxFontBold is the ratchet for the bold-discipline convention: routine
// emphasis belongs one step down (semibold/medium); font-bold is reserved
// for KPIs, page titles, and amounts. Lower it when you demote usages —
// never raise it without a design reason recorded here.
const maxFontBold = 1135

// TestTypographyBoldRatchet fails when new font-bold usages appear in
// server-rendered templates, keeping hierarchy-by-restraint enforceable
// instead of advisory.
func TestTypographyBoldRatchet(t *testing.T) {
	count := 0
	err := filepath.Walk(filepath.Join("..", "templates"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		count += strings.Count(string(b), "font-bold")
		return nil
	})
	require.NoError(t, err)
	assert.LessOrEqual(t, count, maxFontBold,
		"font-bold usages grew to %d (cap %d): demote routine emphasis to semibold/medium, reserve bold for KPIs/titles/amounts", count, maxFontBold)
}
