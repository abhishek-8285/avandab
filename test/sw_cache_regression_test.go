package test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression test: the service worker must key static cache by full URL
// INCLUDING ?v=. With ignoreSearch:true every deploy's version bump was
// defeated — phones kept serving stale app.css (both hamburger icons
// visible) long after the fix was live on the network.
func TestServiceWorker_VersionedAssetsBustCache(t *testing.T) {
	data, err := os.ReadFile("../internal/static/js/sw.js")
	require.NoError(t, err)
	sw := string(data)

	assert.NotContains(t, sw, "ignoreSearch: true",
		"sw.js must not ignore query strings: ?v= bumps are the cache-buster")

	// Static strategy stays cache-first (offline shell), keyed exactly.
	assert.Contains(t, sw, "caches.match(request)",
		"static cache lookup must use the exact request URL")
}
