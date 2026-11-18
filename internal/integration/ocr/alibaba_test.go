package ocr

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

func TestAmountFromLines_UsesLabelledTotal(t *testing.T) {
	lines := []recognizedLine{
		{text: "Fuel Qty 12.5", probability: 0.98},
		{text: "GST 12.00", probability: 0.97},
		{text: "Grand Total ₹1,234.50", probability: 0.93},
	}
	got, err := amountFromLines(lines)
	if err != nil {
		t.Fatal(err)
	}
	if got.Amount != 1234.5 || got.Confidence != 0.93 {
		t.Errorf("got %+v, want {1234.5 0.93}", got)
	}
}

func TestAmountFromLines_NoTotalFailsClosed(t *testing.T) {
	for _, lines := range [][]recognizedLine{
		{{text: "₹450.00", probability: 0.99}},
		{{text: "Total 450", probability: 0.99}}, // bare integer stays manual
		nil,
	} {
		if _, err := amountFromLines(lines); err == nil {
			t.Errorf("lines %v must not produce a verifying amount", lines)
		}
	}
}

func TestAmountFromLines_IgnoresQuantityAndBalanceLines(t *testing.T) {
	_, err := amountFromLines([]recognizedLine{
		{text: "Total Qty 12.50 L", probability: 0.99},
		{text: "Balance ₹500.00", probability: 0.99},
	})
	if err == nil {
		t.Error("quantity/balance lines must not be read as the bill total")
	}
}

func TestAmountFromLines_BadProbabilityFails(t *testing.T) {
	_, err := amountFromLines([]recognizedLine{{text: "Total ₹450.00", probability: 1.4}})
	if err == nil {
		t.Error("out-of-range probability must fail")
	}
}

func TestAlibabaClient_ExtractSurfacesErrors(t *testing.T) {
	sentinel := errors.New("boom")
	c := &alibabaClient{recognize: func(context.Context, string) ([]recognizedLine, error) {
		return nil, sentinel
	}}
	if _, err := c.Extract(context.Background(), "https://x/receipt.jpg"); !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want %v", err, sentinel)
	}
	if _, err := c.Extract(context.Background(), ""); err == nil {
		t.Error("empty image ref must fail")
	}
}

func TestNewClient_AlibabaConfig(t *testing.T) {
	if _, ok := NewClient(Config{Provider: "alibaba"}, nil).(*MockClient); !ok {
		t.Error("alibaba without credentials must fall back to mock")
	}
	c := NewClient(Config{
		Provider:               "alibaba",
		AlibabaRegionID:        "ap-southeast-1",
		AlibabaAccessKeyID:     "id",
		AlibabaAccessKeySecret: "secret",
	}, slog.Default())
	if _, ok := c.(*alibabaClient); !ok {
		t.Errorf("alibaba with credentials = %T, want *alibabaClient", c)
	}
}
