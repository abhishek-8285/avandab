package vahan

import (
	"context"
	"testing"
	"time"
)

func TestMockClient_FetchRC(t *testing.T) {
	rc, err := NewMock().FetchRC(context.Background(), "mh12ab3456")
	if err != nil {
		t.Fatal(err)
	}
	if rc.VehicleNumber != "MH12AB3456" {
		t.Errorf("number = %q, want normalized uppercase", rc.VehicleNumber)
	}
	if rc.FitnessUpto.Before(time.Now()) {
		t.Error("mock fitness must be future-dated")
	}
	if _, err := NewMock().FetchRC(context.Background(), "  "); err == nil {
		t.Error("empty number must error")
	}
	if err := context.Canceled; true {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := NewMock().FetchRC(ctx, "MH12"); err == nil {
			t.Error("canceled ctx must propagate")
		}
		_ = err
	}
}

func TestNewClient_DefaultsToMock(t *testing.T) {
	c := NewClient("")
	if _, ok := c.(*MockClient); !ok {
		t.Errorf("unknown provider must degrade to mock, got %T", c)
	}
	c = NewClient("http") // no licensed aggregator wired yet
	if _, ok := c.(*MockClient); !ok {
		t.Errorf("unlicensed provider must degrade to mock, got %T", c)
	}
}
