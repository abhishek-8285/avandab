package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"transport-app/internal/domain"
	"transport-app/internal/events"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
)

// TelemetryDataPoint represents a streamed IoT data point from a vehicle/sensor.
type TelemetryDataPoint struct {
	VehicleID       domain.VehicleID `json:"vehicle_id"`
	TripID          *domain.TripID   `json:"trip_id,omitempty"`
	DriverID        *domain.DriverID `json:"driver_id,omitempty"`
	Latitude        float64          `json:"latitude"`
	Longitude       float64          `json:"longitude"`
	Speed           float64          `json:"speed"`
	FuelLevel       float64          `json:"fuel_level"`
	IgnitionOn      bool             `json:"ignition_on"`
	Temperature     *float64         `json:"temperature,omitempty"`
	Odometer        float64          `json:"odometer"`
	Timestamp       time.Time        `json:"timestamp"`
	PlannedRouteLat float64          `json:"planned_route_lat,omitempty"`
	PlannedRouteLng float64          `json:"planned_route_lng,omitempty"`
}

// TelemetryAlert represents an exception alert raised by telemetry.
type TelemetryAlert struct {
	ID        string    `json:"id"`
	TripID    string    `json:"trip_id"`
	VehicleID string    `json:"vehicle_id"`
	DriverID  string    `json:"driver_id"`
	AlertType string    `json:"alert_type"` // gps_deviation, fuel_theft, temp_breach
	Severity  string    `json:"severity"`   // warning, critical
	Details   string    `json:"details"`
	CreatedAt time.Time `json:"created_at"`
}

// TelemetryService ingests IoT streams and evaluates exception alerting rules.
type TelemetryService struct {
	baseService
}

// ProcessTelemetryStream evaluates telemetry data points against exception rules.
// Rule 3: GPS deviation > 5km OR Fuel drop > 10L while ignition OFF.
func (s *TelemetryService) ProcessTelemetryStream(ctx context.Context, dp TelemetryDataPoint, lastFuelLevel float64) ([]TelemetryAlert, error) {
	var generatedAlerts []TelemetryAlert

	// 1. Rule 3 Check: GPS Deviation > 5.0 km
	if dp.PlannedRouteLat != 0 && dp.PlannedRouteLng != 0 {
		distKM := haversineDistance(dp.Latitude, dp.Longitude, dp.PlannedRouteLat, dp.PlannedRouteLng)
		if distKM > 5.0 {
			alert := TelemetryAlert{
				ID:        generateID(),
				TripID:    strFromPtr(dp.TripID),
				VehicleID: string(dp.VehicleID),
				DriverID:  strFromPtr(dp.DriverID),
				AlertType: "gps_deviation",
				Severity:  "critical",
				Details:   fmt.Sprintf("GPS deviation of %.2f km detected from planned route (limit 5.0 km)", distKM),
				CreatedAt: time.Now(),
			}
			generatedAlerts = append(generatedAlerts, alert)
			s.persistRawAlert(ctx, alert, dp.Latitude, dp.Longitude)

			if s.log != nil {
				s.log.Warn("telemetry exception: GPS deviation", "dist_km", distKM, "vehicle_id", dp.VehicleID)
			}
			if s.events != nil {
				s.events.Publish(ctx, events.Event{
					Type: "AlertEvent",
					Payload: map[string]interface{}{
						"source":      "telemetry",
						"alert_type":  alert.AlertType,
						"severity":    alert.Severity,
						"title":       "GPS Deviation Alert",
						"details":     alert.Details,
						"vehicle_id":  alert.VehicleID,
						"driver_id":   alert.DriverID,
						"trip_id":     alert.TripID,
						"tenant_id":   string(shared.TenantIDFromContext(ctx)),
						"latitude":    dp.Latitude,
						"longitude":   dp.Longitude,
						"occurred_at": alert.CreatedAt,
					},
				})
				s.events.Publish(ctx, events.Event{
					Type: events.GPSDeviationAlert,
					Payload: map[string]interface{}{
						"tenant_id":   string(shared.TenantIDFromContext(ctx)),
						"trip_id":     alert.TripID,
						"vehicle_id":  alert.VehicleID,
						"alert":       alert,
						"eta_risk":    "HIGH",
						"occurred_at": time.Now(),
					},
				})
			}
		}
	}

	// 2. Rule 3 Check: Fuel drop > 10L while ignition is OFF (Fuel Theft / Theft Suspicion Alert)
	if !dp.IgnitionOn && lastFuelLevel > 0 {
		fuelDrop := lastFuelLevel - dp.FuelLevel
		if fuelDrop > 10.0 {
			alert := TelemetryAlert{
				ID:        generateID(),
				TripID:    strFromPtr(dp.TripID),
				VehicleID: string(dp.VehicleID),
				DriverID:  strFromPtr(dp.DriverID),
				AlertType: "theft_suspicion",
				Severity:  "critical",
				Details:   fmt.Sprintf("Fuel theft suspected: %.2f L drop detected while ignition OFF", fuelDrop),
				CreatedAt: time.Now(),
			}
			generatedAlerts = append(generatedAlerts, alert)
			s.persistRawAlert(ctx, alert, dp.Latitude, dp.Longitude)

			if s.log != nil {
				s.log.Warn("telemetry exception: Fuel Theft", "fuel_drop_l", fuelDrop, "vehicle_id", dp.VehicleID)
			}
			if s.events != nil {
				s.events.Publish(ctx, events.Event{
					Type: "AlertEvent",
					Payload: map[string]interface{}{
						"source":      "fuel",
						"alert_type":  alert.AlertType,
						"severity":    alert.Severity,
						"title":       "Fuel Theft Suspicion",
						"details":     alert.Details,
						"vehicle_id":  alert.VehicleID,
						"driver_id":   alert.DriverID,
						"trip_id":     alert.TripID,
						"tenant_id":   string(shared.TenantIDFromContext(ctx)),
						"latitude":    dp.Latitude,
						"longitude":   dp.Longitude,
						"occurred_at": alert.CreatedAt,
					},
				})
				s.events.Publish(ctx, events.Event{
					Type: events.FuelTheftAlert,
					Payload: map[string]interface{}{
						"tenant_id":   string(shared.TenantIDFromContext(ctx)),
						"trip_id":     alert.TripID,
						"vehicle_id":  alert.VehicleID,
						"alert":       alert,
						"occurred_at": time.Now(),
					},
				})
			}
		}
	}

	return generatedAlerts, nil
}

func (s *TelemetryService) persistRawAlert(ctx context.Context, alert TelemetryAlert, lat, lng float64) {
	if getter, ok := s.store.(repository.DBGetter); ok && getter != nil && getter.DB() != nil {
		var tripIDVal, driverIDVal *string
		if alert.TripID != "" {
			tripIDVal = &alert.TripID
		}
		if alert.DriverID != "" {
			driverIDVal = &alert.DriverID
		}
		_, _ = getter.DB().ExecContext(ctx, `
			INSERT INTO telemetry_alerts (id, trip_id, vehicle_id, driver_id, alert_type, severity, details, latitude, longitude, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			alert.ID, tripIDVal, alert.VehicleID, driverIDVal, alert.AlertType, alert.Severity, alert.Details, lat, lng, alert.CreatedAt)
	}
}

func strFromPtr[T ~string](ptr *T) string {
	if ptr == nil {
		return ""
	}
	return string(*ptr)
}

// haversineDistance calculates distance in kilometers between two lat/lng points.
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0 // Radius of Earth in km
	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLon := (lon2 - lon1) * (math.Pi / 180.0)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*(math.Pi/180.0))*math.Cos(lat2*(math.Pi/180.0))*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}
