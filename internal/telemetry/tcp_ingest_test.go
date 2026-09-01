package telemetry

import (
	"context"
	"encoding/hex"
	"net"
	"testing"
	"time"

	"transport-app/internal/events"
)

func TestTCPIngestServer_LifecycleAndPacketHandshake(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bus := events.NewInMemoryBus()
	ingestor := &Ingestor{
		bus: bus,
		cfg: IngestConfig{},
	}
	queue := NewAsyncIngestQueue(100, 1, nil, nil)
	ingestor.SetQueue(queue)

	// Use port :0 for dynamic port allocation
	server := NewTCPIngestServer("127.0.0.1:0", ingestor, nil, nil)
	if err := server.Start(ctx); err != nil {
		t.Fatalf("failed to start tcp server: %v", err)
	}
	defer server.Stop()

	serverAddr := server.listener.Addr().String()

	// Connect TCP client
	conn, err := net.DialTimeout("tcp", serverAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("failed to dial tcp server: %v", err)
	}
	defer conn.Close()

	// Send GT06 Login Packet:
	// 7878 0d 01 0864209048123456 0001 8408 0d0a
	loginHex := "78780d010864209048123456000184080d0a"
	loginBytes, _ := hex.DecodeString(loginHex)

	_, err = conn.Write(loginBytes)
	if err != nil {
		t.Fatalf("failed to write login bytes: %v", err)
	}

	// Read ACK from server
	ackBuf := make([]byte, 10)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(ackBuf)
	if err != nil {
		t.Fatalf("failed to read ACK: %v", err)
	}

	if n != 10 {
		t.Errorf("expected 10-byte ACK, got %d", n)
	}
	if ackBuf[0] != 0x78 || ackBuf[1] != 0x78 || ackBuf[3] != 0x01 {
		t.Errorf("invalid login ACK packet: %x", ackBuf[:n])
	}

	// Send Indian AIS-140 packet over same TCP connection
	ais140Msg := "$PVT,864209048123456,31082026,083000,1831.2240,N,07351.3780,E,55.0,180.0,10,1,0*3B\r\n"
	if _, err := conn.Write([]byte(ais140Msg)); err != nil {
		t.Fatalf("failed to write AIS-140 message: %v", err)
	}
}
