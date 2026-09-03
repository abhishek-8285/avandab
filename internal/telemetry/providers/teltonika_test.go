package providers

import (
	"encoding/hex"
	"testing"
	"time"
)

func TestTeltonika_IMEIHandshake(t *testing.T) {
	// 2 bytes length (0x000F = 15) + 15 ASCII bytes IMEI "860123456789012"
	handshake := BuildTeltonikaHandshake("860123456789012")

	res, err := ParseTeltonikaPacket(handshake, "")
	if err != nil {
		t.Fatalf("handshake parse failed: %v", err)
	}

	if !res.IsHandshake {
		t.Errorf("expected IsHandshake true")
	}
	if res.IMEI != "860123456789012" {
		t.Errorf("expected IMEI '860123456789012', got '%s'", res.IMEI)
	}
	if len(res.ACKResponse) != 1 || res.ACKResponse[0] != 0x01 {
		t.Errorf("expected 0x01 ACK response, got %x", res.ACKResponse)
	}

	// Test DecodeTeltonikaPacket with handshake packet
	frame, err := DecodeTeltonikaPacket(handshake)
	if err != nil {
		t.Fatalf("DecodeTeltonikaPacket handshake failed: %v", err)
	}
	if frame.IMEI != "860123456789012" {
		t.Errorf("expected frame IMEI '860123456789012', got '%s'", frame.IMEI)
	}
}

func TestTeltonika_Codec8Packet(t *testing.T) {
	now := time.Date(2026, 9, 2, 8, 30, 0, 0, time.UTC)
	record := TeltonikaDataRecord{
		Timestamp:  now,
		Priority:   1,
		Longitude:  77.2090, // Delhi (77.2090° E)
		Latitude:   28.6139, // Delhi (28.6139° N)
		Altitude:   216,
		Angle:      180,
		Satellites: 12,
		Speed:      65,
		EventIOID:  1,
		IOItems: map[uint16]uint64{
			TeltonikaIOIgnition:        1,        // Ignition ON
			TeltonikaIOMovement:        1,        // Moving
			TeltonikaIOExternalVoltage: 12450,    // 12.45 V
			TeltonikaIOBatteryLevel:    95,       // 95%
			TeltonikaIOTotalOdometer:   12540000, // 12540 km (12540000 m)
			TeltonikaIOGSMSignal:       4,        // 4/5
			TeltonikaIOFuelLevelPct:    78,       // 78%
		},
	}

	packetBytes := BuildTeltonikaCodec8Packet([]TeltonikaDataRecord{record})

	res, err := ParseTeltonikaPacket(packetBytes, "860123456789012")
	if err != nil {
		t.Fatalf("ParseTeltonikaPacket failed: %v", err)
	}

	if res.CodecID != CodecIDCodec8 {
		t.Errorf("expected Codec 8 (0x08), got 0x%02X", res.CodecID)
	}
	if res.RecordCount != 1 {
		t.Errorf("expected record count 1, got %d", res.RecordCount)
	}
	if len(res.ACKResponse) != 4 || res.ACKResponse[3] != 0x01 {
		t.Errorf("expected ACK [0x00,0x00,0x00,0x01], got %x", res.ACKResponse)
	}

	if len(res.Frames) != 1 {
		t.Fatalf("expected 1 decoded frame, got %d", len(res.Frames))
	}

	f := res.Frames[0]
	if f.IMEI != "860123456789012" {
		t.Errorf("expected IMEI '860123456789012', got '%s'", f.IMEI)
	}
	if f.DeviceTime.UnixMilli() != now.UnixMilli() {
		t.Errorf("expected timestamp %v, got %v", now, f.DeviceTime)
	}
	if f.Latitude < 28.6138 || f.Latitude > 28.6140 {
		t.Errorf("expected lat ~28.6139, got %f", f.Latitude)
	}
	if f.Longitude < 77.2089 || f.Longitude > 77.2091 {
		t.Errorf("expected lng ~77.2090, got %f", f.Longitude)
	}
	if f.Speed != 65 {
		t.Errorf("expected speed 65 km/h, got %f", f.Speed)
	}
	if f.Heading != 180 {
		t.Errorf("expected heading 180, got %f", f.Heading)
	}
	if f.Satellites == nil || *f.Satellites != 12 {
		t.Errorf("expected 12 satellites, got %v", f.Satellites)
	}
	if f.Ignition == nil || !*f.Ignition {
		t.Errorf("expected ignition ON, got %v", f.Ignition)
	}
	if f.Motion == nil || !*f.Motion {
		t.Errorf("expected motion true, got %v", f.Motion)
	}
	if f.BatteryLevel == nil || *f.BatteryLevel != 95 {
		t.Errorf("expected battery 95%%, got %v", f.BatteryLevel)
	}
	if f.ExternalVoltage == nil || *f.ExternalVoltage != 12.45 {
		t.Errorf("expected external voltage 12.45 V, got %v", f.ExternalVoltage)
	}
	if f.Odometer == nil || *f.Odometer != 12540.0 {
		t.Errorf("expected odometer 12540.0 km, got %v", f.Odometer)
	}
	if f.GSMSignal == nil || *f.GSMSignal != 4 {
		t.Errorf("expected GSM signal 4, got %v", f.GSMSignal)
	}
	if f.FuelLevel == nil || *f.FuelLevel != 78 {
		t.Errorf("expected fuel level 78%%, got %v", f.FuelLevel)
	}
	if f.Provider != "teltonika" {
		t.Errorf("expected provider 'teltonika', got '%s'", f.Provider)
	}
}

func TestTeltonika_MultipleRecordsAndSOS(t *testing.T) {
	now := time.Now().UTC()
	rec1 := TeltonikaDataRecord{
		Timestamp:  now.Add(-10 * time.Second),
		Priority:   0,
		Longitude:  77.1000,
		Latitude:   28.5000,
		Speed:      40,
		Satellites: 10,
		IOItems: map[uint16]uint64{
			TeltonikaIOIgnition: 1,
		},
	}
	rec2 := TeltonikaDataRecord{
		Timestamp:  now,
		Priority:   2, // Panic priority
		Longitude:  77.1050,
		Latitude:   28.5050,
		Speed:      42,
		Satellites: 11,
		EventIOID:  TeltonikaIOSOS,
		IOItems: map[uint16]uint64{
			TeltonikaIOIgnition: 1,
			TeltonikaIOSOS:      1,
		},
	}

	packetBytes := BuildTeltonikaCodec8Packet([]TeltonikaDataRecord{rec1, rec2})

	res, err := ParseTeltonikaPacket(packetBytes, "358123456789012")
	if err != nil {
		t.Fatalf("multiple records parse failed: %v", err)
	}

	if res.RecordCount != 2 {
		t.Errorf("expected 2 records, got %d", res.RecordCount)
	}
	if len(res.Frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(res.Frames))
	}
	if len(res.ACKResponse) != 4 || res.ACKResponse[3] != 0x02 {
		t.Errorf("expected ACK [0x00,0x00,0x00,0x02], got %x", res.ACKResponse)
	}

	if res.Frames[0].SOS {
		t.Errorf("expected rec1 SOS false")
	}
	if !res.Frames[1].SOS {
		t.Errorf("expected rec2 SOS true")
	}
}

func TestTeltonika_Codec8Extended(t *testing.T) {
	// Test binary parsing of Codec 8 Extended (0x8E)
	// Preamble: 00 00 00 00
	// Data length: 4 bytes
	// Codec: 8E
	// Record count: 1
	// Record 1:
	//   Timestamp: 8B
	//   Priority: 1B
	//   GPS: 15B (lon 4B, lat 4B, alt 2B, angle 2B, sat 1B, speed 2B)
	//   IO: EventID 2B (00 01), TotalIO 2B (00 02)
	//       1B IO Count 2B (00 01) -> ID 2B (00 EF = 239 Ignition), Val 1B (01)
	//       2B IO Count 2B (00 01) -> ID 2B (00 42 = 66 ExtVoltage), Val 2B (2E E0 = 12000 mV)
	//       4B IO Count 2B (00 00)
	//       8B IO Count 2B (00 00)
	//       XB IO Count 2B (00 00)
	// Number of Data 2: 1B (01)
	// CRC: 4B
	hexData := "00000000" + // Preamble (4B)
		"00000030" + // Length = 48 bytes (4B)
		"8e" + // Codec ID 0x8E (1B)
		"01" + // Record count (1B)
		"0000018f4a3e7400" + // Timestamp (8B)
		"01" + // Priority (1B)
		"2E0C4940" + // Longitude: 77.2555072 (4B)
		"11100C40" + // Latitude: 28.6264384 (4B)
		"00D8" + // Altitude: 216m (2B)
		"00B4" + // Angle: 180 (2B)
		"0C" + // Satellites: 12 (1B)
		"0037" + // Speed: 55 km/h (2B)
		"0001" + // Event IO ID: 1 (2B)
		"0002" + // Total IO: 2 (2B)
		"0001" + "00EF" + "01" + // 1B IO count=1, ID=239 (Ignition), Val=1 (5B)
		"0001" + "0042" + "2EE0" + // 2B IO count=1, ID=66 (ExtVoltage), Val=12000 mV (6B)
		"0000" + // 4B IO count=0 (2B)
		"0000" + // 8B IO count=0 (2B)
		"0000" + // XB IO count=0 (2B)
		"01" + // Record count 2 (1B)
		"00001234" // CRC (4B)

	data, err := hex.DecodeString(hexData)
	if err != nil {
		t.Fatalf("hex decode failed: %v", err)
	}

	res, err := ParseTeltonikaPacket(data, "860123456789012")
	if err != nil {
		t.Fatalf("ParseTeltonikaPacket Extended failed: %v", err)
	}

	if res.CodecID != CodecIDCodec8Extended {
		t.Errorf("expected Codec 0x8E, got 0x%02X", res.CodecID)
	}
	if len(res.Frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(res.Frames))
	}

	f := res.Frames[0]
	if f.Ignition == nil || !*f.Ignition {
		t.Errorf("expected ignition ON")
	}
	if f.ExternalVoltage == nil || *f.ExternalVoltage != 12.0 {
		t.Errorf("expected external voltage 12.0V, got %v", f.ExternalVoltage)
	}
	if f.Speed != 55.0 {
		t.Errorf("expected speed 55.0, got %f", f.Speed)
	}
}

func TestTeltonika_ProviderRegistration(t *testing.T) {
	provider, ok := Get("teltonika")
	if !ok || provider == nil {
		t.Fatalf("expected 'teltonika' provider to be registered in registry")
	}
	if provider.Name() != "teltonika" {
		t.Errorf("expected provider name 'teltonika', got '%s'", provider.Name())
	}
}
