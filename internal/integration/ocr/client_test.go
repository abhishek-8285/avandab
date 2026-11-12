package ocr

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMockClient_NoExtraction — the mock has no receipt text, so it must
// fail rather than return an amount that could rubber-stamp a claim.
func TestMockClient_NoExtraction(t *testing.T) {
	m := NewMockClient()
	for _, ref := range []string{"/storage/receipts/r1.jpg", ""} {
		r, err := m.Extract(context.Background(), ref)
		if !errors.Is(err, ErrMockNoExtraction) {
			t.Errorf("ref=%q err = %v, want ErrMockNoExtraction", ref, err)
		}
		if r.Amount != 0 || r.Confidence != 0 {
			t.Errorf("ref=%q mock = %+v, want zero result", ref, r)
		}
	}
}

func TestNewClient_WarnsMockCannotVerify(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	NewClient(Config{}, logger)
	if !strings.Contains(buf.String(), "cannot verify") {
		t.Errorf("mock selection must warn it cannot verify, log = %q", buf.String())
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
