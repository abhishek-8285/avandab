package workflow_test

import (
	"context"
	"testing"

	"transport-app/internal/invoice/application"
	"transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/invoice/domain/workflow"
)

func TestInvoiceWorkflow_CanPay(t *testing.T) {
	wf := workflow.NewInvoiceWorkflow(nil, nil, nil, nil)

	tests := []struct {
		status   aggregate.PaymentStatus
		expected bool
	}{
		{aggregate.PaymentStatusPending, true},
		{aggregate.PaymentStatusPartiallyPaid, true},
		{aggregate.PaymentStatusPaid, false},
		{aggregate.PaymentStatus("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			got := wf.CanPay(tt.status)
			if got != tt.expected {
				t.Fatalf("CanPay(%s) = %v, expected %v", tt.status, got, tt.expected)
			}
		})
	}
}

func TestInvoiceWorkflow_GenerateInvoice_Validation(t *testing.T) {
	wf := workflow.NewInvoiceWorkflow(nil, nil, nil, nil)
	ctx := context.Background()

	_, err := wf.GenerateInvoice(ctx, application.GenerateInvoiceCommand{
		BookingID:  "",
		CustomerID: "cust-1",
	})
	if err == nil || err.Error() != "booking ID is required" {
		t.Fatalf("expected 'booking ID is required', got: %v", err)
	}

	_, err = wf.GenerateInvoice(ctx, application.GenerateInvoiceCommand{
		BookingID:  "bk-1",
		CustomerID: "",
	})
	if err == nil || err.Error() != "customer ID is required" {
		t.Fatalf("expected 'customer ID is required', got: %v", err)
	}
}
