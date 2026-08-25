package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseDateParam(t *testing.T) {
	// ISO accepted as-is
	assert.Equal(t, "2026-08-25", parseDateParam("2026-08-25"))
	// Indian DD-MM-YYYY converted to ISO
	assert.Equal(t, "2026-08-25", parseDateParam("25-08-2026"))
	assert.Equal(t, "2026-01-05", parseDateParam("05-01-2026"))
	// Empty / invalid rejected
	assert.Equal(t, "", parseDateParam(""))
	assert.Equal(t, "", parseDateParam("garbage"))
	assert.Equal(t, "", parseDateParam("32-13-2026"))
	// Slash-separated DD/MM/YYYY not supported (kept strict to dash)
	assert.Equal(t, "", parseDateParam("25/08/2026"))
}

func TestInDate(t *testing.T) {
	assert.Equal(t, "25-08-2026", inDate("2026-08-25"))
	assert.Equal(t, "", inDate(""))
	assert.Equal(t, "", inDate(nil))
	// Non-ISO passthrough (defensive)
	assert.Equal(t, "junk", inDate("junk"))
}
