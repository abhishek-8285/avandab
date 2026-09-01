package providers

import (
	"encoding/hex"
	"testing"
)

func TestGT06_LoginPacket(t *testing.T) {
	// Sample GT06 Login Packet:
	// Start: 78 78
	// Length: 0D (13 bytes)
	// Protocol: 01 (Login)
	// IMEI (8B BCD): 08 64 20 90 48 12 34 56 -> IMEI "864209048123456"
	// Serial: 00 01
	// CRC: 84 08 (or calculated)
	// Stop: 0D 0A
	rawHex := "78780d010864209048123456000184080d0a"
	packet, err := hex.DecodeString(rawHex)
	if err != nil {
		t.Fatalf("hex decode failed: %v", err)
	}

	res, err := ParseGT06Packet(packet, "")
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if res.IMEI != "864209048123456" {
		t.Errorf("expected IMEI '864209048123456', got '%s'", res.IMEI)
	}

	ack := res.ACKResponse
	if len(ack) != 10 {
		t.Fatalf("expected 10-byte ACK, got %d bytes", len(ack))
	}
	if ack[0] != 0x78 || ack[1] != 0x78 || ack[3] != GT06PacketLogin {
		t.Errorf("invalid ACK header: %x", ack)
	}
}

func TestGT06_LocationPacket(t *testing.T) {
	// Sample GT06 GPS Location Packet (0x12)
	// 78 78 (Start)
	// 1F (Length = 31 bytes)
	// 12 (Protocol: GPS Location)
	// 1A 08 1F 08 1E 00 (DateTime: 2026-08-31 08:30:00 UTC)
	// CF (15 Satellites)
	// 02 71 89 00 (Latitude: ~19.0760)
	// 08 2A 9C 00 (Longitude: ~72.8777)
	// 30 (Speed: 48 km/h)
	// 14 00 (Course/Status: North, East, Valid)
	// 00 00 00 00 00 00 00 00 (LBS / MNC / MCC)
	// 00 02 (Serial Number)
	// CRC (2B)
	// 0D 0A (Stop)
	data := []byte{
		0x78, 0x78,
		0x16,                               // Length: 22 bytes
		0x12,                               // Location
		0x1A, 0x08, 0x1F, 0x08, 0x1E, 0x00, // 2026-08-31 08:30:00 UTC
		0xCF,                   // Satellites
		0x02, 0x0D, 0xAE, 0x60, // Lat: 34451040 / 1800000 = 19.139466
		0x07, 0xDB, 0xC4, 0x80, // Lng: 131843200 / 1800000 = 73.246222
		0x30,       // Speed: 48 km/h
		0x1C, 0x00, // Course: 0, North (0x04) + East (0x08) + Valid (0x10)
		0x00, 0x02, // Serial
		0x12, 0x34, // CRC
		0x0D, 0x0A, // Stop
	}

	frame, ack, err := DecodeGT06Packet(data, "864209048123456")
	if err != nil {
		t.Fatalf("location decode failed: %v", err)
	}

	if frame == nil {
		t.Fatal("expected location frame, got nil")
	}
	if frame.IMEI != "864209048123456" {
		t.Errorf("expected session IMEI, got '%s'", frame.IMEI)
	}
	if frame.Speed != 48.0 {
		t.Errorf("expected speed 48.0, got %f", frame.Speed)
	}
	if frame.Latitude <= 0 || frame.Longitude <= 0 {
		t.Errorf("invalid coordinates lat=%f, lng=%f", frame.Latitude, frame.Longitude)
	}
	if frame.Valid == nil || !*frame.Valid {
		t.Errorf("expected valid GPS fix")
	}
	if frame.DeviceTime.Year() != 2026 {
		t.Errorf("expected year 2026, got %d", frame.DeviceTime.Year())
	}
	if len(ack) != 10 {
		t.Errorf("expected 10-byte ACK, got %d", len(ack))
	}
}

func TestGT06_StatusHeartbeatPacket(t *testing.T) {
	// Status/Heartbeat Packet (0x13)
	data := []byte{
		0x78, 0x78,
		0x0A,       // Length
		0x13,       // Status
		0x02,       // Terminal info (Ignition = ON)
		0x05,       // Voltage level (5 = High battery ~83%)
		0x04,       // GSM Signal (4 = Strong)
		0x00, 0x00, // Status extension
		0x00, 0x03, // Serial
		0x56, 0x78, // CRC
		0x0D, 0x0A, // Stop
	}

	frame, ack, err := DecodeGT06Packet(data, "864209048123456")
	if err != nil {
		t.Fatalf("status decode failed: %v", err)
	}

	if frame == nil {
		t.Fatal("expected status frame, got nil")
	}
	if frame.Ignition == nil || !*frame.Ignition {
		t.Errorf("expected ignition ON")
	}
	if frame.BatteryLevel == nil || *frame.BatteryLevel < 80 {
		t.Errorf("expected battery > 80%%, got %v", frame.BatteryLevel)
	}
	if frame.GSMSignal == nil || *frame.GSMSignal != 4 {
		t.Errorf("expected GSM signal 4, got %v", frame.GSMSignal)
	}
	if len(ack) != 10 {
		t.Errorf("expected 10-byte ACK, got %d", len(ack))
	}
}

func TestGT06_CRC(t *testing.T) {
	data := []byte{0x05, 0x01, 0x00, 0x01}
	crc := CalculateGT06CRC(data)
	if crc == 0 {
		t.Errorf("expected non-zero CRC")
	}
}
