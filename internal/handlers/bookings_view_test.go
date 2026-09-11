package handlers

// Bookings board is the default view (kanban over table): the sidebar lands
// on /bookings/board, and both pages cross-link via a Board/List toggle.
import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBookingsBoard_IsDefaultView(t *testing.T) {
	layout, err := os.ReadFile("../templates/layout.html")
	require.NoError(t, err)
	require.Contains(t, string(layout), `<a href="/bookings/board"`,
		"sidebar Bookings link must land on the board")

	board, err := os.ReadFile("../templates/bookings_board.html")
	require.NoError(t, err)
	require.Contains(t, string(board), `<a href="/bookings/board" aria-current="page"`,
		"board must mark Board active")
	require.Contains(t, string(board), `<a href="/bookings"`,
		"board must link back to the list")

	list, err := os.ReadFile("../templates/booking_list.html")
	require.NoError(t, err)
	require.Contains(t, string(list), `<a href="/bookings" aria-current="page"`,
		"list must mark List active")
	require.Contains(t, string(list), `<a href="/bookings/board"`,
		"list must link to the board")
}

// TestBookingsBoard_RendersCardsThroughRealPath renders the board exactly
// as renderPage does (buildTemplateData flattens Extra to top level) and
// asserts lanes + cards appear. History: the template ranged over
// `.Extra.Columns`, which is nil post-flattening — the page returned 200
// with zero lanes for every tenant while JSON + no-error render tests
// stayed green.
func TestBookingsBoard_RendersCardsThroughRealPath(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	tmpl, err := parseTemplates(&mockAuthSvc{})
	require.NoError(t, err)
	cols := []boardColumn{
		{Status: "pending", Cards: []boardCard{{ID: "bk-1", Number: "BK-001", Customer: "Acme", Freight: 12000}}},
		{Status: "confirmed", Cards: []boardCard{}},
		{Status: "completed", Cards: []boardCard{}},
		{Status: "cancelled", Cards: []boardCard{}},
	}
	var buf strings.Builder
	require.NoError(t, tmpl.ExecuteTemplate(&buf, "bookings_board.html",
		buildTemplateData(PageData{Title: "Bookings Board", Extra: map[string]interface{}{"Columns": cols}})))
	out := buf.String()
	require.Contains(t, out, "BK-001", "card number must render")
	require.Contains(t, out, "Acme", "card customer must render")
	for _, lane := range []string{"pending", "confirmed", "completed", "cancelled"} {
		require.Contains(t, out, `data-status="`+lane+`"`, "lane %s must render", lane)
	}
}
