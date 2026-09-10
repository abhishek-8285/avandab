package gstn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStub_CancelIRN_MockMode_ReturnsAcknowledgement(t *testing.T) {
	c := NewClient(Config{Enabled: true, UseMock: true})
	req := CancelIRNRequest{
		IRN:          strings.Repeat("a", 64),
		CancelReason: 1,
		CancelRemark: "duplicate invoice",
	}
	res, err := c.CancelIRN(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, res.Cancelled)
	assert.Equal(t, req.IRN, res.IRN, "IRN must be echoed back")
	assert.Equal(t, "duplicate invoice", res.Remark, "remark must be echoed back")
	assert.NotEmpty(t, res.CancelNo, "stub must return a fake cancellation acknowledgement number")
	assert.NotEmpty(t, res.CancelDate)
}

func TestStub_CancelIRN_Disabled_ReturnsError(t *testing.T) {
	c := NewClient(Config{Enabled: false, UseMock: true})
	_, err := c.CancelIRN(context.Background(), CancelIRNRequest{IRN: "x", CancelReason: 2})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gstn integration disabled")
}

// Regression guard mirroring the fastag fabrication guards: without demo mode
// the stub must never fabricate cancellations.
func TestStub_CancelIRN_NoMock_DoesNotFabricate(t *testing.T) {
	c := NewClient(Config{Enabled: true, UseMock: false})
	_, err := c.CancelIRN(context.Background(), CancelIRNRequest{IRN: strings.Repeat("b", 64), CancelReason: 3})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials not configured")
}

func TestStub_CancelIRN_RejectsBadInput(t *testing.T) {
	c := NewClient(Config{Enabled: true, UseMock: true})

	_, err := c.CancelIRN(context.Background(), CancelIRNRequest{CancelReason: 1})
	require.Error(t, err, "empty IRN must be rejected")
	assert.Contains(t, err.Error(), "irn is required")

	_, err = c.CancelIRN(context.Background(), CancelIRNRequest{IRN: strings.Repeat("c", 64), CancelReason: 9})
	require.Error(t, err, "out-of-range cancel_reason must be rejected")
	assert.Contains(t, err.Error(), "cancel_reason")
}

func TestRealHttpClient_CancelIRN_WirePayload(t *testing.T) {
	var capturedPayload map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/einvoice/cancel", r.URL.Path)
		assert.Equal(t, "test-api-key", r.Header.Get("X-API-Key"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		_ = json.NewDecoder(r.Body).Decode(&capturedPayload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Irn":"1234567890123456789012345678901234567890123456789012345678901234","CancelDate":"2026-09-11 12:00:00"}`))
	}))
	defer srv.Close()

	client := NewClient(Config{
		Enabled:  true,
		UseMock:  false,
		Endpoint: srv.URL,
		APIKey:   "test-api-key",
	})
	res, err := client.CancelIRN(context.Background(), CancelIRNRequest{
		IRN:          "1234567890123456789012345678901234567890123456789012345678901234",
		CancelReason: 2,
		CancelRemark: "Order cancelled by shipper",
	})
	require.NoError(t, err)
	assert.True(t, res.Cancelled)
	assert.Equal(t, "1234567890123456789012345678901234567890123456789012345678901234", res.IRN)
	assert.Equal(t, "2026-09-11 12:00:00", res.CancelDate)
	assert.Equal(t, "Order cancelled by shipper", res.Remark)

	// Verify exact NIC/GSP wire field names
	assert.Equal(t, "1234567890123456789012345678901234567890123456789012345678901234", capturedPayload["Irn"])
	assert.Equal(t, "2", capturedPayload["CnlRsn"])
	assert.Equal(t, "Order cancelled by shipper", capturedPayload["CnlRem"])
}
