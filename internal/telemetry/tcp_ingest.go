package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"transport-app/internal/telemetry/providers"
)

// TCPIngestServer receives GPS telemetry from physical hardware devices over raw TCP sockets.
type TCPIngestServer struct {
	addr        string
	ingestor    *Ingestor
	deviceStore *DeviceStore
	logger      *slog.Logger
	listener    net.Listener
	wg          sync.WaitGroup
	quit        chan struct{}
	mu          sync.Mutex
	running     bool
}

// NewTCPIngestServer constructs a TCPIngestServer.
func NewTCPIngestServer(addr string, ingestor *Ingestor, store *DeviceStore, logger *slog.Logger) *TCPIngestServer {
	if addr == "" {
		addr = ":5023" // Standard default GT06/Concox GPS port
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &TCPIngestServer{
		addr:        addr,
		ingestor:    ingestor,
		deviceStore: store,
		logger:      logger,
		quit:        make(chan struct{}),
	}
}

// Start launches the TCP listener and accepts incoming hardware GPS connections.
func (s *TCPIngestServer) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errors.New("tcp server already running")
	}

	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", s.addr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("tcp listen %s: %w", s.addr, err)
	}

	s.listener = listener
	s.running = true
	s.mu.Unlock()

	s.logger.Info("telemetry hardware TCP server listening", "addr", s.addr)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-s.quit:
					return
				case <-ctx.Done():
					return
				default:
					s.logger.Debug("tcp accept error", "error", err)
					continue
				}
			}

			s.wg.Add(1)
			go func(c net.Conn) {
				defer s.wg.Done()
				s.handleConnection(ctx, c)
			}(conn)
		}
	}()

	return nil
}

// Stop gracefully shuts down the TCP listener and active connections.
func (s *TCPIngestServer) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	close(s.quit)
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.mu.Unlock()

	s.wg.Wait()
	s.logger.Info("telemetry hardware TCP server stopped")
	return nil
}

// handleConnection manages the lifecycle of a single hardware tracker socket.
func (s *TCPIngestServer) handleConnection(ctx context.Context, conn net.Conn) {
	defer func() {
		_ = conn.Close()
	}()

	remoteAddr := conn.RemoteAddr().String()
	s.logger.Debug("hardware GPS connected", "remote", remoteAddr)

	buf := make([]byte, 2048)
	var sessionIMEI string

	for {
		select {
		case <-s.quit:
			return
		case <-ctx.Done():
			return
		default:
		}

		// 5-minute read deadline for idle hardware trackers
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		n, err := conn.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.logger.Debug("hardware GPS disconnected", "remote", remoteAddr, "imei", sessionIMEI)
			}
			return
		}

		if n == 0 {
			continue
		}

		packet := buf[:n]

		// Multi-Protocol Auto-Detection:
		// 1. AIS-140 ASCII Packet ($PVT / $AIS140 / $EMR)
		if len(packet) > 0 && packet[0] == '$' {
			frame, aisErr := providers.DecodeAIS140Packet(string(packet))
			if aisErr != nil {
				s.logger.Debug("ais140 decode error", "remote", remoteAddr, "error", aisErr)
				continue
			}
			if frame != nil && frame.IMEI != "" {
				sessionIMEI = frame.IMEI
				if ingestErr := s.ingestor.IngestAsync(ctx, *frame); ingestErr != nil {
					s.logger.Error("ais140 ingest failed", "imei", frame.IMEI, "error", ingestErr)
				}
			}
			continue
		}

		// 2. GT06 / Concox Binary Hex Packet (0x78 0x78 or 0x79 0x79)
		res, err := providers.ParseGT06Packet(packet, sessionIMEI)
		if err != nil {
			s.logger.Debug("unknown packet format", "remote", remoteAddr, "error", err)
			continue
		}

		// If this is a login or first identified packet, associate IMEI with socket session
		if res.IMEI != "" {
			sessionIMEI = res.IMEI
		}

		// Send protocol ACK back to device immediately if required
		if len(res.ACKResponse) > 0 {
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, writeErr := conn.Write(res.ACKResponse); writeErr != nil {
				s.logger.Warn("failed to send ACK to device", "imei", sessionIMEI, "error", writeErr)
			}
		}

		// If frame was decoded, ingest through the async queue pipeline
		if res.Frame != nil && res.Frame.IMEI != "" {
			if ingestErr := s.ingestor.IngestAsync(ctx, *res.Frame); ingestErr != nil {
				s.logger.Error("hardware GPS ingest failed", "imei", res.Frame.IMEI, "error", ingestErr)
			}
		}
	}
}
