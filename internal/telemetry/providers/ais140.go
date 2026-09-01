package providers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// AIS140Provider implements TelematicsProvider for Indian AIS-140 standard GPS trackers.
type AIS140Provider struct{}

// NewAIS140Provider constructs an AIS140Provider.
func NewAIS140Provider() *AIS140Provider {
	return &AIS140Provider{}
}

func (p *AIS140Provider) Name() string {
	return "ais140"
}

func (p *AIS140Provider) VerifySignature(rawBody []byte, header http.Header) error {
	return nil
}

func (p *AIS140Provider) HandleWebhook(ctx context.Context, rawBody []byte) ([]RawFrame, error) {
	frame, err := DecodeAIS140Packet(string(rawBody))
	if err != nil {
		return nil, err
	}
	if frame == nil {
		return nil, nil
	}
	return []RawFrame{*frame}, nil
}

func (p *AIS140Provider) Poll(ctx context.Context, since time.Time) ([]RawFrame, error) {
	return nil, nil
}

// DecodeAIS140Packet decodes a standard AIS-140 ASCII string.
// Standard AIS-140 Packet Formats:
// $PVT,VendorID,Firmware,PacketType,AlertID,PacketStatus,IMEI,VehicleReg,GPSFix,Date(DDMMYYYY),Time(HHMMSS),Lat,LatDir,Lng,LngDir,Speed,Heading,Satellites,Altitude,PDOP,HDOP,Operator,Ignition,MainPower,InternalBatt,EmergencyStatus...
// Or compact: $PVT,IMEI,DDMMYYYY,HHMMSS,Lat,LatDir,Lng,LngDir,Speed,Heading,Satellites,Ignition,Emergency*Checksum
func DecodeAIS140Packet(raw string) (*RawFrame, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "$") {
		return nil, errors.New("ais140: missing leading $ delimiter")
	}

	// Strip checksum if present (*XX)
	body := raw[1:]
	if idx := strings.Index(body, "*"); idx != -1 {
		body = body[:idx]
	}

	parts := strings.Split(body, ",")
	if len(parts) < 10 {
		return nil, errors.New("ais140: insufficient fields")
	}

	header := parts[0]
	if header != "PVT" && header != "AIS140" && header != "POS" && header != "EMR" {
		return nil, errors.New("ais140: unsupported packet type " + header)
	}

	var imei string
	var lat, lng, speed, heading float64
	var satellites int
	var isNorth, isEast, ignition, isEmergency bool
	var validFix = true
	var deviceTime time.Time

	// Case 1: Standard Full AIS-140 (20+ fields, IMEI usually at index 6)
	if len(parts) >= 18 && len(parts[6]) >= 15 {
		imei = parts[6]
		dateStr := parts[9]  // DDMMYYYY
		timeStr := parts[10] // HHMMSS
		deviceTime = parseAIS140DateTime(dateStr, timeStr)

		rawLat, _ := strconv.ParseFloat(parts[11], 64)
		isNorth = (parts[12] == "N")
		rawLng, _ := strconv.ParseFloat(parts[13], 64)
		isEast = (parts[14] == "E")

		lat = convertNMEAToDecimal(rawLat, isNorth)
		lng = convertNMEAToDecimal(rawLng, isEast)

		speed, _ = strconv.ParseFloat(parts[15], 64)
		heading, _ = strconv.ParseFloat(parts[16], 64)
		satellites, _ = strconv.Atoi(parts[17])

		if len(parts) > 22 {
			ignition = (parts[22] == "1" || strings.EqualFold(parts[22], "ON"))
		}
		if len(parts) > 25 {
			isEmergency = (parts[25] == "1" || strings.EqualFold(parts[25], "SOS"))
		}
	} else {
		// Case 2: Compact AIS-140 ($PVT,IMEI,DDMMYYYY,HHMMSS,Lat,LatDir,Lng,LngDir,Speed,Heading,Satellites,Ignition,SOS)
		imei = parts[1]
		dateStr := parts[2]
		timeStr := parts[3]
		deviceTime = parseAIS140DateTime(dateStr, timeStr)

		rawLat, _ := strconv.ParseFloat(parts[4], 64)
		isNorth = (parts[5] == "N")
		rawLng, _ := strconv.ParseFloat(parts[6], 64)
		isEast = (parts[7] == "E")

		lat = convertNMEAToDecimal(rawLat, isNorth)
		lng = convertNMEAToDecimal(rawLng, isEast)

		speed, _ = strconv.ParseFloat(parts[8], 64)
		if len(parts) > 9 {
			heading, _ = strconv.ParseFloat(parts[9], 64)
		}
		if len(parts) > 10 {
			satellites, _ = strconv.Atoi(parts[10])
		}
		if len(parts) > 11 {
			ignition = (parts[11] == "1" || strings.EqualFold(parts[11], "ON"))
		}
		if len(parts) > 12 {
			isEmergency = (parts[12] == "1" || strings.EqualFold(parts[12], "SOS"))
		}
	}

	if imei == "" {
		return nil, errors.New("ais140: empty imei")
	}

	motion := speed > 2.0
	frame := &RawFrame{
		IMEI:       imei,
		DeviceTime: deviceTime,
		Latitude:   lat,
		Longitude:  lng,
		Speed:      speed,
		Heading:    heading,
		Satellites: &satellites,
		Valid:      &validFix,
		Motion:     &motion,
		Ignition:   &ignition,
		SOS:        isEmergency || (header == "EMR"),
		Provider:   "ais140",
	}

	return frame, nil
}

// convertNMEAToDecimal converts NMEA coordinate DDMM.MMMM to decimal degrees.
func convertNMEAToDecimal(raw float64, isPositive bool) float64 {
	if raw == 0 {
		return 0
	}
	degrees := float64(int(raw / 100))
	minutes := raw - (degrees * 100)
	decimal := degrees + (minutes / 60.0)
	if !isPositive {
		decimal = -decimal
	}
	return decimal
}

// parseAIS140DateTime parses DDMMYYYY and HHMMSS.
func parseAIS140DateTime(dateStr, timeStr string) time.Time {
	if len(dateStr) == 8 && len(timeStr) >= 6 {
		day, _ := strconv.Atoi(dateStr[0:2])
		month, _ := strconv.Atoi(dateStr[2:4])
		year, _ := strconv.Atoi(dateStr[4:8])

		hour, _ := strconv.Atoi(timeStr[0:2])
		min, _ := strconv.Atoi(timeStr[2:4])
		sec, _ := strconv.Atoi(timeStr[4:6])

		return time.Date(year, time.Month(month), day, hour, min, sec, 0, time.UTC)
	}
	return time.Now().UTC()
}

func init() {
	Register(NewAIS140Provider())
}
