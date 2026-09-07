package openapispec

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// cleanYAML must convert yaml.v3's map[interface{}]interface{} into JSON-safe maps.
func TestCleanYAML(t *testing.T) {
	in := map[string]interface{}{
		"openapi": "3.0.0",
		"nested":  map[interface{}]interface{}{"key": "val", "list": []interface{}{"a"}},
	}
	got, ok := cleanYAML(in).(map[string]interface{})
	if !ok {
		t.Fatalf("cleanYAML returned %T, want map[string]interface{}", cleanYAML(in))
	}
	nested, ok := got["nested"].(map[string]interface{})
	if !ok || nested["key"] != "val" {
		t.Errorf("nested map not converted: %#v", got["nested"])
	}
	if _, err := json.Marshal(got); err != nil {
		t.Errorf("cleaned doc must be JSON-serializable: %v", err)
	}
}

// Handler must route /openapi.json and /openapi.yaml, 404 otherwise.
func TestHandler_RouteMatching(t *testing.T) {
	if specErr != nil {
		t.Skipf("openapi.yaml not loadable in test env: %v", specErr)
	}
	h := Handler()

	jsonRec := httptest.NewRecorder()
	h.ServeHTTP(jsonRec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if jsonRec.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d, want 200", jsonRec.Code)
	}
	if ct := jsonRec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("GET /openapi.json Content-Type = %q", ct)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(jsonRec.Body.Bytes(), &doc); err != nil {
		t.Errorf("GET /openapi.json body is not valid JSON: %v", err)
	}

	yamlRec := httptest.NewRecorder()
	h.ServeHTTP(yamlRec, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))
	if yamlRec.Code != http.StatusOK {
		t.Fatalf("GET /openapi.yaml = %d, want 200", yamlRec.Code)
	}
	if ct := yamlRec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/yaml") {
		t.Errorf("GET /openapi.yaml Content-Type = %q", ct)
	}

	missRec := httptest.NewRecorder()
	h.ServeHTTP(missRec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if missRec.Code != http.StatusNotFound {
		t.Errorf("GET /nope = %d, want 404", missRec.Code)
	}
}
