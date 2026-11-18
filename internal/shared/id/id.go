package id

import (
	"encoding/binary"
	"fmt"
	"sync/atomic"

	"github.com/google/uuid"
	"transport-app/internal/shared/ports"
)

type uuidGenerator struct {
	seq atomic.Uint64
}

// NewUUIDGenerator returns an IDGenerator using UUID v4.
func NewUUIDGenerator() ports.IDGenerator {
	g := &uuidGenerator{}
	// Seed display IDs from a random 64-bit value: sequential calls are
	// distinct and monotonically increasing, and separate processes get
	// disjoint ranges. A truncated UUID (32 bits) collided by the birthday
	// bound at ~77k rows, but invoice_number is globally UNIQUE.
	u := uuid.New()
	g.seq.Store(binary.BigEndian.Uint64(u[:8]))
	return g
}

func (g *uuidGenerator) GenerateUUID() string {
	return uuid.NewString()
}

func (g *uuidGenerator) GenerateDisplayID(prefix string) string {
	return fmt.Sprintf("%s-%016x", prefix, g.seq.Add(1))
}
