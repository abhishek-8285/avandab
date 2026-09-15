package handlers

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebInterfaceGuidelines_FieldPartialAccessibility(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	tmpl, err := parseTemplates(&mockAuthSvc{})
	if err != nil {
		t.Fatalf("Failed to parse templates: %v", err)
	}

	var buf bytes.Buffer
	data := map[string]string{
		"Label": "License Number",
		"Name":  "license_number",
	}

	if err := tmpl.ExecuteTemplate(&buf, "field.html", data); err != nil {
		t.Fatalf("Failed to execute field.html: %v", err)
	}

	rendered := buf.String()

	// 1. Label 'for' attribute must match Name, not hardcoded 'Name'
	if !strings.Contains(rendered, `for="license_number"`) {
		t.Errorf("Expected field.html label to have for=\"license_number\", got: %s", rendered)
	}
	if !strings.Contains(rendered, `id="license_number"`) {
		t.Errorf("Expected field.html input to have id=\"license_number\", got: %s", rendered)
	}

	// 2. Focus visible must be used for keyboard accessibility
	if !strings.Contains(rendered, "focus-visible:ring") {
		t.Errorf("Expected field.html to include focus-visible:ring, got: %s", rendered)
	}
}

func TestWebInterfaceGuidelines_ButtonFocusAndMotion(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	tmpl, err := parseTemplates(&mockAuthSvc{})
	if err != nil {
		t.Fatalf("Failed to parse templates: %v", err)
	}

	var buf bytes.Buffer
	data := map[string]string{
		"Label": "Submit Order",
	}

	if err := tmpl.ExecuteTemplate(&buf, "btn.html", data); err != nil {
		t.Fatalf("Failed to execute btn.html: %v", err)
	}

	rendered := buf.String()

	// Must include focus-visible ring by default
	if !strings.Contains(rendered, "focus-visible:ring") {
		t.Errorf("Expected btn.html to include focus-visible:ring, got: %s", rendered)
	}

	// Must include motion-reduce:transform-none for accessibility
	if !strings.Contains(rendered, "motion-reduce:transform-none") {
		t.Errorf("Expected btn.html to include motion-reduce:transform-none, got: %s", rendered)
	}
}

func TestWebInterfaceGuidelines_TypographyEllipsis(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	templatesToCheck := []string{
		"internal/templates/assistant.html",
		"internal/templates/audit_logs_list.html",
		"internal/templates/invoice_list.html",
		"internal/templates/route_list.html",
		"internal/templates/route_optimize.html",
		"internal/templates/share_public.html",
		"internal/templates/telemetry_devices.html",
		"internal/templates/user_list.html",
	}

	for _, path := range templatesToCheck {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Failed to read %s: %v", path, err)
		}
		str := string(content)
		// Ensure none of the user-facing placeholders or status texts use raw triple dots
		badStrings := []string{
			"Thinking...",
			"Filter audit logs by action or table...",
			"Search invoice...",
			"Filter by source or destination city...",
			"Solving...",
			"Loading shipment milestones...",
			"Search IMEI, serial, vehicle...",
			"Search user or email...",
		}
		for _, bad := range badStrings {
			if strings.Contains(str, bad) {
				t.Errorf("File %s contains raw triple dots in string %q, must use proper ellipsis '…'", path, bad)
			}
		}
	}
}

func TestWebInterfaceGuidelines_ImageExplicitDimensions(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	checks := []struct {
		file           string
		requiredSubstr string
	}{
		{"internal/templates/customer_list_table.html", `width="28" height="28"`},
		{"internal/templates/customer_view.html", `width="56" height="56"`},
		{"internal/templates/invoice_pay.html", `width="56" height="56"`},
		{"internal/templates/kharcha_queue.html", `width="40" height="40"`},
		{"internal/templates/partials/irn_qr.html", `width="96" height="96"`},
		{"internal/templates/partials/ewaybill_card.html", `width="80" height="80"`},
		{"internal/templates/settings.html", `height="96"`},
	}

	for _, c := range checks {
		content, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatalf("Failed to read %s: %v", c.file, err)
		}
		if !strings.Contains(string(content), c.requiredSubstr) {
			t.Errorf("Expected %s to contain image dimension attribute %q to prevent CLS", c.file, c.requiredSubstr)
		}
	}
}
