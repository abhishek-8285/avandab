package main

import (
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"transport-app/internal/telemetry/providers"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorPurple = "\033[35m"
	colorCyan   = "\033[36m"
	colorWhite  = "\033[37m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

// Coordinate represents a single GPS geographic point (WGS84).
type Coordinate struct {
	Lat float64
	Lng float64
}

// Predefined realistic Indian highway routes
var routes = map[string][]Coordinate{
	"delhi-gurgaon": {
		{Lat: 28.6129, Lng: 77.2295}, // India Gate, New Delhi
		{Lat: 28.5960, Lng: 77.1680}, // Dhaula Kuan
		{Lat: 28.5490, Lng: 77.1210}, // Mahipalpur / Aerocity
		{Lat: 28.5080, Lng: 77.0870}, // Cyber Hub / DLF Phase 2
		{Lat: 28.4720, Lng: 77.0690}, // IFFCO Chowk, Gurugram
		{Lat: 28.4350, Lng: 77.0390}, // Rajiv Chowk, Gurugram
		{Lat: 28.4050, Lng: 77.0520}, // Golf Course Ext Road
	},
	"delhi-jaipur": {
		{Lat: 28.6139, Lng: 77.2090}, // Delhi Connaught Place
		{Lat: 28.4595, Lng: 77.0266}, // Gurugram
		{Lat: 28.3540, Lng: 76.9380}, // Manesar NH48
		{Lat: 28.1920, Lng: 76.6210}, // Dharuhera
		{Lat: 28.0850, Lng: 76.4350}, // Bawal Industrial Area
		{Lat: 27.9860, Lng: 76.3810}, // Neemrana Fort / Highway
		{Lat: 27.8890, Lng: 76.2840}, // Behror
		{Lat: 27.7050, Lng: 76.2050}, // Kotputli
		{Lat: 27.5020, Lng: 76.0120}, // Paota
		{Lat: 27.3890, Lng: 75.9620}, // Shahpura
		{Lat: 27.1210, Lng: 75.8750}, // Chandwaji
		{Lat: 26.9850, Lng: 75.8500}, // Amer, Jaipur
		{Lat: 26.9220, Lng: 75.8267}, // Jaipur Hawa Mahal / Pink City
	},
	"mumbai-pune": {
		{Lat: 19.0600, Lng: 72.8680}, // BKC, Mumbai
		{Lat: 19.0330, Lng: 73.0297}, // Navi Mumbai Vashi
		{Lat: 18.9900, Lng: 73.1170}, // Panvel Expressway Start
		{Lat: 18.7900, Lng: 73.3100}, // Khalapur Toll Plaza
		{Lat: 18.7500, Lng: 73.4100}, // Lonavala / Khandala Ghats
		{Lat: 18.7200, Lng: 73.6800}, // Talegaon
		{Lat: 18.6270, Lng: 73.8000}, // Pimpri-Chinchwad
		{Lat: 18.5314, Lng: 73.8446}, // Shivajinagar, Pune
	},
}

func main() {
	var (
		protocolFlag = flag.String("protocol", "teltonika", "GPS hardware protocol: teltonika | ais140 | gt06")
		imeiFlag     = flag.String("imei", "358123456789012", "15-digit Device IMEI")
		vehicleFlag  = flag.String("vehicle", "DL-01-AB-1234", "Vehicle Registration Number")
		speedFlag    = flag.Float64("speed", 65.0, "Base target vehicle speed in km/h")
		intervalFlag = flag.Int("interval", 2, "Telemetry transmission interval in seconds")
		routeFlag    = flag.String("route", "delhi-jaipur", "Route name (delhi-jaipur | delhi-gurgaon | mumbai-pune)")
		addrFlag     = flag.String("addr", "localhost:5023", "TCP Ingest Server host:port")
	)
	flag.Parse()

	protocol := strings.ToLower(strings.TrimSpace(*protocolFlag))
	if protocol != "teltonika" && protocol != "ais140" && protocol != "gt06" {
		fmt.Fprintf(os.Stderr, "%sError: Unsupported protocol '%s'. Choose teltonika, ais140, or gt06.%s\n", colorRed, protocol, colorReset)
		os.Exit(1)
	}

	routePoints, ok := routes[*routeFlag]
	if !ok {
		// Fallback to delhi-jaipur
		routePoints = routes["delhi-jaipur"]
	}

	fmt.Println()
	fmt.Printf("%s%s========================================================================%s\n", colorBold, colorCyan, colorReset)
	fmt.Printf("%s%s  🛰️  Avandab Live GPS Hardware Tracker Simulator%s\n", colorBold, colorWhite, colorReset)
	fmt.Printf("%s%s========================================================================%s\n", colorBold, colorCyan, colorReset)
	fmt.Printf(" %sProtocol:%s    %s%s%s\n", colorBold, colorReset, colorYellow, strings.ToUpper(protocol), colorReset)
	fmt.Printf(" %sDevice IMEI:%s %s%s%s\n", colorBold, colorReset, colorGreen, *imeiFlag, colorReset)
	fmt.Printf(" %sVehicle:%s     %s%s%s\n", colorBold, colorReset, colorWhite, *vehicleFlag, colorReset)
	fmt.Printf(" %sTarget Route:%s%s%s (%d waypoints)%s\n", colorBold, colorReset, colorCyan, *routeFlag, len(routePoints), colorReset)
	fmt.Printf(" %sBase Speed:%s  %.1f km/h | %sInterval:%s %ds | %sTarget:%s %s\n",
		colorBold, colorReset, *speedFlag, colorBold, colorReset, *intervalFlag, colorBold, colorReset, *addrFlag)
	fmt.Printf("%s%s------------------------------------------------------------------------%s\n\n", colorBold, colorCyan, colorReset)

	// Connect to TCP server
	fmt.Printf("%s Connecting to TCP telemetry server at %s...%s\n", colorYellow, *addrFlag, colorReset)
	conn, err := net.DialTimeout("tcp", *addrFlag, 5*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s❌ Failed to connect to %s: %v%s\n", colorRed, *addrFlag, err, colorReset)
		fmt.Fprintf(os.Stderr, "%sEnsure the server is running (`bin/server` or `TELEMETRY_TCP_PORT=:5023`).%s\n", colorDim, colorReset)
		os.Exit(1)
	}
	defer conn.Close()
	fmt.Printf("%s Connected successfully! Performing protocol handshake...%s\n", colorGreen, colorReset)

	// Perform Protocol-Specific Handshake
	if err := performHandshake(conn, protocol, *imeiFlag); err != nil {
		fmt.Fprintf(os.Stderr, "%s❌ Handshake failed: %v%s\n", colorRed, err, colorReset)
		os.Exit(1)
	}
	fmt.Printf("%s Handshake ACK confirmed! Commencing live telemetry streaming...%s\n\n", colorGreen, colorReset)

	// Setup graceful termination
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Simulator State
	currentSegment := 0
	segmentProgress := 0.0 // 0.0 to 1.0 between currentSegment and currentSegment+1
	odometerKM := 12540.0
	fuelLevelPct := 92.5
	serialNum := uint16(1)
	ticker := time.NewTicker(time.Duration(*intervalFlag) * time.Second)
	defer ticker.Stop()

	packetCount := 0

	for {
		select {
		case <-sigChan:
			fmt.Printf("\n%s🛑 Simulation interrupted by user. Closing socket session...%s\n", colorYellow, colorReset)
			return
		case t := <-ticker.C:
			packetCount++

			// Calculate next GPS position along route
			p1 := routePoints[currentSegment]
			p2 := routePoints[(currentSegment+1)%len(routePoints)]

			segDistKM := haversineKM(p1.Lat, p1.Lng, p2.Lat, p2.Lng)
			if segDistKM <= 0.001 {
				segDistKM = 0.001
			}

			// Add slight realistic speed variation (+/- 4 km/h)
			currentSpeed := *speedFlag + (rand.Float64()*8.0 - 4.0)
			if currentSpeed < 10 {
				currentSpeed = 10
			}

			// Step distance for this interval
			intervalHours := float64(*intervalFlag) / 3600.0
			stepKM := currentSpeed * intervalHours
			odometerKM += stepKM

			// Fuel consumption (~1 liter per 4 km for commercial heavy vehicle -> ~0.05% per km)
			fuelLevelPct -= stepKM * 0.035
			if fuelLevelPct < 5.0 {
				fuelLevelPct = 95.0 // Refuel simulation
			}

			segmentProgress += stepKM / segDistKM
			if segmentProgress >= 1.0 {
				segmentProgress = 0.0
				currentSegment = (currentSegment + 1) % len(routePoints)
				p1 = routePoints[currentSegment]
				p2 = routePoints[(currentSegment+1)%len(routePoints)]
			}

			// Interpolate latitude and longitude
			currentLat := p1.Lat + (p2.Lat-p1.Lat)*segmentProgress
			currentLng := p1.Lng + (p2.Lng-p1.Lng)*segmentProgress
			heading := calculateBearing(p1.Lat, p1.Lng, p2.Lat, p2.Lng)

			satellites := 12 + rand.Intn(4) // 12-15 satellites
			extVoltage := 12.35 + (rand.Float64()*0.4 - 0.2)
			batteryPct := 98.0

			// Transmit packet and receive ACK
			ackStr, err := sendTelemetryFrame(conn, protocol, *imeiFlag, currentLat, currentLng, currentSpeed, heading, satellites, extVoltage, batteryPct, odometerKM, fuelLevelPct, serialNum, t)
			if err != nil {
				fmt.Printf("%s[%s] Packet #%d failed: %v%s\n", colorRed, t.Format("15:04:05"), packetCount, err, colorReset)
				return
			}
			serialNum++

			// Live CLI Status print
			fmt.Printf("%s[%s]%s #%03d | %sLat:%s %8.5f | %sLng:%s %8.5f | %sSpd:%s %4.1f km/h | %sHdg:%s %3.0f° | %sOdo:%s %7.1f km | %sFuel:%s %4.1f%% | %sACK:%s %s%s%s\n",
				colorDim, t.Format("15:04:05"), colorReset,
				packetCount,
				colorCyan, colorReset, currentLat,
				colorCyan, colorReset, currentLng,
				colorYellow, colorReset, currentSpeed,
				colorWhite, colorReset, heading,
				colorGreen, colorReset, odometerKM,
				colorPurple, colorReset, fuelLevelPct,
				colorBold, colorReset, colorGreen, ackStr, colorReset,
			)
		}
	}
}

// performHandshake initiates the initial handshake packet expected by the protocol.
func performHandshake(conn net.Conn, protocol, imei string) error {
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	switch protocol {
	case "teltonika":
		// Teltonika IMEI Preamble: 0x00 0x0F + 15 ASCII bytes IMEI
		handshake := providers.BuildTeltonikaHandshake(imei)
		if _, err := conn.Write(handshake); err != nil {
			return fmt.Errorf("teltonika write handshake: %w", err)
		}
		ackBuf := make([]byte, 1)
		if _, err := conn.Read(ackBuf); err != nil {
			return fmt.Errorf("teltonika read handshake ack: %w", err)
		}
		if ackBuf[0] != 0x01 {
			return fmt.Errorf("server rejected IMEI (ack 0x%02X)", ackBuf[0])
		}
		return nil

	case "gt06":
		// GT06 Login Packet: 0x78 0x78 + 0x0D + 0x01 + 8B BCD IMEI + 2B Serial + 2B CRC + 0x0D 0x0A
		loginPacket := buildGT06LoginPacket(imei, 1)
		if _, err := conn.Write(loginPacket); err != nil {
			return fmt.Errorf("gt06 write login: %w", err)
		}
		ackBuf := make([]byte, 10)
		if _, err := conn.Read(ackBuf); err != nil {
			return fmt.Errorf("gt06 read login ack: %w", err)
		}
		if len(ackBuf) != 10 || ackBuf[0] != 0x78 || ackBuf[1] != 0x78 || ackBuf[3] != 0x01 {
			return fmt.Errorf("invalid GT06 login ack: %x", ackBuf)
		}
		return nil

	case "ais140":
		// AIS-140 doesn't require pre-handshake socket exchange; first frame registers IMEI
		return nil

	default:
		return fmt.Errorf("unknown protocol: %s", protocol)
	}
}

// sendTelemetryFrame constructs and sends a live GPS record and awaits ACK.
func sendTelemetryFrame(
	conn net.Conn,
	protocol, imei string,
	lat, lng, speed, heading float64,
	satellites int,
	extVoltage, batteryPct, odometerKM, fuelLevelPct float64,
	serial uint16,
	t time.Time,
) (string, error) {
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	switch protocol {
	case "teltonika":
		record := providers.TeltonikaDataRecord{
			Timestamp:  t.UTC(),
			Priority:   1,
			Longitude:  lng,
			Latitude:   lat,
			Altitude:   215,
			Angle:      uint16(math.Round(heading)),
			Satellites: uint8(satellites),
			Speed:      uint16(math.Round(speed)),
			EventIOID:  1,
			IOItems: map[uint16]uint64{
				providers.TeltonikaIOIgnition:        1,
				providers.TeltonikaIOMovement:        1,
				providers.TeltonikaIOExternalVoltage: uint64(extVoltage * 1000.0),
				providers.TeltonikaIOBatteryLevel:    uint64(batteryPct),
				providers.TeltonikaIOTotalOdometer:   uint64(odometerKM * 1000.0),
				providers.TeltonikaIOGSMSignal:       5,
				providers.TeltonikaIOFuelLevelPct:    uint64(fuelLevelPct),
			},
		}

		packet := providers.BuildTeltonikaCodec8Packet([]providers.TeltonikaDataRecord{record})
		if _, err := conn.Write(packet); err != nil {
			return "", fmt.Errorf("write teltonika packet: %w", err)
		}

		ackBuf := make([]byte, 4)
		if _, err := conn.Read(ackBuf); err != nil {
			return "", fmt.Errorf("read teltonika ack: %w", err)
		}
		return fmt.Sprintf("0x000000%02X (%d records)", ackBuf[3], ackBuf[3]), nil

	case "ais140":
		// Format NMEA DDMM.MMMM
		latDeg := int(math.Abs(lat))
		latMin := (math.Abs(lat) - float64(latDeg)) * 60.0
		latDir := "N"
		if lat < 0 {
			latDir = "S"
		}
		latNMEA := fmt.Sprintf("%02d%07.4f", latDeg, latMin)

		lngDeg := int(math.Abs(lng))
		lngMin := (math.Abs(lng) - float64(lngDeg)) * 60.0
		lngDir := "E"
		if lng < 0 {
			lngDir = "W"
		}
		lngNMEA := fmt.Sprintf("%03d%07.4f", lngDeg, lngMin)

		dateStr := t.Format("02012006")
		timeStr := t.Format("150405")

		rawPayload := fmt.Sprintf("PVT,%s,%s,%s,%s,%s,%s,%s,%.1f,%.1f,%d,1,0",
			imei, dateStr, timeStr, latNMEA, latDir, lngNMEA, lngDir, speed, heading, satellites)

		// Calculate 8-bit XOR checksum
		var checksum byte
		for i := 0; i < len(rawPayload); i++ {
			checksum ^= rawPayload[i]
		}
		msg := fmt.Sprintf("$%s*%02X\r\n", rawPayload, checksum)

		if _, err := conn.Write([]byte(msg)); err != nil {
			return "", fmt.Errorf("write ais140 msg: %w", err)
		}
		return "OK (Streamed)", nil

	case "gt06":
		locPacket := buildGT06LocationPacket(lat, lng, speed, heading, satellites, serial, t)
		if _, err := conn.Write(locPacket); err != nil {
			return "", fmt.Errorf("write gt06 location: %w", err)
		}
		ackBuf := make([]byte, 10)
		if _, err := conn.Read(ackBuf); err != nil {
			return "", fmt.Errorf("read gt06 ack: %w", err)
		}
		return fmt.Sprintf("0x%02X Serial %d", ackBuf[3], binary.BigEndian.Uint16(ackBuf[4:6])), nil

	default:
		return "", fmt.Errorf("unknown protocol: %s", protocol)
	}
}

// buildGT06LoginPacket constructs a binary GT06 login frame.
func buildGT06LoginPacket(imei string, serial uint16) []byte {
	buf := make([]byte, 18)
	buf[0] = 0x78
	buf[1] = 0x78
	buf[2] = 0x0D
	buf[3] = 0x01

	// Pad or trim IMEI to 16 hex chars (8 BCD bytes)
	padded := imei
	if len(padded) < 16 {
		padded = strings.Repeat("0", 16-len(padded)) + padded
	} else if len(padded) > 16 {
		padded = padded[len(padded)-16:]
	}
	bcdBytes, _ := hex.DecodeString(padded)
	if len(bcdBytes) == 8 {
		copy(buf[4:12], bcdBytes)
	}

	binary.BigEndian.PutUint16(buf[12:14], serial)
	crc := providers.CalculateGT06CRC(buf[2:14])
	binary.BigEndian.PutUint16(buf[14:16], crc)
	buf[16] = 0x0D
	buf[17] = 0x0A
	return buf
}

// buildGT06LocationPacket constructs a binary GT06 GPS (0x12) frame.
func buildGT06LocationPacket(lat, lng, speed, heading float64, satellites int, serial uint16, t time.Time) []byte {
	buf := make([]byte, 28)
	buf[0] = 0x78
	buf[1] = 0x78
	buf[2] = 0x16 // Length = 22 bytes
	buf[3] = 0x12 // Protocol: Location

	// Date time (UTC)
	buf[4] = byte(t.Year() - 2000)
	buf[5] = byte(t.Month())
	buf[6] = byte(t.Day())
	buf[7] = byte(t.Hour())
	buf[8] = byte(t.Minute())
	buf[9] = byte(t.Second())

	// Satellites
	buf[10] = byte(0xC0 | (satellites & 0x0F))

	// Latitude (deg * 60 * 30000)
	rawLat := uint32(math.Abs(lat) * 1800000.0)
	binary.BigEndian.PutUint32(buf[11:15], rawLat)

	// Longitude (deg * 60 * 30000)
	rawLng := uint32(math.Abs(lng) * 1800000.0)
	binary.BigEndian.PutUint32(buf[15:19], rawLng)

	// Speed (km/h)
	buf[19] = byte(int(speed) & 0xFF)

	// Course & Status flags
	courseVal := uint16(int(heading) & 0x03FF)
	if lat >= 0 {
		courseVal |= 0x0400 // North
	}
	if lng >= 0 {
		courseVal |= 0x0800 // East
	}
	courseVal |= 0x1000 // GPS Fix Valid
	binary.BigEndian.PutUint16(buf[20:22], courseVal)

	binary.BigEndian.PutUint16(buf[22:24], serial)
	crc := providers.CalculateGT06CRC(buf[2:24])
	binary.BigEndian.PutUint16(buf[24:26], crc)
	buf[26] = 0x0D
	buf[27] = 0x0A
	return buf
}

// haversineKM calculates great-circle distance between two points in kilometers.
func haversineKM(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0 // Earth radius in km
	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLon := (lon2 - lon1) * (math.Pi / 180.0)

	rLat1 := lat1 * (math.Pi / 180.0)
	rLat2 := lat2 * (math.Pi / 180.0)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Sin(dLon/2)*math.Sin(dLon/2)*math.Cos(rLat1)*math.Cos(rLat2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

// calculateBearing computes compass bearing from point 1 to point 2 in degrees (0-360).
func calculateBearing(lat1, lon1, lat2, lon2 float64) float64 {
	rLat1 := lat1 * (math.Pi / 180.0)
	rLat2 := lat2 * (math.Pi / 180.0)
	dLon := (lon2 - lon1) * (math.Pi / 180.0)

	y := math.Sin(dLon) * math.Cos(rLat2)
	x := math.Cos(rLat1)*math.Sin(rLat2) - math.Sin(rLat1)*math.Cos(rLat2)*math.Cos(dLon)
	bearingRad := math.Atan2(y, x)
	bearingDeg := (bearingRad * 180.0 / math.Pi)
	return math.Mod(bearingDeg+360.0, 360.0)
}
