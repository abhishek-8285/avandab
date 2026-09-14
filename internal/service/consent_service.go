package service

import (
	"context"
	"database/sql"

	"transport-app/internal/repository"
)

// DPDP consent ledger (Digital Personal Data Protection Act, 2023, §§6–8).
// One row per (tenant, user, purpose) in user_consents (00153): grant records
// notice_version + granted_at; withdrawal stamps withdrawn_at; re-grant clears
// it. No row (legacy/OAuth/admin-created users) = allowed — only an explicit
// withdrawal blocks Login, mirroring the tenantActive legacy allowance.
const (
	// ConsentPurposePlatformUse is the single purpose in this slice: processing
	// the account to operate the platform. New purposes widen the CHECK + UX.
	ConsentPurposePlatformUse = "platform_use"
	// ConsentNoticeVersion identifies the notice text the user agreed to.
	// Bump when the notice changes; re-grant then re-binds users to it.
	ConsentNoticeVersion = "v1"
)

// grantConsentTx upserts a granted row, routing through the ambient tx when
// the caller holds one (registration) via repository.ExecTx.
func grantConsentTx(ctx context.Context, db *sql.DB, tenantID, userID string) error {
	_, err := repository.ExecTx(ctx, db, `
		INSERT INTO user_consents (id, tenant_id, user_id, purpose, notice_version, granted_at, withdrawn_at)
		VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP, NULL)
		ON CONFLICT (tenant_id, user_id, purpose) DO UPDATE SET
			notice_version = excluded.notice_version,
			granted_at = CURRENT_TIMESTAMP,
			withdrawn_at = NULL,
			updated_at = CURRENT_TIMESTAMP`,
		"consent_"+tenantID+"_"+userID, tenantID, userID, ConsentPurposePlatformUse, ConsentNoticeVersion)
	return err
}

// consentWithdrawn reports whether the user explicitly withdrew platform-use
// consent. Missing row = legacy = false (allowed).
func consentWithdrawn(ctx context.Context, db *sql.DB, tenantID, userID string) (bool, error) {
	var withdrawn sql.NullTime
	err := repository.QueryRowTx(ctx, db, `
		SELECT withdrawn_at FROM user_consents
		WHERE tenant_id = $1 AND user_id = $2 AND purpose = $3`,
		tenantID, userID, ConsentPurposePlatformUse).Scan(&withdrawn)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return withdrawn.Valid, nil
}

// consentDB returns the raw DB or nil when the store cannot provide one
// (fakes without storage: consent checks become pass-through, same as
// tenantActive).
func (s *baseService) consentDB() *sql.DB {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter == nil {
		return nil
	}
	return getter.DB()
}

// GrantConsent records (or re-records after withdrawal) platform-use consent.
func (s *UserService) GrantConsent(ctx context.Context, tenantID, userID string) error {
	db := s.consentDB()
	if db == nil {
		return nil
	}
	return grantConsentTx(ctx, db, tenantID, userID)
}

// WithdrawConsent stamps withdrawal; Login refuses while set (DPDP §6(4):
// cease processing on withdrawal). Works with or without a prior grant.
func (s *UserService) WithdrawConsent(ctx context.Context, tenantID, userID string) error {
	db := s.consentDB()
	if db == nil {
		return nil
	}
	_, err := repository.ExecTx(ctx, db, `
		INSERT INTO user_consents (id, tenant_id, user_id, purpose, notice_version, granted_at, withdrawn_at)
		VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (tenant_id, user_id, purpose) DO UPDATE SET
			withdrawn_at = CURRENT_TIMESTAMP,
			updated_at = CURRENT_TIMESTAMP`,
		"consent_"+tenantID+"_"+userID, tenantID, userID, ConsentPurposePlatformUse, ConsentNoticeVersion)
	return err
}

// ConsentStatus reports the ledger state for the caller (nulls when unknown).
func (s *UserService) ConsentStatus(ctx context.Context, tenantID, userID string) (grantedAt, withdrawnAt sql.NullTime, err error) {
	db := s.consentDB()
	if db == nil {
		return sql.NullTime{}, sql.NullTime{}, nil
	}
	err = repository.QueryRowTx(ctx, db, `
		SELECT granted_at, withdrawn_at FROM user_consents
		WHERE tenant_id = $1 AND user_id = $2 AND purpose = $3`,
		tenantID, userID, ConsentPurposePlatformUse).Scan(&grantedAt, &withdrawnAt)
	if err == sql.ErrNoRows {
		return sql.NullTime{}, sql.NullTime{}, nil
	}
	return grantedAt, withdrawnAt, err
}
