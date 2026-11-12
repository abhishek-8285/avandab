package digest_test

import (
	"strings"
	"testing"
	"time"

	"transport-app/internal/founder/digest"
)

func TestFormatDailyDigest(t *testing.T) {
	date := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	report := digest.DailyDigestReport{
		Date:           date,
		Visitors:       120,
		NewSignups:     15,
		Activated:      8,
		Trials:         5,
		Paid:           10,
		MRR:            "₹1,50,000",
		ChurnRiskCount: 2,
		CriticalErrors: 0,
		OpenIncidents:  1,
	}

	got := digest.FormatDailyDigest(report)
	if !strings.Contains(got, "Nov 12, 2026") {
		t.Errorf("expected date in digest, got: %s", got)
	}
	if !strings.Contains(got, "₹1,50,000") {
		t.Errorf("expected MRR in digest, got: %s", got)
	}
	if !strings.Contains(got, "15") || !strings.Contains(got, "8") {
		t.Errorf("expected metrics in digest, got: %s", got)
	}
}
