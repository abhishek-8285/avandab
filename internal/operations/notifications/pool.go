package notifications

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	idpkg "transport-app/internal/shared/id"
)

// ProviderSpec defines one SMTP relay / direct-MX provider in the pool.
// DailyQuota / MonthlyQuota == 0 means unlimited (e.g. Postfix/direct).
type ProviderSpec struct {
	Name         string  `json:"name"`
	Host         string  `json:"host"`
	Port         string  `json:"port"`
	User         string  `json:"user"`
	Password     string  `json:"password"`
	From         string  `json:"from"`
	Direct       bool    `json:"direct,omitempty"`
	DailyQuota   int     `json:"daily_quota"`
	MonthlyQuota int     `json:"monthly_quota"`
	Priority     int     `json:"priority"`
	Enabled      *bool   `json:"enabled,omitempty"`
	CostPer1k    float64 `json:"cost_per_1k,omitempty"`
}

// isEnabled returns true when Enabled is nil or true.
func (s ProviderSpec) isEnabled() bool {
	if s.Enabled == nil {
		return true
	}
	return *s.Enabled
}

// PoolConfig groups all providers plus the selection strategy.
type PoolConfig struct {
	Strategy  string         `json:"strategy"` // priority | cost_optimized | round_robin
	Providers []ProviderSpec `json:"providers"`
}

// ProviderUsage is the quota snapshot exposed to admins / metrics.
type ProviderUsage struct {
	Name             string  `json:"name"`
	Host             string  `json:"host"`
	Enabled          bool    `json:"enabled"`
	Priority         int     `json:"priority"`
	DailyQuota       int     `json:"daily_quota"`
	MonthlyQuota     int     `json:"monthly_quota"`
	DailyUsed        int     `json:"daily_used"`
	MonthlyUsed      int     `json:"monthly_used"`
	DailyRemaining   int     `json:"daily_remaining"`
	MonthlyRemaining int     `json:"monthly_remaining"`
	CostPer1k        float64 `json:"cost_per_1k"`
	Exhausted        bool    `json:"exhausted"`
}

// EmailPool implements RichEmailSender by routing each send through the cheapest
// free-tier provider that still has quota. Quota counters are persisted in
// email_provider_counters + email_send_log so usage survives restarts and is
// queryable for cost dashboards.
type EmailPool struct {
	mu        sync.RWMutex
	providers []*providerEntry
	strategy  string
	db        *sql.DB
	logger    *slog.Logger
	rrIdx     int
	// in-memory fallback counters when db == nil (tests / dev without migration)
	memCounters map[string]*memCounter
}

type providerEntry struct {
	spec   ProviderSpec
	sender RichEmailSender
}

type memCounter struct {
	dailyUsed    int
	monthlyUsed  int
	currentDay   string
	currentMonth string
}

// NewEmailPool builds a pool from an explicit PoolConfig. Callers that want
// env-driven config should use NewEmailPoolFromEnv.
func NewEmailPool(cfg PoolConfig, db *sql.DB, logger *slog.Logger) *EmailPool {
	if logger == nil {
		logger = slog.Default()
	}
	strategy := strings.ToLower(strings.TrimSpace(cfg.Strategy))
	if strategy == "" {
		strategy = "priority"
	}
	p := &EmailPool{
		strategy:    strategy,
		db:          db,
		logger:      logger,
		memCounters: make(map[string]*memCounter),
	}
	for _, spec := range cfg.Providers {
		if strings.TrimSpace(spec.Name) == "" {
			continue
		}
		if spec.From == "" {
			spec.From = DefaultFromEmail
		}
		entry := &providerEntry{
			spec:   spec,
			sender: buildSenderForSpec(spec),
		}
		p.providers = append(p.providers, entry)
		p.memCounters[spec.Name] = &memCounter{
			currentDay:   time.Now().Format("2006-01-02"),
			currentMonth: time.Now().Format("2006-01"),
		}
	}
	p.sortProviders()
	if db != nil {
		p.syncFromDB()
		p.ensureDBRows()
	}
	return p
}

// NewEmailPoolFromEnv builds a pool from environment variables. When no explicit
// EMAIL_PROVIDERS_JSON is set it auto-assembles Brevo (300/day 9000/mo),
// Resend (100/day 3000/mo), the primary SMTP_HOST relay, and direct-MX fallback.
func NewEmailPoolFromEnv(db *sql.DB, logger *slog.Logger) *EmailPool {
	cfg := LoadPoolConfigFromEnv()
	return NewEmailPool(cfg, db, logger)
}

// LoadPoolConfigFromEnv parses EMAIL_POOL_STRATEGY / EMAIL_PROVIDERS_JSON plus
// per-provider env vars into a PoolConfig. Exported for testing / admin reload.
func LoadPoolConfigFromEnv() PoolConfig {
	strategy := strings.ToLower(strings.TrimSpace(os.Getenv("EMAIL_POOL_STRATEGY")))
	if strategy == "" {
		strategy = strings.ToLower(strings.TrimSpace(os.Getenv("EMAIL_PROVIDER_STRATEGY")))
	}
	if strategy == "" {
		strategy = "priority"
	}

	// Explicit JSON wins.
	if j := strings.TrimSpace(os.Getenv("EMAIL_PROVIDERS_JSON")); j != "" {
		var cfg PoolConfig
		if err := json.Unmarshal([]byte(j), &cfg); err == nil && len(cfg.Providers) > 0 {
			if cfg.Strategy != "" {
				strategy = strings.ToLower(cfg.Strategy)
			}
			cfg.Strategy = strategy
			return cfg
		}
		// Also allow bare array: [{"name":"brevo",...}]
		var arr []ProviderSpec
		if err := json.Unmarshal([]byte(j), &arr); err == nil && len(arr) > 0 {
			return PoolConfig{Strategy: strategy, Providers: arr}
		}
	}

	var providers []ProviderSpec

	smtpFrom := os.Getenv("SMTP_FROM")
	if smtpFrom == "" {
		smtpFrom = DefaultFromEmail
	}

	// Brevo — 9k/mo 300/day free tier
	brevoHost := firstEnv("BREVO_SMTP_HOST", "BREVO_HOST", "")
	brevoUser := firstEnv("BREVO_SMTP_USER", "BREVO_USER", "")
	brevoPass := firstEnv("BREVO_SMTP_PASSWORD", "BREVO_SMTP_PASS", "BREVO_API_KEY", "BREVO_PASSWORD", "")
	brevoFrom := firstEnv("BREVO_SMTP_FROM", "BREVO_FROM", smtpFrom)
	if brevoHost != "" || brevoUser != "" || brevoPass != "" || os.Getenv("BREVO_ENABLED") != "" {
		if brevoHost == "" {
			brevoHost = "smtp-relay.brevo.com"
		}
		enabled := true
		if v := os.Getenv("BREVO_ENABLED"); v == "false" || v == "0" {
			enabled = false
		}
		providers = append(providers, ProviderSpec{
			Name:         "brevo",
			Host:         brevoHost,
			Port:         firstEnv("BREVO_SMTP_PORT", "BREVO_PORT", "587"),
			User:         brevoUser,
			Password:     brevoPass,
			From:         brevoFrom,
			DailyQuota:   envInt("BREVO_DAILY_QUOTA", 300),
			MonthlyQuota: envInt("BREVO_MONTHLY_QUOTA", 9000),
			Priority:     envInt("BREVO_PRIORITY", 1),
			Enabled:      &enabled,
			CostPer1k:    0,
		})
	} else {
		// Always seed brevo entry with quotas so quota tracking works even before
		// credentials are configured — it will simply fail over on auth error.
		enabled := true
		providers = append(providers, ProviderSpec{
			Name:         "brevo",
			Host:         "smtp-relay.brevo.com",
			Port:         "587",
			User:         brevoUser,
			Password:     brevoPass,
			From:         brevoFrom,
			DailyQuota:   300,
			MonthlyQuota: 9000,
			Priority:     1,
			Enabled:      &enabled,
		})
	}

	// Resend — 3k/mo 100/day free tier
	resendHost := firstEnv("RESEND_SMTP_HOST", "RESEND_HOST", "")
	resendUser := firstEnv("RESEND_SMTP_USER", "RESEND_USER", "")
	resendPass := firstEnv("RESEND_SMTP_PASSWORD", "RESEND_SMTP_PASS", "RESEND_API_KEY", "RESEND_PASSWORD", "")
	resendFrom := firstEnv("RESEND_SMTP_FROM", "RESEND_FROM", smtpFrom)
	if resendHost != "" || resendUser != "" || resendPass != "" || os.Getenv("RESEND_ENABLED") != "" {
		if resendHost == "" {
			resendHost = "smtp.resend.com"
		}
		enabled := true
		if v := os.Getenv("RESEND_ENABLED"); v == "false" || v == "0" {
			enabled = false
		}
		providers = append(providers, ProviderSpec{
			Name:         "resend",
			Host:         resendHost,
			Port:         firstEnv("RESEND_SMTP_PORT", "RESEND_PORT", "587"),
			User:         resendUser,
			Password:     resendPass,
			From:         resendFrom,
			DailyQuota:   envInt("RESEND_DAILY_QUOTA", 100),
			MonthlyQuota: envInt("RESEND_MONTHLY_QUOTA", 3000),
			Priority:     envInt("RESEND_PRIORITY", 2),
			Enabled:      &enabled,
		})
	} else {
		enabled := true
		providers = append(providers, ProviderSpec{
			Name:         "resend",
			Host:         "smtp.resend.com",
			Port:         "587",
			User:         "",
			Password:     "",
			From:         resendFrom,
			DailyQuota:   100,
			MonthlyQuota: 3000,
			Priority:     2,
			Enabled:      &enabled,
		})
	}

	// Primary relay from generic SMTP_* env (existing single-provider config)
	if h := strings.TrimSpace(os.Getenv("SMTP_HOST")); h != "" && !strings.EqualFold(h, "direct") && h != "localhost" && h != "127.0.0.1" {
		enabled := true
		providers = append(providers, ProviderSpec{
			Name:         "primary",
			Host:         h,
			Port:         firstEnv("SMTP_PORT", "587"),
			User:         firstEnv("SMTP_USER", ""),
			Password:     firstEnv("SMTP_PASSWORD", "SMTP_PASS", ""),
			From:         smtpFrom,
			DailyQuota:   envInt("SMTP_DAILY_QUOTA", 0),
			MonthlyQuota: envInt("SMTP_MONTHLY_QUOTA", 0),
			Priority:     envInt("SMTP_PRIORITY", 10),
			Enabled:      &enabled,
		})
	} else if h == "localhost" || h == "127.0.0.1" {
		enabled := true
		providers = append(providers, ProviderSpec{
			Name:         "local",
			Host:         h,
			Port:         firstEnv("SMTP_PORT", "25"),
			User:         "",
			Password:     "",
			From:         smtpFrom,
			DailyQuota:   0,
			MonthlyQuota: 0,
			Priority:     5,
			Enabled:      &enabled,
		})
	}

	// Direct MX fallback — always last resort, unlimited quota but lower deliverability
	{
		enabled := true
		if v := os.Getenv("DIRECT_ENABLED"); v == "false" || v == "0" {
			enabled = false
		}
		providers = append(providers, ProviderSpec{
			Name:         "direct",
			Host:         "direct",
			Direct:       true,
			From:         smtpFrom,
			DailyQuota:   0,
			MonthlyQuota: 0,
			Priority:     envInt("DIRECT_PRIORITY", 90),
			Enabled:      &enabled,
		})
	}

	return PoolConfig{Strategy: strategy, Providers: providers}
}

func buildSenderForSpec(spec ProviderSpec) RichEmailSender {
	if spec.Direct || strings.EqualFold(strings.TrimSpace(spec.Host), "direct") {
		from := spec.From
		if from == "" {
			from = DefaultFromEmail
		}
		return NewDirectMXEmailSender(DirectMXConfig{From: from})
	}
	host := strings.ToLower(strings.TrimSpace(spec.Host))
	if host == "localhost" || host == "127.0.0.1" {
		port := spec.Port
		if port == "" {
			port = "25"
		}
		from := spec.From
		if from == "" {
			from = DefaultFromEmail
		}
		return NewSMTPEmailSender(SMTPConfig{
			Host: host, Port: port, User: "", Password: "", From: from,
		})
	}
	return NewSMTPEmailSender(SMTPConfig{
		Host: spec.Host, Port: spec.Port, User: spec.User, Password: spec.Password, From: spec.From,
	})
}

func (p *EmailPool) sortProviders() {
	switch p.strategy {
	case "cost_optimized", "cost", "cheapest":
		sort.SliceStable(p.providers, func(i, j int) bool {
			if p.providers[i].spec.CostPer1k != p.providers[j].spec.CostPer1k {
				return p.providers[i].spec.CostPer1k < p.providers[j].spec.CostPer1k
			}
			return p.providers[i].spec.Priority < p.providers[j].spec.Priority
		})
	case "round_robin":
		// keep priority order but RR will rotate among quota-available
		sort.SliceStable(p.providers, func(i, j int) bool {
			return p.providers[i].spec.Priority < p.providers[j].spec.Priority
		})
	default: // priority
		sort.SliceStable(p.providers, func(i, j int) bool {
			return p.providers[i].spec.Priority < p.providers[j].spec.Priority
		})
	}
}

// syncFromDB merges admin overrides from email_providers into in-memory specs.
// DB is source of truth for enabled/priority/quotas after the first boot.
func (p *EmailPool) syncFromDB() {
	if p.db == nil {
		return
	}
	rows, err := p.db.QueryContext(context.Background(), `SELECT provider, enabled, priority, daily_quota, monthly_quota, cost_per_1k, host, port, from_addr FROM email_providers`)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	overrides := make(map[string]ProviderSpec)
	for rows.Next() {
		var name, host, port, fromAddr sql.NullString
		var enabled int
		var priority, dailyQuota, monthlyQuota int
		var costPer1k float64
		if err := rows.Scan(&name, &enabled, &priority, &dailyQuota, &monthlyQuota, &costPer1k, &host, &port, &fromAddr); err != nil {
			continue
		}
		ps := ProviderSpec{
			Name: name.String, Priority: priority, DailyQuota: dailyQuota, MonthlyQuota: monthlyQuota, CostPer1k: costPer1k,
		}
		e := enabled == 1
		ps.Enabled = &e
		if host.Valid {
			ps.Host = host.String
		}
		if port.Valid {
			ps.Port = port.String
		}
		if fromAddr.Valid {
			ps.From = fromAddr.String
		}
		overrides[name.String] = ps
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.providers {
		o, ok := overrides[e.spec.Name]
		if !ok {
			continue
		}
		if o.Enabled != nil {
			e.spec.Enabled = o.Enabled
		}
		// Priority/quota/cost from DB always wins when row exists
		e.spec.Priority = o.Priority
		e.spec.DailyQuota = o.DailyQuota
		e.spec.MonthlyQuota = o.MonthlyQuota
		e.spec.CostPer1k = o.CostPer1k
		if o.Host != "" {
			e.spec.Host = o.Host
		}
		if o.Port != "" {
			e.spec.Port = o.Port
		}
		if o.From != "" {
			e.spec.From = o.From
		}
	}
	p.sortProvidersLocked()
}

func (p *EmailPool) sortProvidersLocked() {
	switch p.strategy {
	case "cost_optimized", "cost", "cheapest":
		sort.SliceStable(p.providers, func(i, j int) bool {
			if p.providers[i].spec.CostPer1k != p.providers[j].spec.CostPer1k {
				return p.providers[i].spec.CostPer1k < p.providers[j].spec.CostPer1k
			}
			return p.providers[i].spec.Priority < p.providers[j].spec.Priority
		})
	default:
		sort.SliceStable(p.providers, func(i, j int) bool {
			return p.providers[i].spec.Priority < p.providers[j].spec.Priority
		})
	}
}

func (p *EmailPool) ensureDBRows() {
	if p.db == nil {
		return
	}
	for _, e := range p.providers {
		_, _ = p.db.ExecContext(context.Background(), `INSERT OR IGNORE INTO email_providers (provider, enabled, priority, daily_quota, monthly_quota, cost_per_1k, host, port, from_addr)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.spec.Name, boolToInt(e.spec.isEnabled()), e.spec.Priority, e.spec.DailyQuota, e.spec.MonthlyQuota, e.spec.CostPer1k, e.spec.Host, e.spec.Port, e.spec.From)
		_, _ = p.db.ExecContext(context.Background(), `INSERT OR IGNORE INTO email_provider_counters (provider, daily_used, monthly_used, current_day, current_month)
			VALUES (?, 0, 0, date('now'), strftime('%Y-%m','now'))`, e.spec.Name)
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Configured reports whether at least one provider is enabled and configured.
func (p *EmailPool) Configured() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, e := range p.providers {
		if e.spec.isEnabled() && e.sender.Configured() {
			return true
		}
	}
	// Even if senders report unconfigured (missing creds), pool is still
	// considered configured when at least one enabled entry exists — it will
	// fail over honestly and the error will be surfaced.
	for _, e := range p.providers {
		if e.spec.isEnabled() {
			return true
		}
	}
	return false
}

func (p *EmailPool) Send(ctx context.Context, to, subject, body string) error {
	return p.SendRich(ctx, EmailMessage{To: to, Subject: subject, TextBody: body})
}

func (p *EmailPool) SendHTML(ctx context.Context, to, subject, textBody, htmlBody string) error {
	return p.SendRich(ctx, EmailMessage{To: to, Subject: subject, TextBody: textBody, HTMLBody: htmlBody})
}

func (p *EmailPool) SendWithAttachments(ctx context.Context, to, subject, textBody, htmlBody string, attachments []Attachment) error {
	return p.SendRich(ctx, EmailMessage{To: to, Subject: subject, TextBody: textBody, HTMLBody: htmlBody, Attachments: attachments})
}

// SendRich tries providers in priority order, respecting daily/monthly quotas,
// and fails over on error. The successful provider's quota is incremented.
func (p *EmailPool) SendRich(ctx context.Context, msg EmailMessage) error {
	candidates := p.quotaAwareCandidates()
	if len(candidates) == 0 {
		return fmt.Errorf("email pool: no provider with available quota (all %d providers exhausted or disabled)", len(p.providers))
	}

	// Round-robin rotates the candidate slice
	if p.strategy == "round_robin" {
		p.mu.Lock()
		if p.rrIdx >= len(candidates) {
			p.rrIdx = 0
		}
		if p.rrIdx > 0 {
			candidates = append(candidates[p.rrIdx:], candidates[:p.rrIdx]...)
		}
		p.rrIdx = (p.rrIdx + 1) % len(candidates)
		p.mu.Unlock()
	}

	var lastErr error
	var attempted []string
	for _, entry := range candidates {
		attempted = append(attempted, entry.spec.Name)
		err := entry.sender.SendRich(ctx, msg)
		if err == nil {
			_ = p.recordUsage(entry.spec.Name, msg)
			p.logger.Info("email pool: delivered", "provider", entry.spec.Name, "to", msg.To, "subject", msg.Subject)
			return nil
		}
		lastErr = err
		p.logger.Warn("email pool: provider failed, failing over", "provider", entry.spec.Name, "error", err)
		_ = p.recordFailure(entry.spec.Name, msg, err)
		// Auth errors are not retried on same provider but we still try next
	}
	return fmt.Errorf("email pool: all %d candidates failed %v: last error: %w", len(candidates), attempted, lastErr)
}

func (p *EmailPool) quotaAwareCandidates() []*providerEntry {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var out []*providerEntry
	for _, e := range p.providers {
		if !e.spec.isEnabled() {
			continue
		}
		// Check quota via DB or mem
		dailyUsed, monthlyUsed := p.getQuotaUsed(e.spec.Name)
		if e.spec.DailyQuota > 0 && dailyUsed >= e.spec.DailyQuota {
			continue
		}
		if e.spec.MonthlyQuota > 0 && monthlyUsed >= e.spec.MonthlyQuota {
			continue
		}
		// Skip providers whose sender reports unconfigured only if we have
		// other candidates — otherwise try anyway to surface honest error.
		out = append(out, e)
	}
	// If all are quota-exhausted, return empty so caller gets clear message
	return out
}

func (p *EmailPool) getQuotaUsed(provider string) (dailyUsed, monthlyUsed int) {
	if p.db != nil {
		var du, mu int
		var curDay, curMonth string
		err := p.db.QueryRowContext(context.Background(), `SELECT daily_used, monthly_used, current_day, current_month FROM email_provider_counters WHERE provider = ?`, provider).Scan(&du, &mu, &curDay, &curMonth)
		if err != nil {
			return 0, 0
		}
		today := time.Now().Format("2006-01-02")
		thisMonth := time.Now().Format("2006-01")
		if curDay != today {
			du = 0
		}
		if curMonth != thisMonth {
			mu = 0
		}
		return du, mu
	}
	// in-memory fallback
	p.mu.RLock()
	mc, ok := p.memCounters[provider]
	p.mu.RUnlock()
	if !ok {
		return 0, 0
	}
	today := time.Now().Format("2006-01-02")
	thisMonth := time.Now().Format("2006-01")
	du, mu := mc.dailyUsed, mc.monthlyUsed
	if mc.currentDay != today {
		du = 0
	}
	if mc.currentMonth != thisMonth {
		mu = 0
	}
	return du, mu
}

func (p *EmailPool) recordUsage(provider string, msg EmailMessage) error {
	// Persist to send log for audit + future quota derivation
	if p.db != nil {
		idv := idpkg.NewUUIDGenerator().GenerateUUID()
		_, _ = p.db.ExecContext(context.Background(), `INSERT INTO email_send_log (id, provider, recipient, subject, status, created_at)
			VALUES (?, ?, ?, ?, 'sent', datetime('now'))`, idv, provider, msg.To, msg.Subject)
		// Upsert counter with day/month rollover
		_, _ = p.db.ExecContext(context.Background(), `INSERT OR IGNORE INTO email_provider_counters (provider, daily_used, monthly_used, current_day, current_month)
			VALUES (?, 0, 0, date('now'), strftime('%Y-%m','now'))`, provider)
		_, err := p.db.ExecContext(context.Background(), `
			UPDATE email_provider_counters SET
				daily_used = CASE WHEN current_day != date('now') THEN 1 ELSE daily_used + 1 END,
				monthly_used = CASE WHEN current_month != strftime('%Y-%m','now') THEN 1 ELSE monthly_used + 1 END,
				current_day = date('now'),
				current_month = strftime('%Y-%m','now'),
				updated_at = datetime('now')
			WHERE provider = ?`, provider)
		return err
	}
	// in-memory increment with rollover
	p.mu.Lock()
	defer p.mu.Unlock()
	mc, ok := p.memCounters[provider]
	if !ok {
		mc = &memCounter{currentDay: time.Now().Format("2006-01-02"), currentMonth: time.Now().Format("2006-01")}
		p.memCounters[provider] = mc
	}
	today := time.Now().Format("2006-01-02")
	thisMonth := time.Now().Format("2006-01")
	if mc.currentDay != today {
		mc.dailyUsed = 0
		mc.currentDay = today
	}
	if mc.currentMonth != thisMonth {
		mc.monthlyUsed = 0
		mc.currentMonth = thisMonth
	}
	mc.dailyUsed++
	mc.monthlyUsed++
	return nil
}

func (p *EmailPool) recordFailure(provider string, msg EmailMessage, sendErr error) error {
	if p.db == nil {
		return nil
	}
	idv := idpkg.NewUUIDGenerator().GenerateUUID()
	_, _ = p.db.ExecContext(context.Background(), `INSERT INTO email_send_log (id, provider, recipient, subject, status, error, created_at)
		VALUES (?, ?, ?, ?, 'failed', ?, datetime('now'))`, idv, provider, msg.To, msg.Subject, sendErr.Error())
	return nil
}

// GetUsage returns quota snapshots for all providers, ordered by current priority.
func (p *EmailPool) GetUsage() []ProviderUsage {
	p.mu.RLock()
	providersCopy := append([]*providerEntry(nil), p.providers...)
	p.mu.RUnlock()
	var out []ProviderUsage
	for _, e := range providersCopy {
		du, mu := p.getQuotaUsed(e.spec.Name)
		dq, mq := e.spec.DailyQuota, e.spec.MonthlyQuota
		dr, mr := 0, 0
		if dq > 0 {
			dr = dq - du
			if dr < 0 {
				dr = 0
			}
		} else {
			dr = -1 // unlimited
		}
		if mq > 0 {
			mr = mq - mu
			if mr < 0 {
				mr = 0
			}
		} else {
			mr = -1
		}
		exhausted := (dq > 0 && du >= dq) || (mq > 0 && mu >= mq)
		out = append(out, ProviderUsage{
			Name:             e.spec.Name,
			Host:             e.spec.Host,
			Enabled:          e.spec.isEnabled(),
			Priority:         e.spec.Priority,
			DailyQuota:       dq,
			MonthlyQuota:     mq,
			DailyUsed:        du,
			MonthlyUsed:      mu,
			DailyRemaining:   dr,
			MonthlyRemaining: mr,
			CostPer1k:        e.spec.CostPer1k,
			Exhausted:        exhausted,
		})
	}
	return out
}

// SetProviderEnabled dynamically enables/disables a provider at runtime and
// persists the choice to email_providers so it survives restarts.
func (p *EmailPool) SetProviderEnabled(name string, enabled bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	found := false
	for _, e := range p.providers {
		if e.spec.Name == name {
			e.spec.Enabled = &enabled
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("email pool: unknown provider %q", name)
	}
	if p.db != nil {
		_, err := p.db.ExecContext(context.Background(), `UPDATE email_providers SET enabled = ?, updated_at = datetime('now') WHERE provider = ?`, boolToInt(enabled), name)
		if err != nil {
			return err
		}
	}
	return nil
}

// SetProviderPriority changes a provider's priority and re-sorts the pool.
func (p *EmailPool) SetProviderPriority(name string, priority int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	found := false
	for _, e := range p.providers {
		if e.spec.Name == name {
			e.spec.Priority = priority
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("email pool: unknown provider %q", name)
	}
	p.sortProvidersLocked()
	if p.db != nil {
		_, err := p.db.ExecContext(context.Background(), `UPDATE email_providers SET priority = ?, updated_at = datetime('now') WHERE provider = ?`, priority, name)
		return err
	}
	return nil
}

// SetPrimary makes name the highest-priority (1) provider and bumps others.
func (p *EmailPool) SetPrimary(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var target *providerEntry
	for _, e := range p.providers {
		if e.spec.Name == name {
			target = e
			break
		}
	}
	if target == nil {
		return fmt.Errorf("email pool: unknown provider %q", name)
	}
	if !target.spec.isEnabled() {
		enabled := true
		target.spec.Enabled = &enabled
	}
	// Assign priority 1 to target, 2..N to rest ordered by old priority
	sort.SliceStable(p.providers, func(i, j int) bool {
		return p.providers[i].spec.Priority < p.providers[j].spec.Priority
	})
	// Move target to front then reassign sequential priorities
	var reordered []*providerEntry
	reordered = append(reordered, target)
	for _, e := range p.providers {
		if e.spec.Name != name {
			reordered = append(reordered, e)
		}
	}
	for i, e := range reordered {
		e.spec.Priority = i + 1
		if p.db != nil {
			_, _ = p.db.ExecContext(context.Background(), `UPDATE email_providers SET priority = ?, enabled = ?, updated_at = datetime('now') WHERE provider = ?`,
				e.spec.Priority, boolToInt(e.spec.isEnabled()), e.spec.Name)
		}
	}
	p.providers = reordered
	return nil
}

// ListProviders returns a snapshot of provider specs (for admin UI).
func (p *EmailPool) ListProviders() []ProviderSpec {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]ProviderSpec, len(p.providers))
	for i, e := range p.providers {
		out[i] = e.spec
	}
	return out
}

// ResetUsage clears counters for a provider (admin recovery).
func (p *EmailPool) ResetUsage(name string) error {
	if p.db != nil {
		_, err := p.db.ExecContext(context.Background(), `UPDATE email_provider_counters SET daily_used = 0, monthly_used = 0, current_day = date('now'), current_month = strftime('%Y-%m','now'), updated_at = datetime('now') WHERE provider = ?`, name)
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if mc, ok := p.memCounters[name]; ok {
		mc.dailyUsed = 0
		mc.monthlyUsed = 0
		mc.currentDay = time.Now().Format("2006-01-02")
		mc.currentMonth = time.Now().Format("2006-01")
	}
	return nil
}

// Strategy returns the pool's selection strategy.
func (p *EmailPool) Strategy() string { return p.strategy }

// Helpers for env parsing

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func envInt(key string, fallback int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return fallback
}
