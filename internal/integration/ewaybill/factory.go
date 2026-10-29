package ewaybill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type realHttpClient struct {
	cfg        Config
	httpClient *http.Client
}

// NewClient returns a live HTTP client if !UseMock && APIKey != "", otherwise stub (Spec 21 §5).
func NewClient(cfg Config) Client {
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://ewaybill.nic.in/api"
	}
	if !cfg.UseMock && cfg.APIKey != "" {
		return &realHttpClient{cfg: cfg, httpClient: &http.Client{Timeout: 10 * time.Second}}
	}
	return &stubClient{cfg: cfg}
}

// IsMockClient reports whether c is the demo stub (nil counts as mock: callers
// that omitted a client get one). Background writers must use this to avoid
// persisting fabricated EWB numbers as government documents.
func IsMockClient(c Client) bool {
	if c == nil {
		return true
	}
	_, isStub := c.(*stubClient)
	return isStub
}

func (c *realHttpClient) Generate(ctx context.Context, req GenerateRequest) (EWayBill, error) {
	if !c.cfg.Enabled {
		return EWayBill{}, fmt.Errorf("ewaybill integration disabled")
	}
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint+"/generate", bytes.NewReader(body))
	if err != nil {
		return EWayBill{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-API-Key", c.cfg.APIKey)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: status %d", resp.StatusCode)
	}
	var out EWayBill
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: %w", err)
	}
	return out, nil
}

func (c *realHttpClient) Get(ctx context.Context, ewbNumber string) (EWayBill, error) {
	if !c.cfg.Enabled {
		return EWayBill{}, fmt.Errorf("ewaybill integration disabled")
	}
	url := fmt.Sprintf("%s/get/%s", c.cfg.Endpoint, ewbNumber)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return EWayBill{}, err
	}
	req.Header.Set("X-API-Key", c.cfg.APIKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: status %d", resp.StatusCode)
	}
	var out EWayBill
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: %w", err)
	}
	return out, nil
}

func (c *realHttpClient) Cancel(ctx context.Context, ewbNumber, reason string) (Cancellation, error) {
	if !c.cfg.Enabled {
		return Cancellation{}, fmt.Errorf("ewaybill integration disabled")
	}
	body, _ := json.Marshal(map[string]string{"ewb_number": ewbNumber, "reason": reason})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint+"/cancel", bytes.NewReader(body))
	if err != nil {
		return Cancellation{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.cfg.APIKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Cancellation{}, fmt.Errorf("ewaybill_unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Cancellation{}, fmt.Errorf("ewaybill_unavailable: status %d", resp.StatusCode)
	}
	var out Cancellation
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Cancellation{}, fmt.Errorf("ewaybill_unavailable: %w", err)
	}
	return out, nil
}

func (c *realHttpClient) GeneratePartA(ctx context.Context, req GenerateRequest) (EWayBill, error) {
	if !c.cfg.Enabled {
		return EWayBill{}, fmt.Errorf("ewaybill integration disabled")
	}
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint+"/part-a", bytes.NewReader(body))
	if err != nil {
		return EWayBill{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-API-Key", c.cfg.APIKey)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: status %d", resp.StatusCode)
	}
	var out EWayBill
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: %w", err)
	}
	return out, nil
}

func (c *realHttpClient) AttachPartB(ctx context.Context, ewbNumber, vehicleNumber, transporterID string) (EWayBill, error) {
	if !c.cfg.Enabled {
		return EWayBill{}, fmt.Errorf("ewaybill integration disabled")
	}
	body, _ := json.Marshal(map[string]string{"ewb_number": ewbNumber, "vehicle_number": vehicleNumber, "transporter_id": transporterID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint+"/part-b", bytes.NewReader(body))
	if err != nil {
		return EWayBill{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.cfg.APIKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: status %d", resp.StatusCode)
	}
	var out EWayBill
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: %w", err)
	}
	return out, nil
}

func (c *realHttpClient) Extend(ctx context.Context, ewbNumber string, req ExtendRequest) (EWayBill, error) {
	if !c.cfg.Enabled {
		return EWayBill{}, fmt.Errorf("ewaybill integration disabled")
	}
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/extend/%s", c.cfg.Endpoint, ewbNumber), bytes.NewReader(body))
	if err != nil {
		return EWayBill{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-API-Key", c.cfg.APIKey)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: status %d", resp.StatusCode)
	}
	var out EWayBill
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: %w", err)
	}
	return out, nil
}

func (c *realHttpClient) GetByNumber(ctx context.Context, ewbNumber string) (EWayBill, error) {
	return c.Get(ctx, ewbNumber)
}

func (c *realHttpClient) GetByTrip(ctx context.Context, tripID string) (EWayBill, error) {
	if !c.cfg.Enabled {
		return EWayBill{}, fmt.Errorf("ewaybill integration disabled")
	}
	url := fmt.Sprintf("%s/trip/%s", c.cfg.Endpoint, tripID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return EWayBill{}, err
	}
	req.Header.Set("X-API-Key", c.cfg.APIKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: status %d", resp.StatusCode)
	}
	var out EWayBill
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return EWayBill{}, fmt.Errorf("ewaybill_unavailable: %w", err)
	}
	return out, nil
}
