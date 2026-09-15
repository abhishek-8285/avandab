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

func TestParseDateParamStrict(t *testing.T) {
	// Real dates pass in both accepted formats, with no error.
	v, ok := parseDateParamStrict("2026-08-25")
	assert.True(t, ok)
	assert.Equal(t, "2026-08-25", v)
	v, ok = parseDateParamStrict("25-08-2026")
	assert.True(t, ok)
	assert.Equal(t, "2026-08-25", v)
	// Empty means "no bound", not an error.
	v, ok = parseDateParamStrict("")
	assert.True(t, ok)
	assert.Equal(t, "", v)
	// Impossible / malformed input must FAIL LOUDLY: callers drop the value
	// from the query, so without this flag the list silently returns
	// unfiltered rows.
	for _, bad := range []string{"31-02-2026", "garbage", "32-13-2026", "--08-2026"} {
		_, ok = parseDateParamStrict(bad)
		assert.False(t, ok, "input %q must be rejected", bad)
	}
}
