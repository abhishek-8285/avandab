package providers

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"
)

// GT06 Protocol packet type constants.
const (
	GT06PacketLogin     = 0x01
	GT06PacketLocation  = 0x12
	GT06PacketStatus    = 0x13
	GT06PacketString    = 0x15
	GT06PacketAlarm     = 0x16
	GT06PacketGPSLBS1   = 0x19
	GT06PacketGPSLBS2   = 0x22
	GT06PacketLBSStatus = 0x27
)

// GT06Provider implements TelematicsProvider for GT06/Concox binary devices.
type GT06Provider struct{}

// NewGT06Provider constructs a GT06Provider.
func NewGT06Provider() *GT06Provider {
	return &GT06Provider{}
}

func (p *GT06Provider) Name() string {
	return "gt06"
}

func (p *GT06Provider) VerifySignature(rawBody []byte, header http.Header) error {
	return nil
}

func (p *GT06Provider) HandleWebhook(ctx context.Context, rawBody []byte) ([]RawFrame, error) {
	frame, _, err := DecodeGT06Packet(rawBody, "")
	if err != nil {
		return nil, err
	}
	if frame == nil {
		return nil, nil
	}
	return []RawFrame{*frame}, nil
}

func (p *GT06Provider) Poll(ctx context.Context, since time.Time) ([]RawFrame, error) {
	return nil, nil
}

// GT06DecodedResult holds the decoded frame, protocol type, serial number, and ACK response.
type GT06DecodedResult struct {
	Frame        *RawFrame
	ProtocolType byte
	SerialNumber uint16
	ACKResponse  []byte
	IMEI         string
}

// DecodeGT06Packet decodes raw GT06 binary bytes into a RawFrame and generates the required ACK packet.
func DecodeGT06Packet(data []byte, sessionIMEI string) (*RawFrame, []byte, error) {
	res, err := ParseGT06Packet(data, sessionIMEI)
	if err != nil {
		return nil, nil, err
	}
	return res.Frame, res.ACKResponse, nil
}

// ParseGT06Packet parses a GT06 packet and returns detailed protocol results.
func ParseGT06Packet(data []byte, sessionIMEI string) (*GT06DecodedResult, error) {
	if len(data) < 5 {
		return nil, errors.New("gt06: packet too short")
	}

	// Check Start Bytes: 0x78 0x78 (Standard) or 0x79 0x79 (Extended)
	isExtended := false
	if data[0] == 0x78 && data[1] == 0x78 {
		isExtended = false
	} else if data[0] == 0x79 && data[1] == 0x79 {
		isExtended = true
	} else {
		return nil, fmt.Errorf("gt06: invalid start bytes 0x%02X 0x%02X", data[0], data[1])
	}

	offset := 2
	var packetLength int
	if isExtended {
		if len(data) < 6 {
			return nil, errors.New("gt06: extended packet too short")
		}
		packetLength = int(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2
	} else {
		packetLength = int(data[offset])
		offset++
	}

	if len(data) < offset+packetLength+2 {
		return nil, fmt.Errorf("gt06: incomplete packet, expected length %d, got %d", offset+packetLength+2, len(data))
	}

	protocolType := data[offset]
	offset++

	res := &GT06DecodedResult{
		ProtocolType: protocolType,
		IMEI:         sessionIMEI,
	}

	switch protocolType {
	case GT06PacketLogin:
		// Login Packet: Contains 8-byte BCD IMEI (15 or 16 digits) + 2-byte serial + 2-byte CRC
		if offset+8 > len(data) {
			return nil, errors.New("gt06: login packet too short for IMEI")
		}
		imeiBytes := data[offset : offset+8]
		offset += 8
		imei := decodeBCDIMEI(imeiBytes)
		res.IMEI = imei

		// Serial number (2 bytes)
		var serial uint16
		if offset+2 <= len(data) {
			serial = binary.BigEndian.Uint16(data[offset : offset+2])
		}
		res.SerialNumber = serial
		res.ACKResponse = buildGT06ACK(protocolType, serial)
		res.Frame = nil
		return res, nil

	case GT06PacketLocation, GT06PacketAlarm, GT06PacketGPSLBS1, GT06PacketGPSLBS2:
		// Location Packet (0x12 / 0x16 / 0x22)
		frame, serial, err := parseLocationData(data[offset:], protocolType, sessionIMEI)
		if err != nil {
			return nil, err
		}
		res.Frame = frame
		res.SerialNumber = serial

		// Alarms & Location require ACK so the device stops repeating the SOS/alarm
		if protocolType == GT06PacketAlarm || protocolType == GT06PacketLocation {
			res.ACKResponse = buildGT06ACK(protocolType, serial)
		}
		return res, nil

	case GT06PacketStatus:
		// Heartbeat Status Packet: Terminal info (1B) + Voltage (1B) + GSM (1B) + Status (2B) + Serial (2B)
		st, err := parseStatusData(data[offset:])
		if err != nil {
			return nil, err
		}
		res.SerialNumber = st.Serial
		res.ACKResponse = buildGT06ACK(protocolType, st.Serial)

		now := time.Now().UTC()
		motion := false
		if st.Ignition != nil && *st.Ignition {
			motion = true
		}
		res.Frame = &RawFrame{
			IMEI:            sessionIMEI,
			DeviceTime:      now,
			BatteryLevel:    st.BatteryPct,
			ExternalVoltage: st.ExtVoltage,
			GSMSignal:       st.GSMSignal,
			Ignition:        st.Ignition,
			Motion:          &motion,
			Provider:        "gt06",
		}
		return res, nil

	default:
		// Generic ACK for unhandled protocol types with serial numbers
		if len(data) >= offset+2 {
			serial := binary.BigEndian.Uint16(data[len(data)-4 : len(data)-2])
			res.SerialNumber = serial
			res.ACKResponse = buildGT06ACK(protocolType, serial)
		}
		return res, nil
	}
}

// parseLocationData parses standard GT06 0x12 / 0x16 / 0x22 coordinates payload.
func parseLocationData(payload []byte, protocolType byte, imei string) (*RawFrame, uint16, error) {
	if len(payload) < 18 {
		return nil, 0, errors.New("gt06: location payload too short")
	}

	// 1. Date Time: 6 bytes (YY MM DD HH MM SS) in UTC
	year := 2000 + int(payload[0])
	month := time.Month(payload[1])
	day := int(payload[2])
	hour := int(payload[3])
	min := int(payload[4])
	sec := int(payload[5])
	deviceTime := time.Date(year, month, day, hour, min, sec, 0, time.UTC)

	// 2. GPS Length & Satellites (1 byte)
	satByte := payload[6]
	satellites := int(satByte & 0x0F)

	// 3. Latitude (4 bytes): (Deg * 60 * 30000)
	rawLat := binary.BigEndian.Uint32(payload[7:11])
	lat := float64(rawLat) / 1800000.0 // (raw / 30000) / 60

	// 4. Longitude (4 bytes): (Deg * 60 * 30000)
	rawLng := binary.BigEndian.Uint32(payload[11:15])
	lng := float64(rawLng) / 1800000.0

	// 5. Speed (1 byte): in km/h
	speed := float64(payload[15])

	// Course & Status (2 bytes)
	courseStatus := binary.BigEndian.Uint16(payload[16:18])
	heading := float64(courseStatus & 0x03FF) // Heading 0-360 deg

	// GT06 Status Bits:
	// Bit 10 (0x0400): 1 = North Latitude, 0 = South Latitude
	// Bit 11 (0x0800): 1 = East Longitude, 0 = West Longitude
	// Bit 12 (0x1000): 1 = GPS Fix Valid, 0 = Invalid
	isNorth := (courseStatus & 0x0400) != 0
	isEast := (courseStatus & 0x0800) != 0
	isValid := (courseStatus & 0x1000) != 0

	if !isNorth {
		lat = -lat
	}
	if !isEast {
		lng = -lng
	}

	// Clamp invalid zeroes/NaN
	if math.IsNaN(lat) || math.Abs(lat) > 90 {
		lat = 0
	}
	if math.IsNaN(lng) || math.Abs(lng) > 180 {
		lng = 0
	}

	motion := speed > 2.0
	var ignition *bool
	if speed > 0 {
		ign := true
		ignition = &ign
	}

	isSOS := (protocolType == GT06PacketAlarm)

	// Serial Number (last 4 bytes before stop: 2 bytes serial + 2 bytes CRC)
	var serial uint16
	if len(payload) >= 22 {
		serial = binary.BigEndian.Uint16(payload[len(payload)-4 : len(payload)-2])
	}

	frame := &RawFrame{
		IMEI:       imei,
		DeviceTime: deviceTime,
		Latitude:   lat,
		Longitude:  lng,
		Speed:      speed,
		Heading:    heading,
		Satellites: &satellites,
		Valid:      &isValid,
		Motion:     &motion,
		Ignition:   ignition,
		SOS:        isSOS,
		Provider:   "gt06",
	}

	return frame, serial, nil
}

type gt06Status struct {
	Serial     uint16
	BatteryPct *float64
	ExtVoltage *float64
	GSMSignal  *int
	Ignition   *bool
}

// parseStatusData parses 0x13 heartbeat status payload.
func parseStatusData(payload []byte) (gt06Status, error) {
	if len(payload) < 5 {
		return gt06Status{}, errors.New("gt06: status payload too short")
	}

	terminalInfo := payload[0]
	// Terminal info bit 1: Ignition (0 = OFF, 1 = ON)
	ign := (terminalInfo & 0x02) != 0

	// Voltage level (1 byte): 0-6 (0=No power, 1=Extremely low, 2=Very low, 3=Low, 4=Medium, 5=High, 6=Full)
	voltageLevel := payload[1]
	batteryPct := float64(voltageLevel) * (100.0 / 6.0)
	if batteryPct > 100 {
		batteryPct = 100
	}

	// GSM signal (1 byte): 0-4 (0=No signal, 4=Strongest)
	gsmSignal := int(payload[2])

	// Serial number
	var serial uint16
	if len(payload) >= 7 {
		serial = binary.BigEndian.Uint16(payload[len(payload)-4 : len(payload)-2])
	}

	return gt06Status{
		Serial:     serial,
		BatteryPct: &batteryPct,
		ExtVoltage: nil,
		GSMSignal:  &gsmSignal,
		Ignition:   &ign,
	}, nil
}

// decodeBCDIMEI extracts standard 15-digit IMEI from 8-byte BCD array.
func decodeBCDIMEI(b []byte) string {
	hexStr := hex.EncodeToString(b)
	// BCD IMEI usually starts with a leading '0' or contains 15 digits
	if len(hexStr) == 16 && hexStr[0] == '0' {
		return hexStr[1:]
	}
	if len(hexStr) > 15 {
		return hexStr[:15]
	}
	return hexStr
}

// buildGT06ACK constructs the standard 10-byte response packet expected by GT06 devices:
// 0x78 0x78 (Start) + 0x05 (Length) + ProtocolType + Serial(2B) + CRC(2B) + 0x0D 0x0A (Stop)
func buildGT06ACK(protocolType byte, serial uint16) []byte {
	ack := make([]byte, 10)
	ack[0] = 0x78
	ack[1] = 0x78
	ack[2] = 0x05 // Length
	ack[3] = protocolType
	binary.BigEndian.PutUint16(ack[4:6], serial)

	// Calculate CRC16 ITU over Length + ProtocolType + Serial
	crc := CalculateGT06CRC(ack[2:6])
	binary.BigEndian.PutUint16(ack[6:8], crc)

	ack[8] = 0x0D // Stop bytes \r\n
	ack[9] = 0x0A
	return ack
}

// CalculateGT06CRC computes ITU-16 CRC used by GT06/Concox tracking devices.
func CalculateGT06CRC(data []byte) uint16 {
	var crc uint16 = 0xFFFF
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if (crc & 0x0001) != 0 {
				crc = (crc >> 1) ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc
}

func init() {
	Register(NewGT06Provider())
}
