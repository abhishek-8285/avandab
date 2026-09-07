package domain

import "time"

// MeasuringPoint is an SOP IK01 measuring point: a counter (or containment)
// attached to a fleet object, e.g. ODO/DISTANCE in KM, fuel top-up, or a
// fuel-station pump reading.
type MeasuringPoint struct {
	ID             string    `json:"id"`
	VehicleID      string    `json:"vehicle_id"`
	Category       string    `json:"category"`
	Kind           string    `json:"kind"`
	MeasPosition   string    `json:"meas_position"`
	Unit           string    `json:"unit"`
	DecimalPlaces  int64     `json:"decimal_places"`
	AnnualEstimate float64   `json:"annual_estimate"`
	CountBackwards bool      `json:"count_backwards"`
	IsCounter      bool      `json:"is_counter"`
	Description    string    `json:"description"`
	CreatedAt      time.Time `json:"created_at"`
}

// Measurement is an SOP IK11 measuring document: one counter reading plus
// the computed difference against the previous document.
type Measurement struct {
	ID                  string    `json:"id"`
	PointID             string    `json:"point_id"`
	DocNumber           string    `json:"doc_number"`
	CounterReading      float64   `json:"counter_reading"`
	DifferenceReading   float64   `json:"difference_reading"`
	TotalCounterReading float64   `json:"total_counter_reading"`
	MeasuredAt          time.Time `json:"measured_at"`
	ReadBy              string    `json:"read_by"`
	Remarks             string    `json:"remarks"`
	RecordedBy          string    `json:"recorded_by"`
	RecordedAt          time.Time `json:"recorded_at"`
}
