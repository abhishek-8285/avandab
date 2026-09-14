package notifications

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"transport-app/internal/shared/ports"
)

// seqID returns stub-1, stub-2, … — deterministic, so notif_ ids are exact.
type seqID struct{ n int }

func (s *seqID) GenerateUUID() string {
	s.n++
	return fmt.Sprintf("stub-%d", s.n)
}

func (s *seqID) GenerateDisplayID(prefix string) string { return prefix + "-" + s.GenerateUUID() }

// fixedClock never advances — the coarse-clock collision case by construction.
type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

var (
	_ ports.IDGenerator = (*seqID)(nil)
	_ ports.Clock       = fixedClock{}
)

// notif_ ids used to come from a package-level generator + time.Now() inline,
// untestable and invisible to callers. With the seam injected, two sends
// inside one frozen tick produce distinct, deterministic ids and timestamps.
func TestService_SendInAppSeamDeterministic(t *testing.T) {
	frozen := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	svc := NewService(&seqID{}, fixedClock{t: frozen})
	ctx := context.Background()

	msg := ports.NotificationMessage{TenantID: "t1", UserID: "u1", Recipient: "u1", Subject: "a", Body: "b"}
	require.NoError(t, svc.SendInApp(ctx, msg))
	require.NoError(t, svc.SendInApp(ctx, msg))

	got := svc.inAppStore["u1"]
	require.Len(t, got, 2)
	require.Equal(t, "notif_stub-1", got[0].ID)
	require.Equal(t, "notif_stub-2", got[1].ID)
	require.True(t, got[0].CreatedAt.Equal(frozen), "CreatedAt must come from injected clock")
	require.True(t, strings.HasPrefix(got[0].ID, "notif_"))
}
