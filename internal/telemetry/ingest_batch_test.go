package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/events"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/ports"
	"transport-app/internal/shared/uow"
)

type spyUoW struct {
	inner ports.UnitOfWork
	count atomic.Int32
}

func (s *spyUoW) Execute(ctx context.Context, fn func(ports.TxContext) error) error {
	s.count.Add(1)
	return s.inner.Execute(ctx, fn)
}

func TestIngestBatch_SingleTransactionPerBatch(t *testing.T) {
	db := newTestIngestorDB(t)

	// Setup 3 vehicles and 3 active devices
	for i := 1; i <= 3; i++ {
		devID := fmt.Sprintf("dev-batch-%d", i)
		vid := fmt.Sprintf("v-batch-%d", i)
		imei := fmt.Sprintf("IMEI-BATCH-%d", i)
		reg := fmt.Sprintf("REG-BATCH-%d", i)
		insertTestVehicleReg(t, db, vid, reg)
		_, err := db.Exec(`INSERT INTO telemetry_devices
			(id, tenant_id, imei, serial_number, device_type, status, vehicle_id, customer_id, device_secret_hash)
			VALUES (?, '1', ?, 'SN-001', 'hardware', 'active', ?, NULL, 'hash-secret')`,
			devID, imei, vid)
		require.NoError(t, err)
	}

	baseUoW := uow.NewSQLUnitOfWork(db)
	spy := &spyUoW{inner: baseUoW}
	bus := events.NewInMemoryBus()

	cfg := IngestConfig{
		OdometerMaxRegressionKM: 1.0,
		FuelClampDeltaPct:       5.0,
		BatchSize:               100,
		FlushInterval:           50 * time.Millisecond,
	}
	ing := NewIngestor(db, spy, bus, id.NewUUIDGenerator(), &testAudit{}, cfg)

	baseTime := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	var frames []RawFrame
	ignOn := true

	// Generate 15 frames total: 5 progressive frames per vehicle
	for vehIdx := 1; vehIdx <= 3; vehIdx++ {
		imei := fmt.Sprintf("IMEI-BATCH-%d", vehIdx)
		for f := 1; f <= 5; f++ {
			frames = append(frames, RawFrame{
				IMEI:          imei,
				Latitude:      19.0 + float64(vehIdx)*0.1 + float64(f)*0.01,
				Longitude:     72.0 + float64(vehIdx)*0.1 + float64(f)*0.01,
				Speed:         30.0 + float64(f)*5,
				Heading:       90.0,
				Ignition:      &ignOn,
				DeviceTime:    baseTime.Add(time.Duration(f) * time.Minute),
				Provider:      "own",
				ProviderMsgID: fmt.Sprintf("msg-b-%d-%d", vehIdx, f),
			})
		}
	}

	ctx := context.Background()
	results, err := ing.IngestBatch(ctx, frames)
	require.NoError(t, err)
	require.Len(t, results, 15)

	for i, res := range results {
		assert.True(t, res.Accepted, "frame %d must be accepted", i)
		assert.False(t, res.Deduped, "frame %d must not be deduped", i)
	}

	// Acceptance Criterion: Exactly 1 transaction executed for the whole batch
	assert.Equal(t, int32(1), spy.count.Load(), "must execute exactly 1 transaction per batch")

	// Verify all 15 frames persisted in history (telemetry_positions and telemetry_snapshots)
	var posCount, snapCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions`).Scan(&posCount))
	assert.Equal(t, 15, posCount, "all 15 positions must be recorded in telemetry_positions")

	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_snapshots`).Scan(&snapCount))
	assert.Equal(t, 15, snapCount, "all 15 snapshots must be recorded in telemetry_snapshots")

	// Verify vehicle_latest_position has the latest frame for each vehicle
	for vehIdx := 1; vehIdx <= 3; vehIdx++ {
		vid := fmt.Sprintf("v-batch-%d", vehIdx)
		expectedLat := 19.0 + float64(vehIdx)*0.1 + 0.05
		var lat, speed float64
		err := db.QueryRow(`SELECT latitude, speed FROM vehicle_latest_position WHERE vehicle_id = ?`, vid).Scan(&lat, &speed)
		require.NoError(t, err)
		assert.InDelta(t, expectedLat, lat, 0.0001, "latest position must match 5th frame for vehicle %s", vid)
		assert.InDelta(t, 55.0, speed, 0.0001)
	}

	// Acceptance Criterion: Batch outbox per flush, not per frame!
	// 15 frames across 3 vehicles must result in exactly 3 PositionEvent rows in outbox_events, NOT 15 rows.
	var outboxCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE event_type = ?`, EventTypePosition).Scan(&outboxCount))
	assert.Equal(t, 3, outboxCount, "outbox must have exactly 1 PositionEvent per vehicle aggregate in this flush, not 15")

	// Each vehicle's outbox event carries its latest position payload
	for vehIdx := 1; vehIdx <= 3; vehIdx++ {
		vid := fmt.Sprintf("v-batch-%d", vehIdx)
		var payloadStr string
		err := db.QueryRow(`SELECT payload FROM outbox_events WHERE aggregate_id = ? AND event_type = ?`, vid, EventTypePosition).Scan(&payloadStr)
		require.NoError(t, err)

		var ev PositionEvent
		require.NoError(t, json.Unmarshal([]byte(payloadStr), &ev))
		expectedLat := 19.0 + float64(vehIdx)*0.1 + 0.05
		assert.InDelta(t, expectedLat, ev.Latitude, 0.0001)
		assert.InDelta(t, 55.0, ev.Speed, 0.0001)
	}
}

func TestIngestBatch_PreservesSOSAndAlerts(t *testing.T) {
	db := newTestIngestorDB(t)

	vid := "v-sos-alert"
	imei := "IMEI-SOS-ALERT"
	insertTestVehicle(t, db, vid)
	insertTestDevice(t, db, imei, DeviceStatusActive, &vid)

	ing := newTestIngestor(t, db, nil)
	baseTime := time.Date(2026, 10, 28, 14, 0, 0, 0, time.UTC)

	// Seed previous row with healthy battery (100%) so low_battery transition fires
	battHealthy := 100.0
	_, err := ing.IngestRawFrame(context.Background(), RawFrame{
		IMEI:          imei,
		Latitude:      19.0,
		Longitude:     72.0,
		Speed:         20.0,
		BatteryLevel:  &battHealthy,
		DeviceTime:    baseTime,
		Provider:      "own",
		ProviderMsgID: "msg-seed",
	})
	require.NoError(t, err)

	// Clear outbox from seed
	_, err = db.Exec(`DELETE FROM outbox_events`)
	require.NoError(t, err)

	// Batch containing 4 frames:
	// - Frame 1: normal
	// - Frame 2: SOS triggered
	// - Frame 3: normal
	// - Frame 4: low battery (10%)
	battLow := 10.0
	frames := []RawFrame{
		{
			IMEI:          imei,
			Latitude:      19.01,
			Longitude:     72.01,
			Speed:         25.0,
			BatteryLevel:  &battHealthy,
			DeviceTime:    baseTime.Add(1 * time.Minute),
			Provider:      "own",
			ProviderMsgID: "msg-b1",
		},
		{
			IMEI:          imei,
			Latitude:      19.02,
			Longitude:     72.02,
			Speed:         26.0,
			BatteryLevel:  &battHealthy,
			SOS:           true,
			DeviceTime:    baseTime.Add(2 * time.Minute),
			Provider:      "own",
			ProviderMsgID: "msg-b2",
		},
		{
			IMEI:          imei,
			Latitude:      19.03,
			Longitude:     72.03,
			Speed:         27.0,
			BatteryLevel:  &battHealthy,
			DeviceTime:    baseTime.Add(3 * time.Minute),
			Provider:      "own",
			ProviderMsgID: "msg-b3",
		},
		{
			IMEI:          imei,
			Latitude:      19.04,
			Longitude:     72.04,
			Speed:         28.0,
			BatteryLevel:  &battLow,
			DeviceTime:    baseTime.Add(4 * time.Minute),
			Provider:      "own",
			ProviderMsgID: "msg-b4",
		},
	}

	results, err := ing.IngestBatch(context.Background(), frames)
	require.NoError(t, err)
	require.Len(t, results, 4)

	// Check outbox events:
	// - Exactly 1 PositionEvent (latest frame, lat=19.04)
	// - Exactly 1 SOSEvent (from frame 2)
	// - Exactly 1 AlertEvent (low_battery from frame 4)
	var posCount, sosCount, alertCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = ? AND event_type = ?`, vid, EventTypePosition).Scan(&posCount))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = ? AND event_type = ?`, vid, "SOSEvent").Scan(&sosCount))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = ? AND event_type = ?`, vid, "AlertEvent").Scan(&alertCount))

	assert.Equal(t, 1, posCount, "must have 1 latest position in outbox")
	assert.Equal(t, 1, sosCount, "must preserve SOS event in outbox")
	assert.Equal(t, 1, alertCount, "must preserve AlertEvent in outbox")
}

func TestAsyncIngestQueue_BatchingWithFlushInterval(t *testing.T) {
	db := newTestIngestorDB(t)
	vid := "v-queue-batch"
	imei := "IMEI-QUEUE-BATCH"
	insertTestVehicle(t, db, vid)
	insertTestDevice(t, db, imei, DeviceStatusActive, &vid)

	baseUoW := uow.NewSQLUnitOfWork(db)
	spy := &spyUoW{inner: baseUoW}
	bus := events.NewInMemoryBus()

	cfg := IngestConfig{
		BatchSize:     5,
		FlushInterval: 100 * time.Millisecond,
	}
	ing := NewIngestor(db, spy, bus, id.NewUUIDGenerator(), &testAudit{}, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	queue := NewAsyncIngestQueue(100, 1, ing, nil)
	queue.Start(ctx)
	defer queue.Drain(2 * time.Second)

	baseTime := time.Date(2026, 10, 28, 16, 0, 0, 0, time.UTC)

	// Push 5 frames in quick succession -> should trigger BatchSize threshold
	for i := 1; i <= 5; i++ {
		queue.Push(RawFrame{
			IMEI:          imei,
			Latitude:      19.0 + float64(i)*0.01,
			Longitude:     72.0 + float64(i)*0.01,
			Speed:         40.0,
			DeviceTime:    baseTime.Add(time.Duration(i) * time.Minute),
			Provider:      "own",
			ProviderMsgID: fmt.Sprintf("queue-msg-%d", i),
		})
	}

	// Wait briefly for worker to flush the batch
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions WHERE imei = ?`, imei).Scan(&n)
		if n == 5 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions WHERE imei = ?`, imei).Scan(&count))
	assert.Equal(t, 5, count)
	assert.Equal(t, int32(1), spy.count.Load(), "5 frames should be ingested in 1 batch transaction")

	// Now push 2 frames (< BatchSize) -> should flush after FlushInterval (100ms)
	for i := 6; i <= 7; i++ {
		queue.Push(RawFrame{
			IMEI:          imei,
			Latitude:      19.0 + float64(i)*0.01,
			Longitude:     72.0 + float64(i)*0.01,
			Speed:         40.0,
			DeviceTime:    baseTime.Add(time.Duration(i) * time.Minute),
			Provider:      "own",
			ProviderMsgID: fmt.Sprintf("queue-msg-%d", i),
		})
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions WHERE imei = ?`, imei).Scan(&n)
		if n == 7 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions WHERE imei = ?`, imei).Scan(&count))
	assert.Equal(t, 7, count)
	assert.Equal(t, int32(2), spy.count.Load(), "2 batches total (one by BatchSize, one by FlushInterval ticker)")
}
