package test

import (
	"context"
	"fmt"
	"time"

	intEWB "transport-app/internal/integration/ewaybill"
)

// fakeEWBProvider is a non-stub EWB client for tests that need
// auto-generation. The stub is deliberately rejected by the service now
// (mock providers must not fabricate government document numbers).
type fakeEWBProvider struct {
	seq int
}

func (f *fakeEWBProvider) number() string {
	f.seq++
	return fmt.Sprintf("EWB-TEST-%04d", f.seq)
}

func (f *fakeEWBProvider) bill(number string) intEWB.EWayBill {
	now := time.Now()
	return intEWB.EWayBill{
		EwbNumber:   number,
		Status:      "ACTIVE",
		GeneratedAt: now,
		ValidUpto:   now.Add(24 * time.Hour),
		QRCode:      "data:image/png;base64,dGVzdA==",
	}
}

func (f *fakeEWBProvider) Generate(ctx context.Context, req intEWB.GenerateRequest) (intEWB.EWayBill, error) {
	return f.bill(f.number()), nil
}

func (f *fakeEWBProvider) Get(ctx context.Context, ewbNumber string) (intEWB.EWayBill, error) {
	return f.bill(ewbNumber), nil
}

func (f *fakeEWBProvider) Cancel(ctx context.Context, ewbNumber, reason string) (intEWB.Cancellation, error) {
	return intEWB.Cancellation{
		EwbNumber:   ewbNumber,
		CancelledAt: time.Now(),
		Reason:      reason,
		Status:      "CANCELLED",
	}, nil
}

func (f *fakeEWBProvider) GeneratePartA(ctx context.Context, req intEWB.GenerateRequest) (intEWB.EWayBill, error) {
	return f.bill(f.number()), nil
}

func (f *fakeEWBProvider) AttachPartB(ctx context.Context, ewbNumber, vehicleNumber, transporterID string) (intEWB.EWayBill, error) {
	return f.bill(ewbNumber), nil
}

func (f *fakeEWBProvider) Extend(ctx context.Context, ewbNumber string, req intEWB.ExtendRequest) (intEWB.EWayBill, error) {
	b := f.bill(ewbNumber)
	b.ValidUpto = b.ValidUpto.Add(24 * time.Hour)
	return b, nil
}

func (f *fakeEWBProvider) GetByNumber(ctx context.Context, ewbNumber string) (intEWB.EWayBill, error) {
	return f.bill(ewbNumber), nil
}

func (f *fakeEWBProvider) GetByTrip(ctx context.Context, tripID string) (intEWB.EWayBill, error) {
	return f.bill(f.number()), nil
}
