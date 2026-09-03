package providers

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
)

// Teltonika Codec IDs
const (
	CodecIDCodec8         byte = 0x08
	CodecIDCodec8Extended byte = 0x8E
	CodecIDCodec16        byte = 0x10
)

// Standard Teltonika AVL IO Property IDs
const (
	TeltonikaIOIgnition         uint16 = 239 // 1-byte: 0=OFF, 1=ON
	TeltonikaIOIgnitionAlt      uint16 = 1   // 1-byte: 0=OFF, 1=ON (older FW)
	TeltonikaIOMovement         uint16 = 240 // 1-byte: 0=Stopped, 1=Moving
	TeltonikaIOExternalVoltage  uint16 = 66  // 2-byte: in millivolts
	TeltonikaIOBatteryVoltage   uint16 = 67  // 2-byte: in millivolts
	TeltonikaIOBatteryLevel     uint16 = 113 // 1-byte: battery percentage (0-100%)
	TeltonikaIOTotalOdometer    uint16 = 16  // 4-byte or 8-byte: in meters
	TeltonikaIOTotalOdometerAlt uint16 = 87  // 4-byte: in meters
	TeltonikaIOTripOdometer     uint16 = 199 // 4-byte: in meters
	TeltonikaIOGSMSignal        uint16 = 21  // 1-byte: GSM signal strength (0-5 or 0-31)
	TeltonikaIOFuelLevelLiters  uint16 = 84  // 4-byte or 2-byte: in liters
	TeltonikaIOFuelLevelPct     uint16 = 48  // 1-byte or 2-byte: fuel %
	TeltonikaIOAnalogInput1     uint16 = 9   // 2-byte: mV or raw sensor value
	TeltonikaIOSOS              uint16 = 246 // 1-byte: 0=normal, 1=panic/SOS
	TeltonikaIOSOSAlt           uint16 = 14  // 1-byte: 0=normal, 1=panic/SOS
	TeltonikaIOAlarm            uint16 = 236 // 1-byte: alarm event
)

// TeltonikaProvider implements TelematicsProvider for Teltonika GPS devices.
type TeltonikaProvider struct{}

// NewTeltonikaProvider constructs a TeltonikaProvider.
func NewTeltonikaProvider() *TeltonikaProvider {
	return &TeltonikaProvider{}
}

func (p *TeltonikaProvider) Name() string {
	return "teltonika"
}

func (p *TeltonikaProvider) VerifySignature(rawBody []byte, header http.Header) error {
	return nil
}

func (p *TeltonikaProvider) HandleWebhook(ctx context.Context, rawBody []byte) ([]RawFrame, error) {
	data := bytes.TrimSpace(rawBody)
	if len(data) == 0 {
		return nil, nil
	}

	// Support hex-encoded string payload if received via HTTP JSON webhook
	if hexBytes, err := hex.DecodeString(strings.TrimSpace(string(data))); err == nil && len(hexBytes) >= 10 {
		data = hexBytes
	}

	res, err := ParseTeltonikaPacket(data, "")
	if err != nil {
		return nil, err
	}

	out := make([]RawFrame, 0, len(res.Frames))
	for _, f := range res.Frames {
		if f != nil {
			out = append(out, *f)
		}
	}
	return out, nil
}

func (p *TeltonikaProvider) Poll(ctx context.Context, since time.Time) ([]RawFrame, error) {
	return nil, nil
}

// TeltonikaDataRecord represents a parsed single AVL data record from a Teltonika packet.
type TeltonikaDataRecord struct {
	Timestamp  time.Time
	Priority   byte
	Longitude  float64
	Latitude   float64
	Altitude   int16
	Angle      uint16
	Satellites uint8
	Speed      uint16
	EventIOID  uint16
	IOItems    map[uint16]uint64
}

// TeltonikaDecodedResult contains decoded frames, IMEI info, and required ACK response.
type TeltonikaDecodedResult struct {
	Frames      []*RawFrame
	Frame       *RawFrame
	ACKResponse []byte
	IMEI        string
	IsHandshake bool
	CodecID     byte
	RecordCount int
}

// DecodeTeltonikaPacket decodes raw Teltonika bytes (Handshake or Codec 8/8E) into a RawFrame.
func DecodeTeltonikaPacket(data []byte) (*RawFrame, error) {
	res, err := ParseTeltonikaPacket(data, "")
	if err != nil {
		return nil, err
	}
	if res.Frame != nil {
		return res.Frame, nil
	}
	if len(res.Frames) > 0 {
		return res.Frames[len(res.Frames)-1], nil
	}
	if res.IsHandshake && res.IMEI != "" {
		now := time.Now().UTC()
		return &RawFrame{
			IMEI:       res.IMEI,
			DeviceTime: now,
			Provider:   "teltonika",
		}, nil
	}
	return nil, errors.New("teltonika: no records decoded")
}

// ParseTeltonikaPacket parses raw Teltonika socket bytes, handling both IMEI preamble handshake
// and Codec 8 / Codec 8 Extended AVL data packets.
func ParseTeltonikaPacket(data []byte, sessionIMEI string) (*TeltonikaDecodedResult, error) {
	if len(data) < 2 {
		return nil, errors.New("teltonika: packet too short")
	}

	// 1. Check for IMEI Preamble Handshake:
	// 2 bytes length prefix (e.g. 0x000F for 15-char IMEI) + ASCII IMEI string
	if len(data) >= 17 && data[0] == 0x00 && data[1] == 0x0F {
		imeiStr := string(data[2:17])
		return &TeltonikaDecodedResult{
			IMEI:        imeiStr,
			IsHandshake: true,
			ACKResponse: []byte{0x01}, // 0x01 = Accept IMEI
			Frame: &RawFrame{
				IMEI:       imeiStr,
				DeviceTime: time.Now().UTC(),
				Provider:   "teltonika",
			},
		}, nil
	}

	// Generic handshake length check (e.g., 2 bytes prefix + N ascii characters)
	if len(data) > 2 && data[0] == 0x00 {
		expectedLen := int(binary.BigEndian.Uint16(data[0:2]))
		if expectedLen > 0 && expectedLen <= 32 && len(data) == 2+expectedLen {
			candidate := string(data[2:])
			if isAllASCIIPrintable(candidate) {
				return &TeltonikaDecodedResult{
					IMEI:        candidate,
					IsHandshake: true,
					ACKResponse: []byte{0x01},
					Frame: &RawFrame{
						IMEI:       candidate,
						DeviceTime: time.Now().UTC(),
						Provider:   "teltonika",
					},
				}, nil
			}
		}
	}

	// 2. Codec 8 / Codec 8 Extended AVL Data Packet:
	// Structure:
	// [0x00, 0x00, 0x00, 0x00] (4 bytes Preamble)
	// [Data Length (4 bytes BigEndian uint32)]
	// [Codec ID (1 byte): 0x08, 0x8E, or 0x10]
	// [Number of Data 1 (1 byte)]
	// [AVL Data Records...]
	// [Number of Data 2 (1 byte)]
	// [CRC-16 (4 bytes)]
	offset := 0
	if len(data) >= 4 && data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x00 && data[3] == 0x00 {
		offset = 4
	} else if len(data) < 12 {
		return nil, fmt.Errorf("teltonika: invalid packet header 0x%02X 0x%02X", data[0], data[1])
	}

	if len(data) < offset+8 {
		return nil, errors.New("teltonika: data packet too short")
	}

	dataLength := int(binary.BigEndian.Uint32(data[offset : offset+4]))
	offset += 4

	if len(data) < offset+dataLength {
		return nil, fmt.Errorf("teltonika: incomplete packet, expected length %d, got %d", offset+dataLength, len(data))
	}

	codecID := data[offset]
	offset++

	if codecID != CodecIDCodec8 && codecID != CodecIDCodec8Extended && codecID != CodecIDCodec16 {
		return nil, fmt.Errorf("teltonika: unsupported codec id 0x%02X", codecID)
	}

	recordCount := int(data[offset])
	offset++

	res := &TeltonikaDecodedResult{
		CodecID:     codecID,
		RecordCount: recordCount,
		IMEI:        sessionIMEI,
		Frames:      make([]*RawFrame, 0, recordCount),
	}

	// Parse AVL Data Records
	for i := 0; i < recordCount; i++ {
		frame, nextOffset, err := parseTeltonikaRecord(data, offset, codecID, sessionIMEI)
		if err != nil {
			return nil, fmt.Errorf("teltonika: record %d parse error: %w", i, err)
		}
		offset = nextOffset
		if frame != nil {
			res.Frames = append(res.Frames, frame)
		}
	}

	// Verify Record Count 2
	if offset < len(data) {
		numRecords2 := int(data[offset])
		if numRecords2 != recordCount {
			// Record count mismatch in packet footer
		}
	}

	// 4-byte ACK containing accepted record count
	res.ACKResponse = []byte{0x00, 0x00, 0x00, byte(recordCount)}
	if len(res.Frames) > 0 {
		res.Frame = res.Frames[len(res.Frames)-1]
	}

	return res, nil
}

// parseTeltonikaRecord decodes one AVL record from the byte array at the given offset.
func parseTeltonikaRecord(data []byte, offset int, codecID byte, sessionIMEI string) (*RawFrame, int, error) {
	// Minimum header before IO elements: 8B timestamp + 1B priority + 4B lon + 4B lat + 2B alt + 2B angle + 1B sat + 2B speed = 24 bytes
	if len(data) < offset+24 {
		return nil, offset, errors.New("avl record truncated in gps element")
	}

	// 1. Timestamp (8 bytes in milliseconds since Unix epoch)
	tsMs := binary.BigEndian.Uint64(data[offset : offset+8])
	deviceTime := time.UnixMilli(int64(tsMs)).UTC()
	offset += 8

	// 2. Priority (1 byte: 0=Low, 1=High, 2=Panic)
	priority := data[offset]
	offset++

	// 3. GPS Element (15 bytes):
	// Longitude (4 bytes signed int32, coordinate / 10,000,000)
	rawLon := int32(binary.BigEndian.Uint32(data[offset : offset+4]))
	lon := float64(rawLon) / 10000000.0
	offset += 4

	// Latitude (4 bytes signed int32, coordinate / 10,000,000)
	rawLat := int32(binary.BigEndian.Uint32(data[offset : offset+4]))
	lat := float64(rawLat) / 10000000.0
	offset += 4

	// Altitude (2 bytes signed int16 in meters)
	alt := int16(binary.BigEndian.Uint16(data[offset : offset+2]))
	_ = alt
	offset += 2

	// Angle / Heading (2 bytes unsigned uint16 in degrees 0-360)
	angle := float64(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2

	// Satellites (1 byte visible satellites)
	sat := int(data[offset])
	offset++

	// Speed (2 bytes uint16 in km/h)
	speed := float64(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2

	// Coordinate validity and bounds clamp
	if math.IsNaN(lat) || math.Abs(lat) > 90 {
		lat = 0
	}
	if math.IsNaN(lon) || math.Abs(lon) > 180 {
		lon = 0
	}

	isValid := sat >= 3 && (lat != 0 || lon != 0)
	motion := speed > 2.0
	isPanic := (priority == 2)

	// 4. IO Elements
	ioItems := make(map[uint16]uint64)
	var eventIOID uint16

	if codecID == CodecIDCodec8Extended {
		// Codec 8 Extended (0x8E): 2-byte event ID, 2-byte total IO, 2-byte counts and 2-byte IDs
		if len(data) < offset+4 {
			return nil, offset, errors.New("codec8 extended io header truncated")
		}
		eventIOID = binary.BigEndian.Uint16(data[offset : offset+2])
		totalIO := binary.BigEndian.Uint16(data[offset+2 : offset+4])
		_ = totalIO
		offset += 4

		// 1-byte IO elements
		if len(data) < offset+2 {
			return nil, offset, errors.New("codec8 extended n1 truncated")
		}
		n1 := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2
		for j := 0; j < n1; j++ {
			if len(data) < offset+3 {
				return nil, offset, errors.New("codec8 extended 1b io truncated")
			}
			id := binary.BigEndian.Uint16(data[offset : offset+2])
			val := uint64(data[offset+2])
			ioItems[id] = val
			offset += 3
		}

		// 2-byte IO elements
		if len(data) < offset+2 {
			return nil, offset, errors.New("codec8 extended n2 truncated")
		}
		n2 := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2
		for j := 0; j < n2; j++ {
			if len(data) < offset+4 {
				return nil, offset, errors.New("codec8 extended 2b io truncated")
			}
			id := binary.BigEndian.Uint16(data[offset : offset+2])
			val := uint64(binary.BigEndian.Uint16(data[offset+2 : offset+4]))
			ioItems[id] = val
			offset += 4
		}

		// 4-byte IO elements
		if len(data) < offset+2 {
			return nil, offset, errors.New("codec8 extended n4 truncated")
		}
		n4 := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2
		for j := 0; j < n4; j++ {
			if len(data) < offset+6 {
				return nil, offset, errors.New("codec8 extended 4b io truncated")
			}
			id := binary.BigEndian.Uint16(data[offset : offset+2])
			val := uint64(binary.BigEndian.Uint32(data[offset+2 : offset+6]))
			ioItems[id] = val
			offset += 6
		}

		// 8-byte IO elements
		if len(data) < offset+2 {
			return nil, offset, errors.New("codec8 extended n8 truncated")
		}
		n8 := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2
		for j := 0; j < n8; j++ {
			if len(data) < offset+10 {
				return nil, offset, errors.New("codec8 extended 8b io truncated")
			}
			id := binary.BigEndian.Uint16(data[offset : offset+2])
			val := binary.BigEndian.Uint64(data[offset+2 : offset+10])
			ioItems[id] = val
			offset += 10
		}

		// X-byte variable length IO elements (optional in extended)
		if len(data) >= offset+2 {
			nx := int(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
			for j := 0; j < nx; j++ {
				if len(data) < offset+4 {
					break
				}
				id := binary.BigEndian.Uint16(data[offset : offset+2])
				length := int(binary.BigEndian.Uint16(data[offset+2 : offset+4]))
				offset += 4
				if len(data) < offset+length {
					break
				}
				if length <= 8 && length > 0 {
					var val uint64
					for b := 0; b < length; b++ {
						val = (val << 8) | uint64(data[offset+b])
					}
					ioItems[id] = val
				}
				offset += length
			}
		}
	} else {
		// Standard Codec 8 (0x08)
		if len(data) < offset+2 {
			return nil, offset, errors.New("codec8 io header truncated")
		}
		eventIOID = uint16(data[offset])
		totalIO := int(data[offset+1])
		_ = totalIO
		offset += 2

		// 1-byte elements
		if len(data) < offset+1 {
			return nil, offset, errors.New("codec8 n1 truncated")
		}
		n1 := int(data[offset])
		offset++
		for j := 0; j < n1; j++ {
			if len(data) < offset+2 {
				return nil, offset, errors.New("codec8 1b io truncated")
			}
			id := uint16(data[offset])
			val := uint64(data[offset+1])
			ioItems[id] = val
			offset += 2
		}

		// 2-byte elements
		if len(data) < offset+1 {
			return nil, offset, errors.New("codec8 n2 truncated")
		}
		n2 := int(data[offset])
		offset++
		for j := 0; j < n2; j++ {
			if len(data) < offset+3 {
				return nil, offset, errors.New("codec8 2b io truncated")
			}
			id := uint16(data[offset])
			val := uint64(binary.BigEndian.Uint16(data[offset+1 : offset+3]))
			ioItems[id] = val
			offset += 3
		}

		// 4-byte elements
		if len(data) < offset+1 {
			return nil, offset, errors.New("codec8 n4 truncated")
		}
		n4 := int(data[offset])
		offset++
		for j := 0; j < n4; j++ {
			if len(data) < offset+5 {
				return nil, offset, errors.New("codec8 4b io truncated")
			}
			id := uint16(data[offset])
			val := uint64(binary.BigEndian.Uint32(data[offset+1 : offset+5]))
			ioItems[id] = val
			offset += 5
		}

		// 8-byte elements
		if len(data) < offset+1 {
			return nil, offset, errors.New("codec8 n8 truncated")
		}
		n8 := int(data[offset])
		offset++
		for j := 0; j < n8; j++ {
			if len(data) < offset+9 {
				return nil, offset, errors.New("codec8 8b io truncated")
			}
			id := uint16(data[offset])
			val := binary.BigEndian.Uint64(data[offset+1 : offset+9])
			ioItems[id] = val
			offset += 9
		}
	}

	// 5. Map IO Properties to RawFrame Fields
	var ignition *bool
	if val, ok := ioItems[TeltonikaIOIgnition]; ok {
		ign := val == 1
		ignition = &ign
	} else if val, ok := ioItems[TeltonikaIOIgnitionAlt]; ok {
		ign := val == 1
		ignition = &ign
	} else if speed > 0 {
		ign := true
		ignition = &ign
	}

	if val, ok := ioItems[TeltonikaIOMovement]; ok {
		mot := val == 1
		motion = mot
	}

	var batteryLevel *float64
	if val, ok := ioItems[TeltonikaIOBatteryLevel]; ok {
		pct := float64(val)
		if pct > 100 {
			pct = 100
		}
		batteryLevel = &pct
	} else if val, ok := ioItems[TeltonikaIOBatteryVoltage]; ok {
		mv := float64(val)
		pct := (mv - 3600.0) / (4200.0 - 3600.0) * 100.0
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		batteryLevel = &pct
	}

	var externalVoltage *float64
	if val, ok := ioItems[TeltonikaIOExternalVoltage]; ok {
		volts := float64(val) / 1000.0 // mV to Volts
		externalVoltage = &volts
	}

	var odometer *float64
	if val, ok := ioItems[TeltonikaIOTotalOdometer]; ok {
		km := float64(val) / 1000.0 // meters to KM
		odometer = &km
	} else if val, ok := ioItems[TeltonikaIOTotalOdometerAlt]; ok {
		km := float64(val) / 1000.0
		odometer = &km
	}

	var gsmSignal *int
	if val, ok := ioItems[TeltonikaIOGSMSignal]; ok {
		sig := int(val)
		gsmSignal = &sig
	}

	var fuelLevel *float64
	if val, ok := ioItems[TeltonikaIOFuelLevelPct]; ok {
		pct := float64(val)
		fuelLevel = &pct
	} else if val, ok := ioItems[TeltonikaIOFuelLevelLiters]; ok {
		liters := float64(val)
		fuelLevel = &liters
	} else if val, ok := ioItems[TeltonikaIOAnalogInput1]; ok {
		v := float64(val)
		fuelLevel = &v
	}

	if eventIOID == TeltonikaIOSOS || eventIOID == TeltonikaIOSOSAlt || ioItems[TeltonikaIOSOS] == 1 || ioItems[TeltonikaIOSOSAlt] == 1 {
		isPanic = true
	}

	fixTime := deviceTime
	frame := &RawFrame{
		IMEI:            sessionIMEI,
		DeviceTime:      deviceTime,
		Latitude:        lat,
		Longitude:       lon,
		Speed:           speed,
		Heading:         angle,
		Satellites:      &sat,
		Valid:           &isValid,
		Motion:          &motion,
		Ignition:        ignition,
		BatteryLevel:    batteryLevel,
		ExternalVoltage: externalVoltage,
		Odometer:        odometer,
		GSMSignal:       gsmSignal,
		FuelLevel:       fuelLevel,
		SOS:             isPanic,
		FixTime:         &fixTime,
		Provider:        "teltonika",
	}

	return frame, offset, nil
}

// BuildTeltonikaHandshake creates a Teltonika IMEI handshake packet (0x000F + ASCII IMEI).
func BuildTeltonikaHandshake(imei string) []byte {
	buf := make([]byte, 2+len(imei))
	binary.BigEndian.PutUint16(buf[0:2], uint16(len(imei)))
	copy(buf[2:], []byte(imei))
	return buf
}

// BuildTeltonikaCodec8Packet constructs a binary Codec 8 packet ready for TCP socket transmission.
func BuildTeltonikaCodec8Packet(records []TeltonikaDataRecord) []byte {
	var body bytes.Buffer

	// Codec ID
	body.WriteByte(CodecIDCodec8)
	// Number of Data 1
	body.WriteByte(byte(len(records)))

	for _, rec := range records {
		// 1. Timestamp (8B ms)
		var tsBytes [8]byte
		binary.BigEndian.PutUint64(tsBytes[:], uint64(rec.Timestamp.UnixMilli()))
		body.Write(tsBytes[:])

		// 2. Priority (1B)
		body.WriteByte(rec.Priority)

		// 3. GPS (15B)
		var gpsBuf [15]byte
		rawLon := int32(rec.Longitude * 10000000.0)
		rawLat := int32(rec.Latitude * 10000000.0)
		binary.BigEndian.PutUint32(gpsBuf[0:4], uint32(rawLon))
		binary.BigEndian.PutUint32(gpsBuf[4:8], uint32(rawLat))
		binary.BigEndian.PutUint16(gpsBuf[8:10], uint16(rec.Altitude))
		binary.BigEndian.PutUint16(gpsBuf[10:12], rec.Angle)
		gpsBuf[12] = rec.Satellites
		binary.BigEndian.PutUint16(gpsBuf[13:15], rec.Speed)
		body.Write(gpsBuf[:])

		// 4. IO Elements
		// Group IO by size
		var b1, b2, b4, b8 [][2]uint64
		for id, val := range rec.IOItems {
			switch {
			case id == TeltonikaIOIgnition || id == TeltonikaIOIgnitionAlt || id == TeltonikaIOMovement || id == TeltonikaIOBatteryLevel || id == TeltonikaIOGSMSignal || id == TeltonikaIOSOS:
				b1 = append(b1, [2]uint64{uint64(id), val})
			case id == TeltonikaIOExternalVoltage || id == TeltonikaIOBatteryVoltage || id == TeltonikaIOAnalogInput1:
				b2 = append(b2, [2]uint64{uint64(id), val})
			case id == TeltonikaIOTotalOdometer || id == TeltonikaIOTotalOdometerAlt || id == TeltonikaIOTripOdometer || id == TeltonikaIOFuelLevelLiters:
				b4 = append(b4, [2]uint64{uint64(id), val})
			default:
				if val <= 0xFF {
					b1 = append(b1, [2]uint64{uint64(id), val})
				} else if val <= 0xFFFF {
					b2 = append(b2, [2]uint64{uint64(id), val})
				} else if val <= 0xFFFFFFFF {
					b4 = append(b4, [2]uint64{uint64(id), val})
				} else {
					b8 = append(b8, [2]uint64{uint64(id), val})
				}
			}
		}

		totalIO := len(b1) + len(b2) + len(b4) + len(b8)
		body.WriteByte(byte(rec.EventIOID))
		body.WriteByte(byte(totalIO))

		// 1B
		body.WriteByte(byte(len(b1)))
		for _, item := range b1 {
			body.WriteByte(byte(item[0]))
			body.WriteByte(byte(item[1]))
		}

		// 2B
		body.WriteByte(byte(len(b2)))
		for _, item := range b2 {
			body.WriteByte(byte(item[0]))
			var tmp [2]byte
			binary.BigEndian.PutUint16(tmp[:], uint16(item[1]))
			body.Write(tmp[:])
		}

		// 4B
		body.WriteByte(byte(len(b4)))
		for _, item := range b4 {
			body.WriteByte(byte(item[0]))
			var tmp [4]byte
			binary.BigEndian.PutUint32(tmp[:], uint32(item[1]))
			body.Write(tmp[:])
		}

		// 8B
		body.WriteByte(byte(len(b8)))
		for _, item := range b8 {
			body.WriteByte(byte(item[0]))
			var tmp [8]byte
			binary.BigEndian.PutUint64(tmp[:], item[1])
			body.Write(tmp[:])
		}
	}

	// Number of Data 2
	body.WriteByte(byte(len(records)))

	dataField := body.Bytes()
	crc := CalculateTeltonikaCRC(dataField)

	// Complete packet: 4B preamble + 4B data length + dataField + 4B CRC
	var packet bytes.Buffer
	packet.Write([]byte{0x00, 0x00, 0x00, 0x00}) // Preamble

	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(dataField)))
	packet.Write(lenBuf[:])

	packet.Write(dataField)

	var crcBuf [4]byte
	binary.BigEndian.PutUint32(crcBuf[:], uint32(crc))
	packet.Write(crcBuf[:])

	return packet.Bytes()
}

// CalculateTeltonikaCRC calculates the standard CRC-16/IBM (reversed polynomial 0xA001) for Teltonika packets.
func CalculateTeltonikaCRC(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if (crc & 0x0001) != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

func isAllASCIIPrintable(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 32 || s[i] > 126 {
			return false
		}
	}
	return true
}

func init() {
	Register(NewTeltonikaProvider())
}
