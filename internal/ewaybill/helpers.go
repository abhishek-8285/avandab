package ewaybill

import (
	"encoding/json"
	"math"
	"time"
)

type Config struct {
	Enabled              bool
	Interval             time.Duration
	ExtensionKM          float64
	ExtensionLeadSeconds int
	MinInvoiceValue      float64
}

func extractTripID(payload interface{}) string {
	if m, ok := payload.(map[string]interface{}); ok {
		if tid, ok := m["TripID"].(string); ok && tid != "" {
			return tid
		}
		if tid, ok := m["trip_id"].(string); ok && tid != "" {
			return tid
		}
	}
	b, err := json.Marshal(payload)
	if err == nil {
		var m map[string]interface{}
		if err := json.Unmarshal(b, &m); err == nil {
			if tid, ok := m["trip_id"].(string); ok && tid != "" {
				return tid
			}
			if tid, ok := m["TripID"].(string); ok && tid != "" {
				return tid
			}
		}
	}
	return ""
}

func extractVehicleID(payload interface{}) string {
	if m, ok := payload.(map[string]interface{}); ok {
		if vid, ok := m["VehicleID"].(string); ok && vid != "" {
			return vid
		}
		if vid, ok := m["vehicle_id"].(string); ok && vid != "" {
			return vid
		}
	}
	b, err := json.Marshal(payload)
	if err == nil {
		var m map[string]interface{}
		if err := json.Unmarshal(b, &m); err == nil {
			if vid, ok := m["vehicle_id"].(string); ok && vid != "" {
				return vid
			}
			if vid, ok := m["VehicleID"].(string); ok && vid != "" {
				return vid
			}
		}
	}
	return ""
}

func extractTenantID(payload interface{}) string {
	if m, ok := payload.(map[string]interface{}); ok {
		if tid, ok := m["TenantID"].(string); ok && tid != "" {
			return tid
		}
		if tid, ok := m["tenant_id"].(string); ok && tid != "" {
			return tid
		}
	}
	b, err := json.Marshal(payload)
	if err == nil {
		var m map[string]interface{}
		if err := json.Unmarshal(b, &m); err == nil {
			if tid, ok := m["tenant_id"].(string); ok && tid != "" {
				return tid
			}
			if tid, ok := m["TenantID"].(string); ok && tid != "" {
				return tid
			}
		}
	}
	return ""
}

func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0
	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLon := (lon2 - lon1) * (math.Pi / 180.0)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*(math.Pi/180.0))*math.Cos(lat2*(math.Pi/180.0))*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}
