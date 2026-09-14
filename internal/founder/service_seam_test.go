package founder

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"transport-app/internal/events"
	"transport-app/internal/founder/customer_health"
	"transport-app/internal/shared/ports"
)

// seqID returns stub-1, stub-2, … — deterministic, so alert ids are exact.
type seqID struct{ n int }

func (s *seqID) GenerateUUID() string {
	s.n++
	return fmt.Sprintf("stub-%d", s.n)
}

func (s *seqID) GenerateDisplayID(prefix string) string { return prefix + "-" + s.GenerateUUID() }

// fixedClock never advances — the coarse-clock collision case by construction.
type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// Compile-time seam check: the fakes above must satisfy the ports.
func TestSeamFakesSatisfyPorts(t *testing.T) {
	var (
		_ ports.IDGenerator = (*seqID)(nil)
		_ ports.Clock       = fixedClock{}
	)
}

// Alert ids used to be fmt.Sprintf("rev_%d", time.Now().UnixNano()). Two
// alerts minted inside one coarse-clock tick collided. With the seam injected,
// ids and timestamps are fully deterministic under fakes.
func TestFounderService_SeamDeterministicIDs(t *testing.T) {
	frozen := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	mock := &MockNotifier{}
	svc := NewFounderService(mock, &seqID{}, fixedClock{t: frozen})
	bus := events.NewInMemoryBus()
	svc.RegisterEventHandlers(bus)

	bus.Publish(context.Background(), events.Event{
		Type:    "customer.activated",
		Payload: map[string]interface{}{"company_name": "Acme", "plan": "Pro", "mrr": "100"},
	})
	require.Len(t, mock.SentEvents, 1)
	require.Equal(t, "rev_stub-1", mock.SentEvents[0].ID)
	require.True(t, mock.SentEvents[0].Timestamp.Equal(frozen))

	svc.EvaluateCustomerHealth("comp_1", "Acme", customer_health.CustomerHealthFactors{LastLoginDays: 15})
	require.Len(t, mock.SentEvents, 2)
	require.Equal(t, "churn_comp_1_stub-2", mock.SentEvents[1].ID)
	require.True(t, mock.SentEvents[1].Timestamp.Equal(frozen))
}
