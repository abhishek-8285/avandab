package types_test

import (
	"testing"

	"transport-app/internal/domain/types"
)

func TestTypedIDs_String(t *testing.T) {
	if types.UserID("usr-1").String() != "usr-1" {
		t.Fail()
	}
	if types.DriverID("drv-1").String() != "drv-1" {
		t.Fail()
	}
	if types.VehicleID("veh-1").String() != "veh-1" {
		t.Fail()
	}
	if types.CustomerID("cust-1").String() != "cust-1" {
		t.Fail()
	}
	if types.RouteID("rt-1").String() != "rt-1" {
		t.Fail()
	}
	if types.BookingID("bk-1").String() != "bk-1" {
		t.Fail()
	}
	if types.TripID("tr-1").String() != "tr-1" {
		t.Fail()
	}
	if types.DispatchID("dsp-1").String() != "dsp-1" {
		t.Fail()
	}
	if types.InvoiceID("inv-1").String() != "inv-1" {
		t.Fail()
	}
	if types.PaymentID("pay-1").String() != "pay-1" {
		t.Fail()
	}
	if types.FileID("f-1").String() != "f-1" {
		t.Fail()
	}
	if types.SessionID("s-1").String() != "s-1" {
		t.Fail()
	}
}
