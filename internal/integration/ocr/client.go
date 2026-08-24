// Package ocr provides receipt amount extraction (Spec 22 §2.7, §6).
// Provider is selected by OCR_PROVIDER: mock (default, canned fixture),
// http (POST image ref to OCR_HTTP_URL), tesseract falls back to mock
// until a local binary integration is justified — no external calls
// unless explicitly configured.
package ocr

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Config holds the adapter settings read from env.
type Config struct {
	Provider string // mock | tesseract | http
	HTTPURL  string
	HTTPKey  string
}

// Result is one extraction outcome.
type Result struct {
	Amount     float64
	Confidence float64 // 0..1
}

// Client extracts an amount + confidence from a receipt reference
// (URL or storage path already on file server-side).
type Client interface {
	Extract(ctx context.Context, imageRef string) (Result, error)
}

// NewClient returns the configured provider; unknown/empty → mock.
func NewClient(cfg Config, logger *slog.Logger) Client {
	if logger == nil {
		logger = slog.Default()
	}
	switch strings.ToLower(cfg.Provider) {
	case "http":
		if cfg.HTTPURL == "" {
			logger.Warn("OCR_PROVIDER=http but OCR_HTTP_URL empty — falling back to mock")
			return NewMockClient()
		}
		return &httpClient{cfg: cfg, client: &http.Client{Timeout: 15 * time.Second}}
	case "tesseract":
		logger.Warn("tesseract provider not implemented yet — using mock (Spec 22 §11 open item)")
		return NewMockClient()
	default:
		return NewMockClient()
	}
}

// MockClient returns the spec's canned fixture: ₹3000.00 at 0.94 confidence.
type MockClient struct{}

func NewMockClient() *MockClient { return &MockClient{} }

func (m *MockClient) Extract(ctx context.Context, imageRef string) (Result, error) {
	if imageRef == "" {
		return Result{}, fmt.Errorf("no receipt on file")
	}
	return Result{Amount: 3000.00, Confidence: 0.94}, nil
}

// httpClient posts {image_ref} and expects {"amount":…,"confidence":…}.
type httpClient struct {
	cfg    Config
	client *http.Client
}

func (h *httpClient) Extract(ctx context.Context, imageRef string) (Result, error) {
	if imageRef == "" {
		return Result{}, fmt.Errorf("no receipt on file")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		h.cfg.HTTPURL, strings.NewReader(`{"image_ref":`+quote(imageRef)+`}`))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.cfg.HTTPKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.cfg.HTTPKey)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("ocr endpoint unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("ocr endpoint returned status %d", resp.StatusCode)
	}
	var out struct {
		Amount     float64 `json:"amount"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Result{}, fmt.Errorf("ocr endpoint bad JSON: %w", err)
	}
	if out.Confidence <= 0 || out.Confidence > 1 {
		return Result{}, fmt.Errorf("ocr confidence out of range: %f", out.Confidence)
	}
	return Result{Amount: out.Amount, Confidence: out.Confidence}, nil
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
