package handlers

import (
	"bytes"
	"html/template"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	vehicleapp "transport-app/internal/vehicle/application"
)

// Root-cause lock for the Vehicle View odometer bug: CurrentMileage and
// StandardKmpl are *float64. The old template passed the POINTER straight to
// printf "%.0f"/"%.2f", and Go formats a *float64 as %!f(*float64=0x...) —
// the "%{...} km" garbage seen on real devices. These tests pin the helpers
// and the rendered page to never emit raw format syntax again.

func fptr(f float64) *float64 { return &f }

func TestFormatOdometer(t *testing.T) {
	tests := []struct {
		name    string
		current *float64
		odo     float64
		want    string
	}{
		{"both missing renders dash", nil, 0, "—"},
		{"current mileage wins with grouping", fptr(12345), 999, "12,345 km"},
		{"falls back to odometer", nil, 9876.4, "9,876 km"},
		{"rounds halves away display", fptr(12345.6), 0, "12,346 km"},
		{"explicit zero renders as 0 km", fptr(0), 500, "0 km"},
		{"millions group", fptr(1234567), 0, "1,234,567 km"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatOdometer(tt.current, tt.odo))
		})
	}
}

func TestFormatKmpl(t *testing.T) {
	assert.Equal(t, "—", formatKmpl(nil))
	assert.Equal(t, "4.50", formatKmpl(fptr(4.5)))
	assert.Equal(t, "4.57", formatKmpl(fptr(4.567)))
}

func TestGroupThousands(t *testing.T) {
	assert.Equal(t, "0", groupThousands(0))
	assert.Equal(t, "999", groupThousands(999))
	assert.Equal(t, "1,000", groupThousands(1000))
	assert.Equal(t, "12,345", groupThousands(12345))
	assert.Equal(t, "1,234,567", groupThousands(1234567))
}

func TestDerefNum(t *testing.T) {
	assert.Equal(t, "", derefNum((*float64)(nil)))
	assert.Equal(t, 5.0, derefNum(fptr(5)))
	assert.Equal(t, "x", derefNum("x"))
	assert.Equal(t, false, derefNum(false))
}

// TestOldPrintfPointerRedProof documents the root cause: the pre-fix
// expression `printf "%.0f" .CurrentMileage` (pointer operand) emits %!f.
// If this ever stops containing %!f, the language changed — the template
// must still go through formatOdometer, not raw printf.
func TestOldPrintfPointerRedProof(t *testing.T) {
	tmpl, err := template.New("old").Parse(`{{printf "%.0f" .M}} km`)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, tmpl.Execute(&buf, map[string]interface{}{"M": fptr(12345)}))
	assert.Contains(t, buf.String(), "%!f",
		"expected raw-printf-on-pointer to emit %%!f (the original bug shape)")
}

func renderVehicleView(t *testing.T, v vehicleapp.VehicleResponseDTO) string {
	t.Helper()
	return renderVehicleViewWith(t, v, nil)
}

func renderVehicleViewWith(t *testing.T, v vehicleapp.VehicleResponseDTO, extra map[string]interface{}) string {
	t.Helper()
	tmpl, err := parseTemplates(&mockAuthSvc{})
	require.NoError(t, err)
	data := map[string]interface{}{
		"Vehicle":                   v,
		"MaintenanceDue":            "",
		"MaintenanceOverrideBy":     "",
		"MaintenanceOverrideReason": "",
		"IsMaintenanceDue":          false,
		"IsMaintenanceOverridden":   false,
		"DocCards":                  []map[string]interface{}{},
		"LastPosition":              map[string]interface{}{"Has": false},
		"RecentTrips":               []map[string]interface{}{},
		"OpenWorkOrders":            []map[string]interface{}{},
		"MeasuringPoints":           []map[string]interface{}{},
		"RecentMeasurements":        []map[string]interface{}{},
		"RecentCommands":            []map[string]interface{}{},
		"User":                      &auth.SessionData{UserID: "u-1", Role: "admin", Name: "Admin"},
	}
	for k, val := range extra {
		data[k] = val
	}
	var buf bytes.Buffer
	require.NoError(t, tmpl.ExecuteTemplate(&buf, "vehicle_view.html", data))
	return buf.String()
}

func TestVehicleViewOdometerRendersGroupedKm(t *testing.T) {
	out := renderVehicleView(t, vehicleapp.VehicleResponseDTO{
		ID: "veh-1", RegistrationNumber: "MH12AB1234",
		CurrentMileage: fptr(12345), Odometer: 12345, StandardKmpl: fptr(4.5),
	})
	assert.Contains(t, out, "12,345 km")
	assert.Contains(t, out, "4.50")
	assert.NotContains(t, out, "%!")
	assert.NotContains(t, out, "0x")
}

func TestVehicleViewOdometerMissingRendersDash(t *testing.T) {
	out := renderVehicleView(t, vehicleapp.VehicleResponseDTO{
		ID: "veh-2", RegistrationNumber: "MH12AB1235",
	})
	assert.Contains(t, out, "data-odometer>—<")
	assert.NotContains(t, out, "0 km")
	assert.NotContains(t, out, "%!")
}

func TestVehicleViewKeepsProgressiveDisclosureHooks(t *testing.T) {
	out := renderVehicleView(t, vehicleapp.VehicleResponseDTO{
		ID: "veh-3", RegistrationNumber: "MH12AB1236",
	})
	for _, sec := range []string{
		`data-section="compliance"`, `data-section="fleet"`, `data-section="trips"`,
		`data-section="measuring"`,
		`data-section="maintenance-status"`, `data-section="jobcards"`,
		`data-section="teleop"`, `data-section="telemetry"`,
	} {
		assert.Contains(t, out, sec, "section hook missing")
	}
	// Recent Measurements only renders when readings exist (pre-existing).
	out2 := renderVehicleViewWith(t, vehicleapp.VehicleResponseDTO{
		ID: "veh-4", RegistrationNumber: "MH12AB1237",
	}, map[string]interface{}{
		"RecentMeasurements": []map[string]interface{}{
			{"Counter": "12345", "Unit": "km", "Kind": "ODO", "MeasPosition": "TOTAL"},
		},
	})
	assert.Contains(t, out2, `data-section="measurements"`)
	for _, hook := range []string{"data-summary", "data-maintenance", "data-teleop", "data-odometer", "data-kmpl"} {
		assert.Contains(t, out, hook, "hook missing")
	}
}
