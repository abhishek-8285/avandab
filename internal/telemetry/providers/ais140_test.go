package providers

import (
	"testing"
)

func TestAIS140_StandardPacket(t *testing.T) {
	// Sample Indian AIS-140 Packet:
	// $PVT,1.0.0,SET,PVT,1,A,864209048123456,DL01AB1234,A,31082026,083000,1904.5620,N,07252.6620,E,48.5,120.0,12,50,1.2,0.9,AIRTEL,1,14.2,4.1,0*5A
	raw := "$PVT,1.0.0,SET,PVT,1,A,864209048123456,DL01AB1234,A,31082026,083000,1904.5620,N,07252.6620,E,48.5,120.0,12,50,1.2,0.9,AIRTEL,1,14.2,4.1,0*5A"

	frame, err := DecodeAIS140Packet(raw)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if frame.IMEI != "864209048123456" {
		t.Errorf("expected IMEI '864209048123456', got '%s'", frame.IMEI)
	}
	if frame.Speed != 48.5 {
		t.Errorf("expected speed 48.5, got %f", frame.Speed)
	}
	if frame.Latitude <= 18.0 || frame.Latitude >= 20.0 {
		t.Errorf("invalid converted latitude: %f", frame.Latitude)
	}
	if frame.Longitude <= 72.0 || frame.Longitude >= 74.0 {
		t.Errorf("invalid converted longitude: %f", frame.Longitude)
	}
	if frame.Ignition == nil || !*frame.Ignition {
		t.Errorf("expected ignition ON")
	}
	if frame.Provider != "ais140" {
		t.Errorf("expected provider 'ais140', got '%s'", frame.Provider)
	}
}

func TestAIS140_CompactPacket(t *testing.T) {
	// Compact AIS-140 Packet:
	// $PVT,864209048123456,31082026,083000,1831.2240,N,07351.3780,E,55.0,180.0,10,1,0*3B
	raw := "$PVT,864209048123456,31082026,083000,1831.2240,N,07351.3780,E,55.0,180.0,10,1,0*3B"

	frame, err := DecodeAIS140Packet(raw)
	if err != nil {
		t.Fatalf("compact decode failed: %v", err)
	}

	if frame.IMEI != "864209048123456" {
		t.Errorf("expected IMEI '864209048123456', got '%s'", frame.IMEI)
	}
	if frame.Speed != 55.0 {
		t.Errorf("expected speed 55.0, got %f", frame.Speed)
	}
	if frame.Heading != 180.0 {
		t.Errorf("expected heading 180.0, got %f", frame.Heading)
	}
}
