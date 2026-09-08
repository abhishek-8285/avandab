package realtime_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"transport-app/internal/events"
	"transport-app/internal/realtime"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
	"transport-app/internal/telemetry"
)

type sseRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
	mu      sync.Mutex
	frames  chan string
}

func newSSERecorder() *sseRecorder {
	return &sseRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		frames:           make(chan string, 10),
	}
}

func (r *sseRecorder) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushed = true
}

func (r *sseRecorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	r.frames <- string(b)
	r.mu.Unlock()
	return r.ResponseRecorder.Write(b)
}

func TestBusIntegration_SnapshotToSSE(t *testing.T) {
	// 1. Setup in-memory DB and migrations
	name := fmt.Sprintf("test_realtime_e2e_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("failed to open sqlite memory db: %v", err)
	}
	defer db.Close()

	_ = goose.SetDialect("sqlite")
	if err := goose.Up(db, "../../db/migrations"); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// 2. Setup vehicle and device
	_, err = db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry)
		VALUES ('v-e2e-1', 'REG-E2E-1', 'MH-01-AB-1234', 'truck', 15, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'))`)
	if err != nil {
		t.Fatalf("failed to insert test vehicle: %v", err)
	}

	_, err = db.Exec(`INSERT INTO telemetry_devices
		(id, tenant_id, imei, serial_number, device_type, status, vehicle_id, customer_id, device_secret_hash)
		VALUES ('dev-e2e-1', '1', 'IMEI-E2E-1', 'SN-E2E-1', 'hardware', 'active', 'v-e2e-1', NULL, 'hash')`)
	if err != nil {
		t.Fatalf("failed to insert test device: %v", err)
	}

	// 3. Setup event bus, ingestor, hub, and router
	eventBus := events.NewInMemoryBus()
	sseHub := realtime.NewHub(15, nil)
	realtime.AttachToBus(eventBus, sseHub)

	ingestCfg := telemetry.IngestConfig{
		OdometerMaxRegressionKM: 1.0,
		FuelClampDeltaPct:       5.0,
		BatchSize:               100,
		FlushInterval:           time.Second,
		RawRetentionDays:        30,
	}
	idGen := id.NewUUIDGenerator()
	sqlUoW := uow.NewSQLUnitOfWork(db)
	ingestor := telemetry.NewIngestor(db, sqlUoW, eventBus, idGen, nil, ingestCfg)

	r := chi.NewRouter()
	telemetry.RegisterTelemetryRoutes(r, ingestor, db, 15*time.Minute, 60*time.Minute)
	r.Get("/api/v1/telemetry/stream", realtime.StreamHandler(sseHub, true))

	// 4. Start SSE subscriber on /api/v1/telemetry/stream
	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()

	sseReq := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/stream", nil).WithContext(clientCtx)
	sseRec := newSSERecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.ServeHTTP(sseRec, sseReq)
	}()

	time.Sleep(30 * time.Millisecond)

	// 5. POST a snapshot to /api/v1/telemetry/snapshots
	snap := telemetry.TelemetrySnapshotPayload{
		TripID:    "trip-e2e-99",
		VehicleID: "v-e2e-1",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Latitude:  19.0760,
		Longitude: 72.8777,
		Speed:     45.5,
		FuelLevel: 80.0,
		Odometer:  12500.0,
	}
	body, _ := json.Marshal(snap)
	postReq := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry/snapshots", bytes.NewReader(body))
	postReq.Header.Set("Content-Type", "application/json")
	postRec := httptest.NewRecorder()

	r.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("expected snapshot POST to return 200, got %d: %s", postRec.Code, postRec.Body.String())
	}

	// 6. Verify SSE subscriber receives the telemetry event within 1s
	select {
	case frame := <-sseRec.frames:
		if !strings.HasPrefix(frame, "event: telemetry\ndata: ") {
			t.Fatalf("unexpected SSE frame prefix: %s", frame)
		}
		if !strings.Contains(frame, "v-e2e-1") {
			t.Fatalf("expected SSE frame to contain vehicle v-e2e-1, got: %s", frame)
		}
		if !strings.Contains(frame, "trip-e2e-99") {
			t.Fatalf("expected SSE frame to contain trip-e2e-99, got: %s", frame)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for SSE frame from snapshot ingestion dual-write")
	}

	clientCancel()
	<-done
}
