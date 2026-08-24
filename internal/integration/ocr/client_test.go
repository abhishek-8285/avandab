package ocr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestMockClient_CannedFixture — Spec 22 §7 S8: the mock returns the
// documented canned amount + confidence.
func TestMockClient_CannedFixture(t *testing.T) {
	m := NewMockClient()
	r, err := m.Extract(context.Background(), "/storage/receipts/r1.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if r.Amount != 3000.00 || r.Confidence != 0.94 {
		t.Errorf("mock = %+v, want 3000.00 @ 0.94", r)
	}
	if _, err := m.Extract(context.Background(), ""); err == nil {
		t.Error("empty receipt ref must error")
	}
}

func TestNewClient_DefaultsToMock(t *testing.T) {
	c := NewClient(Config{}, nil)
	if _, ok := c.(*MockClient); !ok {
		t.Errorf("unknown provider must fall back to mock, got %T", c)
	}
	c = NewClient(Config{Provider: "http"}, nil) // no URL → mock fallback
	if _, ok := c.(*MockClient); !ok {
		t.Errorf("http provider without URL must fall back to mock, got %T", c)
	}
}

func TestHTTPClient_DecodesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		w.Write([]byte(`{"amount":1234.5,"confidence":0.81}`))
	}))
	defer srv.Close()

	c := NewClient(Config{Provider: "http", HTTPURL: srv.URL}, nil)
	r, err := c.Extract(context.Background(), "ref-1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Amount != 1234.5 || r.Confidence != 0.81 {
		t.Errorf("got %+v", r)
	}
}

func TestHTTPClient_Non2xxErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewClient(Config{Provider: "http", HTTPURL: srv.URL}, nil)
	if _, err := c.Extract(context.Background(), "ref-1"); err == nil {
		t.Error("500 must surface as error so the expense stays manual")
	}
}
