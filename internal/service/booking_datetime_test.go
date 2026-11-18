package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The agent tool formats its pickup time as RFC3339 ("...Z"), which the parser
// used to reject, making every create_booking action fail.
func TestParseDateTime_AcceptsRFC3339(t *testing.T) {
	for _, in := range []string{"2026-11-20T09:00:00Z", "2026-11-20T09:00:00+05:30"} {
		got, err := parseDateTime(in)
		require.NoError(t, err, "must parse %s", in)
		want, _ := time.Parse(time.RFC3339, in)
		require.True(t, got.Equal(want))
	}
}
