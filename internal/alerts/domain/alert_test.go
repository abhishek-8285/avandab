package domain_test

import (
	"testing"
	"time"

	"transport-app/internal/alerts/domain"
)

func TestSeverityToRank(t *testing.T) {
	tests := []struct {
		severity string
		expected int
	}{
		{domain.SeverityBlocker, domain.RankCritical},
		{domain.SeverityCritical, domain.RankUrgent},
		{domain.SeverityWarning, domain.RankWaste},
		{domain.SeverityInfo, domain.RankInfo},
		{"unknown_string", domain.RankInfo},
		{"", domain.RankInfo},
	}

	for _, tt := range tests {
		t.Run("Severity_"+tt.severity, func(t *testing.T) {
			rank := domain.SeverityToRank(tt.severity)
			if rank != tt.expected {
				t.Fatalf("SeverityToRank(%q) = %d, expected %d", tt.severity, rank, tt.expected)
			}
		})
	}
}

func TestAlertConstantsAndStructs(t *testing.T) {
	now := time.Now()
	ruleID := "rule-100"
	entType := "vehicle"
	entID := "veh-99"

	alert := domain.Alert{
		ID:           "alt-1",
		RuleID:       &ruleID,
		Source:       domain.SourceTelemetry,
		AlertType:    domain.AlertTypeSpeeding,
		Severity:     domain.SeverityCritical,
		Status:       domain.StatusOpen,
		DedupKey:     "veh-99:speeding",
		TenantID:     "t-default",
		AckStatus:    domain.AckStatusOpen,
		SeverityRank: domain.SeverityToRank(domain.SeverityCritical),
		MoneyAtRisk:  0.0,
		EntityType:   &entType,
		EntityID:     &entID,
		Title:        "Speeding Alert",
		Message:      "Vehicle exceeded limit",
		Occurrences:  1,
		FirstSeenAt:  now,
		LastSeenAt:   now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if alert.SeverityRank != domain.RankUrgent {
		t.Fatalf("expected alert SeverityRank %d, got %d", domain.RankUrgent, alert.SeverityRank)
	}

	pref := domain.NotificationPreference{
		ID:          "pref-1",
		UserID:      "usr-1",
		Channel:     "email",
		Enabled:     true,
		MinSeverity: domain.SeverityCritical,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if !pref.Enabled || pref.MinSeverity != domain.SeverityCritical {
		t.Fatalf("notification preference mismatch")
	}
}
