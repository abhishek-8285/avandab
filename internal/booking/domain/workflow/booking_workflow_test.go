package workflow_test

import (
	"testing"

	"transport-app/internal/booking/domain/aggregate"
	"transport-app/internal/booking/domain/workflow"
)

func TestBookingWorkflow_CanComplete(t *testing.T) {
	wf := workflow.NewBookingWorkflow(nil, nil, nil)

	tests := []struct {
		status   aggregate.BookingStatus
		expected bool
	}{
		{aggregate.BookingConfirmed, true},
		{aggregate.BookingDraft, false},
		{aggregate.BookingCompleted, false},
		{aggregate.BookingCancelled, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			got := wf.CanComplete(tt.status)
			if got != tt.expected {
				t.Fatalf("CanComplete(%s) = %v, expected %v", tt.status, got, tt.expected)
			}
		})
	}
}

func TestBookingWorkflow_CanCancel(t *testing.T) {
	wf := workflow.NewBookingWorkflow(nil, nil, nil)

	tests := []struct {
		status   aggregate.BookingStatus
		expected bool
	}{
		{aggregate.BookingDraft, true},
		{aggregate.BookingConfirmed, true},
		{aggregate.BookingCancelled, true},
		{aggregate.BookingCompleted, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			got := wf.CanCancel(tt.status)
			if got != tt.expected {
				t.Fatalf("CanCancel(%s) = %v, expected %v", tt.status, got, tt.expected)
			}
		})
	}
}
