package integration

// C4 live-contract tests: each provider's live HTTP client is exercised
// against an httptest stand-in for the provider API (no creds needed).
// Asserts wire contract — path, X-API-Key header, success decode, and the
// `*_unavailable` error taxonomy on non-2xx — so flipping USE_MOCK=false
// with real creds exercises a proven path. A stub would make no HTTP call
// and fail the hit assertion.
import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"transport-app/internal/integration/ewaybill"
	"transport-app/internal/integration/fastag"
	"transport-app/internal/integration/gstn"
)

func contractServer(t *testing.T, hits *[]string, keys *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits = append(*hits, r.Method+" "+r.URL.RequestURI())
		*keys = append(*keys, r.Header.Get("X-API-Key"))
		if strings.Contains(r.URL.Path, "/fail") {
			http.Error(w, "provider down", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
}

func TestLiveContract_EWayBill(t *testing.T) {
	var hits, keys []string
	srv := contractServer(t, &hits, &keys)
	defer srv.Close()
	ctx := context.Background()

	c := ewaybill.NewClient(ewaybill.Config{Endpoint: srv.URL, APIKey: "k-ewb", Enabled: true, UseMock: false})
	if _, err := c.Generate(ctx, ewaybill.GenerateRequest{DocumentNumber: "INV-C4"}); err != nil {
		t.Fatalf("live generate: %v", err)
	}
	if len(hits) != 1 || hits[0] != "POST /generate" {
		t.Fatalf("hits = %v, want [POST /generate]", hits)
	}
	if len(keys) != 1 || keys[0] != "k-ewb" {
		t.Fatalf("api keys = %v, want [k-ewb]", keys)
	}

	cFail := ewaybill.NewClient(ewaybill.Config{Endpoint: srv.URL + "/fail", APIKey: "k", Enabled: true, UseMock: false})
	if _, err := cFail.Generate(ctx, ewaybill.GenerateRequest{}); err == nil || !strings.Contains(err.Error(), "ewaybill_unavailable") {
		t.Fatalf("non-2xx must surface ewaybill_unavailable, got %v", err)
	}
}

func TestLiveContract_FASTag(t *testing.T) {
	var hits, keys []string
	srv := contractServer(t, &hits, &keys)
	defer srv.Close()
	ctx := context.Background()

	c := fastag.NewClient(fastag.Config{Endpoint: srv.URL, APIKey: "k-tag", Enabled: true, UseMock: false})
	if _, err := c.GetBalance(ctx, "MH01C4", "TAG-C4"); err != nil {
		t.Fatalf("live balance: %v", err)
	}
	if len(hits) != 1 || !strings.HasPrefix(hits[0], "GET /balance?") {
		t.Fatalf("hits = %v, want [GET /balance?...]", hits)
	}
	if keys[0] != "k-tag" {
		t.Fatalf("api keys = %v, want [k-tag]", keys)
	}

	cFail := fastag.NewClient(fastag.Config{Endpoint: srv.URL + "/fail", APIKey: "k", Enabled: true, UseMock: false})
	if _, err := cFail.GetBalance(ctx, "V", "T"); err == nil || !strings.Contains(err.Error(), "fastag_unavailable") {
		t.Fatalf("non-2xx must surface fastag_unavailable, got %v", err)
	}
}

func TestLiveContract_GSTN(t *testing.T) {
	var hits, keys []string
	srv := contractServer(t, &hits, &keys)
	defer srv.Close()
	ctx := context.Background()

	c := gstn.NewClient(gstn.Config{Endpoint: srv.URL, APIKey: "k-gstn", Enabled: true, UseMock: false})
	if _, err := c.ValidateGSTIN(ctx, "27AAAAA0000A1Z5"); err != nil {
		t.Fatalf("live validate: %v", err)
	}
	if len(hits) != 1 || hits[0] != "POST /gstn/validate" {
		t.Fatalf("hits = %v, want [POST /gstn/validate]", hits)
	}
	if keys[0] != "k-gstn" {
		t.Fatalf("api keys = %v, want [k-gstn]", keys)
	}

	cFail := gstn.NewClient(gstn.Config{Endpoint: srv.URL + "/fail", APIKey: "k", Enabled: true, UseMock: false})
	if _, err := cFail.ValidateGSTIN(ctx, "X"); err == nil || !strings.Contains(err.Error(), "gstn") {
		t.Fatalf("non-2xx must surface gstn error, got %v", err)
	}
}
