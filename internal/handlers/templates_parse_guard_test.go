package handlers

import "testing"

// Go parses the whole template set in one call, so a single syntax error in
// any template leaves the set partially populated and EVERY page then fails
// with a confusing secondary error (observed in the wild:
// "auth layout template not found" + HTTP 500 on /login).
//
// This guard turns that class of failure into an immediate, named test failure
// instead of a whole-site outage that only shows up at request time.
func TestParseTemplates_EntireSetParses(t *testing.T) {
	tmpl, err := parseTemplatesLang(nil, "en")
	if err != nil {
		t.Fatalf("template set failed to parse: %v", err)
	}
	if tmpl == nil {
		t.Fatal("parseTemplatesLang returned a nil template set")
	}

	// Spot-check that the layouts every page depends on actually made it in.
	for _, name := range []string{"layout.html", "auth_layout.html"} {
		if tmpl.Lookup(name) == nil {
			t.Errorf("expected template %q to be defined after parsing", name)
		}
	}
}
