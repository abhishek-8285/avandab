package service

import (
	"testing"

	"transport-app/internal/domain"
)

func TestTripIDFromPayload(t *testing.T) {
	if got, ok := tripIDFromPayload(domain.TripID("t-1")); !ok || got != "t-1" {
		t.Errorf("typed id: got %q,%v want t-1,true", got, ok)
	}
	if got, ok := tripIDFromPayload("t-2"); !ok || got != "t-2" {
		t.Errorf("string id (relay shape): got %q,%v want t-2,true", got, ok)
	}
	if _, ok := tripIDFromPayload(""); ok {
		t.Error("empty string must not parse")
	}
	if _, ok := tripIDFromPayload(42); ok {
		t.Error("non-string must not parse")
	}
}
